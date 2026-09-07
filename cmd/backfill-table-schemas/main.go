package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/container"
	"github.com/Tencent/WeKnora/internal/runtime"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"golang.org/x/sync/semaphore"
)

const exclusiveImportSize = 8 << 20

func main() {
	limit := flag.Int("limit", 0, "maximum number to backfill; 0 means no limit")
	tenantID := flag.Uint64("tenant", 0, "tenant ID to backfill")
	allTenants := flag.Bool("all-tenants", false, "backfill every tenant (explicit operator action)")
	concurrency := flag.Int("concurrency", 6, "number of small tables processed concurrently (1-16); large tables run exclusively")
	flag.Parse()
	if (*tenantID == 0) == !*allTenants {
		fmt.Fprintln(os.Stderr, "specify exactly one of --tenant or --all-tenants")
		os.Exit(2)
	}
	if *concurrency < 1 || *concurrency > 16 {
		fmt.Fprintln(os.Stderr, "--concurrency must be between 1 and 16")
		os.Exit(2)
	}

	c := container.BuildContainer(runtime.GetContainer())
	err := c.Invoke(func(
		repo interfaces.KnowledgeTableSchemaRepository,
		knowledgeBaseService interfaces.KnowledgeBaseService,
		knowledgeService interfaces.KnowledgeService,
		tenantService interfaces.TenantService,
		fileService interfaces.FileService,
		duckDB *sql.DB,
	) error {
		ctx := context.Background()
		// Backfills may process very large expanded XLSX XML documents. Keep
		// DuckDB below the container's memory ceiling and allow intermediate
		// state to spill instead of letting the operating system kill the job.
		for _, setting := range []string{
			"SET memory_limit = '1GB'",
			"SET threads = 2",
			"SET temp_directory = '/tmp/weknora-schema-backfill-spill'",
		} {
			if _, err := duckDB.ExecContext(ctx, setting); err != nil {
				return fmt.Errorf("configure DuckDB backfill resources: %w", err)
			}
		}
		if *tenantID != 0 {
			ctx = context.WithValue(ctx, types.TenantIDContextKey, *tenantID)
		}
		items, err := repo.ListMissing(ctx, *tenantID, *limit)
		if err != nil {
			return err
		}
		type job struct {
			index     int
			knowledge *types.Knowledge
		}
		jobs := make(chan job)
		var failed atomic.Int64
		var workers sync.WaitGroup
		importSlots := semaphore.NewWeighted(int64(*concurrency))
		process := func(item job) {
			knowledge := item.knowledge
			itemCtx := context.WithValue(ctx, types.TenantIDContextKey, knowledge.TenantID)
			weight := int64(1)
			if knowledge.FileSize >= exclusiveImportSize {
				weight = int64(*concurrency)
			}
			if err := importSlots.Acquire(itemCtx, weight); err != nil {
				failed.Add(1)
				fmt.Fprintf(os.Stderr, "FAILED %s: %v\n", knowledge.ID, err)
				return
			}
			defer importSlots.Release(weight)
			tool := tools.NewDataAnalysisTool(knowledgeBaseService, knowledgeService, tenantService, fileService, duckDB,
				fmt.Sprintf("schema_backfill_%d_%s", item.index, knowledge.ID), tools.InternalDataAnalysisAuthorization())
			schema, loadErr := tool.LoadFromKnowledge(itemCtx, knowledge)
			tool.Cleanup(itemCtx)
			if loadErr != nil {
				failed.Add(1)
				fmt.Fprintf(os.Stderr, "FAILED %s: %v\n", knowledge.ID, loadErr)
				return
			}
			payload, marshalErr := json.Marshal(schema.PersistenceCopy())
			if marshalErr != nil {
				failed.Add(1)
				fmt.Fprintf(os.Stderr, "FAILED %s: %v\n", knowledge.ID, marshalErr)
				return
			}
			persistCtx, cancel := context.WithTimeout(itemCtx, 30*time.Second)
			persistErr := repo.Upsert(persistCtx, knowledge, payload)
			cancel()
			if persistErr != nil {
				failed.Add(1)
				fmt.Fprintf(os.Stderr, "FAILED %s: %v\n", knowledge.ID, persistErr)
				return
			}
			fmt.Printf("OK %s (%d columns, %d rows)\n", knowledge.ID, len(schema.Columns), schema.RowCount)
		}
		for range *concurrency {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for item := range jobs {
					process(item)
				}
			}()
		}
		for index, knowledge := range items {
			jobs <- job{index: index, knowledge: knowledge}
		}
		close(jobs)
		workers.Wait()
		if failed.Load() > 0 {
			return fmt.Errorf("backfill completed with %d failures", failed.Load())
		}
		fmt.Printf("Backfilled %d table schemas\n", len(items))
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
