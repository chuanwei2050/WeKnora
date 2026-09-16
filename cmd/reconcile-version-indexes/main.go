package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/container"
	"github.com/Tencent/WeKnora/internal/runtime"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

type options struct {
	tenantID    uint64
	kbIDs       string
	concurrency int
	dryRun      bool
	limit       int
}

func main() {
	opts := parseOptions()
	if err := loadLocalDotEnv(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: unable to load local .env: %v\n", err)
	}
	// Host-side runs talk to published docker ports, not compose DNS names.
	overrideHostsideRetrieverAddrs()
	_ = os.Setenv("AUTO_MIGRATE", "false")
	_ = os.Setenv("SKIP_TASK_RECONCILIATION", "true")

	c := container.BuildContainer(runtime.GetContainer())
	err := c.Invoke(func(db *gorm.DB, purger service.VersionIndexPurger) error {
		items, err := loadCandidates(context.Background(), db, opts)
		if err != nil {
			return err
		}
		fmt.Printf("Selected %d completed documents for index reconcile\n", len(items))
		if opts.dryRun {
			for _, item := range items {
				fmt.Printf("DRY-RUN tenant=%d kb=%s knowledge=%s title=%q current=%s\n",
					item.TenantID, item.KnowledgeBaseID, item.ID, item.Title, item.CurrentVersionID)
			}
			return nil
		}
		return runReconcile(context.Background(), purger, items, opts.concurrency)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func overrideHostsideRetrieverAddrs() {
	// Inside the app container, compose DNS names already work. Only rewrite
	// when this command runs on the Windows/macOS host against published ports.
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return
	}
	if addr := strings.TrimSpace(os.Getenv("ELASTICSEARCH_ADDR")); addr == "" || strings.Contains(addr, "elasticsearch") {
		_ = os.Setenv("ELASTICSEARCH_ADDR", "http://127.0.0.1:9200")
	}
	if addr := strings.TrimSpace(os.Getenv("MILVUS_ADDRESS")); addr == "" || strings.Contains(addr, "milvus") {
		_ = os.Setenv("MILVUS_ADDRESS", "127.0.0.1:19530")
	}
	if host := strings.TrimSpace(os.Getenv("REDIS_HOST")); host == "" || host == "redis" {
		_ = os.Setenv("REDIS_HOST", "127.0.0.1")
	}
	if endpoint := strings.TrimSpace(os.Getenv("MINIO_ENDPOINT")); endpoint == "" || strings.HasPrefix(endpoint, "minio") {
		_ = os.Setenv("MINIO_ENDPOINT", "127.0.0.1:9000")
	}
}

func loadLocalDotEnv() error {
	content, err := os.ReadFile(".env")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	values, err := godotenv.Unmarshal(strings.TrimPrefix(string(content), "\ufeff"))
	if err != nil {
		return err
	}
	for name, value := range values {
		if current, exists := os.LookupEnv(name); !exists || strings.TrimSpace(current) == "" {
			if err := os.Setenv(name, value); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseOptions() options {
	var opts options
	flag.Uint64Var(&opts.tenantID, "tenant", 0, "tenant ID")
	flag.StringVar(&opts.kbIDs, "kb", "", "comma-separated knowledge base IDs")
	flag.IntVar(&opts.concurrency, "concurrency", 2, "concurrent reconciles (1-8)")
	flag.BoolVar(&opts.dryRun, "dry-run", false, "list candidates without rewriting indexes")
	flag.IntVar(&opts.limit, "limit", 0, "maximum documents; 0 means no limit")
	flag.Parse()
	if opts.tenantID == 0 {
		fmt.Fprintln(os.Stderr, "--tenant is required")
		os.Exit(2)
	}
	if strings.TrimSpace(opts.kbIDs) == "" {
		fmt.Fprintln(os.Stderr, "--kb is required")
		os.Exit(2)
	}
	if opts.concurrency < 1 || opts.concurrency > 8 {
		fmt.Fprintln(os.Stderr, "--concurrency must be between 1 and 8")
		os.Exit(2)
	}
	return opts
}

func loadCandidates(ctx context.Context, db *gorm.DB, opts options) ([]*types.Knowledge, error) {
	kbIDs := make([]string, 0)
	for _, part := range strings.Split(opts.kbIDs, ",") {
		id := strings.TrimSpace(part)
		if id != "" {
			kbIDs = append(kbIDs, id)
		}
	}
	if len(kbIDs) == 0 {
		return nil, fmt.Errorf("no knowledge base IDs provided")
	}
	query := db.WithContext(ctx).Model(&types.Knowledge{}).
		Where("tenant_id = ?", opts.tenantID).
		Where("knowledge_base_id IN ?", kbIDs).
		Where("deleted_at IS NULL").
		Where("enable_status = ?", "enabled").
		Where("parse_status = ?", types.ParseStatusCompleted).
		Where("current_version_id IS NOT NULL AND current_version_id <> ''").
		Order("updated_at ASC").Order("id ASC")
	if opts.limit > 0 {
		query = query.Limit(opts.limit)
	}
	var items []*types.Knowledge
	if err := query.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func runReconcile(ctx context.Context, purger service.VersionIndexPurger, items []*types.Knowledge, workers int) error {
	jobs := make(chan *types.Knowledge)
	var wg sync.WaitGroup
	var failed atomic.Int64
	var done atomic.Int64
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				itemCtx := context.WithValue(ctx, types.TenantIDContextKey, item.TenantID)
				err := purger.ReconcileCurrentVersionIndexes(itemCtx, item.TenantID, item.ID)
				n := done.Add(1)
				if err != nil {
					failed.Add(1)
					fmt.Fprintf(os.Stderr, "[%d/%d] FAIL knowledge=%s: %v\n", n, len(items), item.ID, err)
					continue
				}
				fmt.Printf("[%d/%d] OK knowledge=%s title=%q\n", n, len(items), item.ID, item.Title)
			}
		}()
	}
	for _, item := range items {
		jobs <- item
	}
	close(jobs)
	wg.Wait()
	if failed.Load() > 0 {
		return fmt.Errorf("reconcile completed with %d failures", failed.Load())
	}
	fmt.Printf("Reconcile completed for %d documents\n", len(items))
	return nil
}
