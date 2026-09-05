package stream

import (
	"context"
	"testing"
)

func TestMemoryStreamLifecycle(t *testing.T) {
	manager := NewMemoryStreamManager()
	ctx := context.Background()

	active, err := manager.IsStreamActive(ctx, "session", "message")
	if err != nil || active {
		t.Fatalf("new stream active = %v, err = %v; want false, nil", active, err)
	}
	if err := manager.MarkStreamActive(ctx, "session", "message"); err != nil {
		t.Fatal(err)
	}
	active, err = manager.IsStreamActive(ctx, "session", "message")
	if err != nil || !active {
		t.Fatalf("marked stream active = %v, err = %v; want true, nil", active, err)
	}
	if err := manager.MarkStreamInactive(ctx, "session", "message"); err != nil {
		t.Fatal(err)
	}
	active, err = manager.IsStreamActive(ctx, "session", "message")
	if err != nil || active {
		t.Fatalf("finished stream active = %v, err = %v; want false, nil", active, err)
	}
}
