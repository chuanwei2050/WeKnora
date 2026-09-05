package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
)

const analysisCacheTTL = 10 * time.Minute
const analysisCacheMaxBytes int64 = 128 << 20

type analysisCacheEntry struct {
	path    string
	schema  *TableSchema
	expires time.Time
	size    int64
	users   int
	invalid bool
}

type analysisCacheCall struct {
	done chan struct{}
	err  error // Published by closing done.
}

var parsedAnalysisCache = struct {
	sync.Mutex
	entries map[string]*analysisCacheEntry
	calls   map[string]*analysisCacheCall
}{entries: make(map[string]*analysisCacheEntry), calls: make(map[string]*analysisCacheCall)}

func analysisCacheKey(t *DataAnalysisTool, k *types.Knowledge) string {
	// Mutable legacy records without an update timestamp have no safe cache key.
	if k.CurrentVersionID == "" && k.UpdatedAt.IsZero() {
		return ""
	}
	encoded, _ := json.Marshal([]interface{}{fmt.Sprintf("%p", t.db), k.TenantID, k.ID, k.CurrentVersionID, k.UpdatedAt, k.FilePath, k.FileSize, k.FileType})
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func acquireAnalysisCache(key string) (*analysisCacheEntry, func()) {
	parsedAnalysisCache.Lock()
	entry := parsedAnalysisCache.entries[key]
	if entry == nil || time.Now().After(entry.expires) {
		parsedAnalysisCache.Unlock()
		return nil, func() {}
	}
	entry.users++
	parsedAnalysisCache.Unlock()
	return entry, func() {
		parsedAnalysisCache.Lock()
		entry.users--
		if entry.invalid && entry.users == 0 {
			_ = os.Remove(entry.path)
		}
		pruneAnalysisCacheLocked()
		parsedAnalysisCache.Unlock()
	}
}

func invalidateAnalysisCache(key string, entry *analysisCacheEntry) {
	parsedAnalysisCache.Lock()
	defer parsedAnalysisCache.Unlock()
	if parsedAnalysisCache.entries[key] == entry {
		delete(parsedAnalysisCache.entries, key)
		entry.invalid = true
		if entry.users == 0 {
			_ = os.Remove(entry.path)
		}
	}
}

func pruneAnalysisCacheLocked() {
	for key, entry := range parsedAnalysisCache.entries {
		if entry.users == 0 && time.Now().After(entry.expires) {
			_ = os.Remove(entry.path)
			delete(parsedAnalysisCache.entries, key)
		}
	}
}

func (t *DataAnalysisTool) storeAnalysisCache(ctx context.Context, key string, schema *TableSchema) {
	temp, err := os.CreateTemp("", "weknora-analysis-cache-*.parquet")
	if err != nil {
		return
	}
	path := temp.Name()
	_ = temp.Close()
	_ = os.Remove(path)
	sql := "COPY " + quoteDuckDBIdentifier(schema.TableName) + " TO '" + sqlSingleQuoteEscape(path) + "' (FORMAT PARQUET)"
	if _, err := t.db.ExecContext(ctx, sql); err != nil {
		_ = os.Remove(path)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		_ = os.Remove(path)
		return
	}
	if info.Size() > dataAnalysisMaxCacheFileBytes {
		_ = os.Remove(path)
		return
	}
	parsedAnalysisCache.Lock()
	defer parsedAnalysisCache.Unlock()
	pruneAnalysisCacheLocked()
	var total int64
	for _, entry := range parsedAnalysisCache.entries {
		total += entry.size
	}
	if _, exists := parsedAnalysisCache.entries[key]; exists || len(parsedAnalysisCache.entries) >= 8 || total+info.Size() > analysisCacheMaxBytes {
		_ = os.Remove(path)
		return
	}
	parsedAnalysisCache.entries[key] = &analysisCacheEntry{path: path, schema: schema, size: info.Size(), expires: time.Now().Add(analysisCacheTTL)}
	time.AfterFunc(analysisCacheTTL, func() { parsedAnalysisCache.Lock(); pruneAnalysisCacheLocked(); parsedAnalysisCache.Unlock() })
}

func (t *DataAnalysisTool) loadCachedAnalysis(ctx context.Context, k *types.Knowledge, key string) (*TableSchema, bool, error) {
	entry, release := acquireAnalysisCache(key)
	if entry == nil {
		return nil, false, nil
	}
	defer release()
	name := t.TableName(k)
	if _, err := t.db.ExecContext(ctx, "CREATE TABLE "+quoteDuckDBIdentifier(name)+" AS SELECT * FROM read_parquet('"+sqlSingleQuoteEscape(entry.path)+"')"); err != nil {
		if ctx.Err() != nil {
			return nil, true, err
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		_, _ = t.db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+quoteDuckDBIdentifier(name))
		cancel()
		invalidateAnalysisCache(key, entry)
		return nil, false, nil
	}
	t.recordCreatedTable(name)
	schema := *entry.schema
	schema.TableName = name
	t.loadedSchemas[knowledgeSchemaCacheKey(k)] = &schema
	return &schema, true, nil
}

// The caller must authorize knowledge before this method. Agent Execute always
// uses LoadFromKnowledgeID, which rechecks scope and governance even on hits.
func (t *DataAnalysisTool) LoadFromKnowledge(ctx context.Context, k *types.Knowledge) (*TableSchema, error) {
	t.operationMu.Lock()
	defer t.operationMu.Unlock()
	return t.loadFromKnowledge(ctx, k)
}

func (t *DataAnalysisTool) loadFromKnowledge(ctx context.Context, k *types.Knowledge) (*TableSchema, error) {
	if k == nil {
		return nil, fmt.Errorf("knowledge cannot be nil")
	}
	if k.FileSize > utils.GetMaxFileSize() {
		return nil, fmt.Errorf("file exceeds analysis size limit")
	}
	if t.loadedSchemas == nil {
		t.loadedSchemas = make(map[string]*TableSchema)
	}
	if schema := t.loadedSchemas[knowledgeSchemaCacheKey(k)]; schema != nil {
		return schema, nil
	}
	key := analysisCacheKey(t, k)
	if key == "" {
		return t.loadKnowledgeFile(ctx, k)
	}
	if schema, hit, err := t.loadCachedAnalysis(ctx, k, key); hit {
		return schema, err
	}
	load := func() (interface{}, error) {
		if schema, hit, err := t.loadCachedAnalysis(ctx, k, key); hit {
			return schema, err
		}
		schema, err := t.loadKnowledgeFile(ctx, k)
		if err == nil {
			t.storeAnalysisCache(ctx, key, schema)
		}
		return schema, err
	}
	parsedAnalysisCache.Lock()
	call, waiting := parsedAnalysisCache.calls[key]
	if !waiting {
		call = &analysisCacheCall{done: make(chan struct{})}
		parsedAnalysisCache.calls[key] = call
	}
	parsedAnalysisCache.Unlock()
	if waiting {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
		}
	} else {
		// Keep table ownership synchronous with the initiating tool's operation
		// lock. Other requests can cancel without waiting for this load to finish.
		func() {
			defer func() {
				parsedAnalysisCache.Lock()
				delete(parsedAnalysisCache.calls, key)
				close(call.done)
				parsedAnalysisCache.Unlock()
			}()
			_, call.err = load()
		}()
	}
	if waiting && call.err != nil && ctx.Err() == nil && (errors.Is(call.err, context.Canceled) || errors.Is(call.err, context.DeadlineExceeded)) {
		// A canceled leader must not cancel an independently live request.
		return t.loadKnowledgeFile(ctx, k)
	}
	if call.err != nil {
		return nil, call.err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if schema := t.loadedSchemas[knowledgeSchemaCacheKey(k)]; schema != nil {
		return schema, nil
	}
	if schema, hit, err := t.loadCachedAnalysis(ctx, k, key); hit {
		return schema, err
	}
	// Cache capacity is bounded. A rejected cache write still allows this request.
	return t.loadKnowledgeFile(ctx, k)
}
