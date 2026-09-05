package tools

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestCanceledCacheWaiterDoesNotWaitForLeader(t *testing.T) {
	db := regressionDuckDB(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	file := &fakeFileService{readers: map[string]func() (io.ReadCloser, error){"slow.csv": func() (io.ReadCloser, error) {
		close(entered)
		<-release
		return io.NopCloser(strings.NewReader("id\n1\n")), nil
	}}}
	k := &types.Knowledge{ID: "review-cancel", FilePath: "slow.csv", FileType: "csv", UpdatedAt: time.Now()}
	a := NewDataAnalysisTool(nil, nil, nil, file, db, "leader", InternalDataAnalysisAuthorization())
	b := NewDataAnalysisTool(nil, nil, nil, file, db, "waiter", InternalDataAnalysisAuthorization())
	t.Cleanup(func() {
		parsedAnalysisCache.Lock()
		defer parsedAnalysisCache.Unlock()
		if entry := parsedAnalysisCache.entries[analysisCacheKey(a, k)]; entry != nil {
			entry.expires = time.Now().Add(-time.Second)
			pruneAnalysisCacheLocked()
		}
	})
	defer a.Cleanup(context.Background())
	defer b.Cleanup(context.Background())
	finished := make(chan error, 1)
	go func() { _, err := a.LoadFromKnowledge(context.Background(), k); finished <- err }()
	<-entered
	defer func() {
		close(release)
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	returned := make(chan error, 1)
	go func() { _, err := b.LoadFromKnowledge(ctx, k); returned <- err }()
	select {
	case err := <-returned:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unexpected cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("canceled waiter still depends on completion of the live leader")
	}
}

func TestCanceledCacheLeaderDoesNotCancelLiveRequest(t *testing.T) {
	db := regressionDuckDB(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var downloads atomic.Int32
	file := &fakeFileService{readers: map[string]func() (io.ReadCloser, error){"source.csv": func() (io.ReadCloser, error) {
		if downloads.Add(1) == 1 {
			close(entered)
			<-release
		}
		return io.NopCloser(strings.NewReader("id\n1\n")), nil
	}}}
	k := &types.Knowledge{ID: "canceled-leader", FilePath: "source.csv", FileType: "csv", UpdatedAt: time.Now()}
	a := NewDataAnalysisTool(nil, nil, nil, file, db, "leader", InternalDataAnalysisAuthorization())
	b := NewDataAnalysisTool(nil, nil, nil, file, db, "live", InternalDataAnalysisAuthorization())
	defer a.Cleanup(context.Background())
	defer b.Cleanup(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leader, live := make(chan error, 1), make(chan error, 1)
	go func() { _, err := a.LoadFromKnowledge(ctx, k); leader <- err }()
	<-entered
	go func() { _, err := b.LoadFromKnowledge(context.Background(), k); live <- err }()
	cancel()
	close(release)
	if err := <-leader; !errors.Is(err, context.Canceled) {
		t.Errorf("unexpected leader result: %v", err)
	}
	if err := <-live; err != nil {
		t.Fatal("live request inherited cancellation:", err)
	}
	if downloads.Load() != 2 {
		t.Fatalf("unexpected downloads: %d", downloads.Load())
	}
	parsedAnalysisCache.Lock()
	if entry := parsedAnalysisCache.entries[analysisCacheKey(b, k)]; entry != nil {
		entry.expires = time.Now().Add(-time.Second)
		pruneAnalysisCacheLocked()
	}
	parsedAnalysisCache.Unlock()
}
