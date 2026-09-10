package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Tencent/WeKnora/internal/container"
	"github.com/Tencent/WeKnora/internal/runtime"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

type structuredFileBackfiller interface {
	BackfillStructuredFile(context.Context, *types.Knowledge) (string, error)
}

type options struct {
	tenantID  uint64
	all       bool
	limit     int
	workers   int
	dryRun    bool
	knowledge string
}

func main() {
	opts := parseOptions()
	if err := loadLocalDotEnv(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: unable to load local .env: %v\n", err)
	}
	// A one-shot backfill must not migrate schemas or reconcile unrelated task
	// state merely because it reuses the application's dependency graph.
	_ = os.Setenv("AUTO_MIGRATE", "false")
	_ = os.Setenv("SKIP_TASK_RECONCILIATION", "true")
	c := container.BuildContainer(runtime.GetContainer())
	err := c.Invoke(func(db *gorm.DB, service interfaces.KnowledgeService) error {
		backfiller, ok := service.(structuredFileBackfiller)
		if !ok {
			return fmt.Errorf("knowledge service does not support structured-query backfill")
		}
		items, scanned, skipped, err := loadCandidates(context.Background(), db, opts)
		if err != nil {
			return err
		}
		fmt.Printf("Scanned %d table documents; selected %d; skipped %d already submitted\n", scanned, len(items), skipped)
		if opts.dryRun {
			for _, item := range items {
				fmt.Printf("DRY-RUN tenant=%d knowledge=%s kb=%s file=%q\n", item.TenantID, item.ID, item.KnowledgeBaseID, item.FileName)
			}
			return nil
		}
		return runBackfill(context.Background(), backfiller, items, opts.workers)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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
	flag.Uint64Var(&opts.tenantID, "tenant", 0, "tenant ID to backfill")
	flag.BoolVar(&opts.all, "all-tenants", false, "backfill every tenant (explicit operator action)")
	flag.IntVar(&opts.limit, "limit", 0, "maximum selected documents; 0 means no limit")
	flag.IntVar(&opts.workers, "concurrency", 4, "concurrent submissions (1-16)")
	flag.BoolVar(&opts.dryRun, "dry-run", false, "list candidates without submitting files")
	flag.StringVar(&opts.knowledge, "knowledge", "", "backfill one knowledge ID")
	flag.Parse()
	if (opts.tenantID == 0) == !opts.all {
		fmt.Fprintln(os.Stderr, "specify exactly one of --tenant or --all-tenants")
		os.Exit(2)
	}
	if opts.limit < 0 {
		fmt.Fprintln(os.Stderr, "--limit must be >= 0")
		os.Exit(2)
	}
	if opts.workers < 1 || opts.workers > 16 {
		fmt.Fprintln(os.Stderr, "--concurrency must be between 1 and 16")
		os.Exit(2)
	}
	return opts
}

func loadCandidates(ctx context.Context, db *gorm.DB, opts options) ([]*types.Knowledge, int, int, error) {
	query := db.WithContext(ctx).Model(&types.Knowledge{}).
		Where("LOWER(file_type) IN ? OR LOWER(file_name) LIKE ? OR LOWER(file_name) LIKE ? OR LOWER(file_name) LIKE ?", []string{"csv", "xls", "xlsx"}, "%.csv", "%.xls", "%.xlsx").
		Where("parse_status = ?", types.ParseStatusCompleted).
		Where("enable_status = ?", "enabled").
		Where("file_path <> ''").
		Order("created_at ASC").Order("id ASC")
	if opts.tenantID != 0 {
		query = query.Where("tenant_id = ?", opts.tenantID)
	}
	if opts.knowledge != "" {
		query = query.Where("id = ?", opts.knowledge)
	}
	var rows []*types.Knowledge
	if err := query.Find(&rows).Error; err != nil {
		return nil, 0, 0, fmt.Errorf("list historical table documents: %w", err)
	}
	selected := make([]*types.Knowledge, 0, len(rows))
	skipped := 0
	for _, row := range rows {
		if strings.TrimSpace(row.GetMetadata()["structured_dataset_id"]) != "" {
			skipped++
			continue
		}
		selected = append(selected, row)
	}
	if opts.limit > 0 && len(selected) > opts.limit {
		selected = selected[:opts.limit]
	}
	return selected, len(rows), skipped, nil
}

func runBackfill(ctx context.Context, backfiller structuredFileBackfiller, items []*types.Knowledge, concurrency int) error {
	jobs := make(chan *types.Knowledge)
	var workers sync.WaitGroup
	var failed atomic.Int64
	var submitted atomic.Int64
	failures := make([]string, 0)
	var failureMu sync.Mutex
	for range concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
				jobID, err := backfiller.BackfillStructuredFile(ctx, item)
				if err != nil {
					failed.Add(1)
					failureMu.Lock()
					failures = append(failures, fmt.Sprintf("%s: %v", item.ID, err))
					failureMu.Unlock()
					fmt.Fprintf(os.Stderr, "FAILED tenant=%d knowledge=%s file=%q: %v\n", item.TenantID, item.ID, item.FileName, err)
					continue
				}
				submitted.Add(1)
				fmt.Printf("SUBMITTED tenant=%d knowledge=%s job=%s file=%q\n", item.TenantID, item.ID, jobID, item.FileName)
			}
		}()
	}
	for _, item := range items {
		jobs <- item
	}
	close(jobs)
	workers.Wait()
	fmt.Printf("Structured-query backfill finished: selected=%d submitted=%d failed=%d\n", len(items), submitted.Load(), failed.Load())
	if failed.Load() == 0 {
		return nil
	}
	sort.Strings(failures)
	return fmt.Errorf("backfill completed with failures:\n%s", strings.Join(failures, "\n"))
}
