package service

import (
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestDocumentProcessQueueIsolatesLargeFiles(t *testing.T) {
	tests := []struct {
		name     string
		fileSize int64
		want     string
	}{
		{name: "regular", fileSize: types.LargeDocumentThresholdBytes, want: "default"},
		{name: "large", fileSize: types.LargeDocumentThresholdBytes + 1, want: types.LargeDocumentQueue},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := documentProcessQueue(test.fileSize); got != test.want {
				t.Fatalf("documentProcessQueue(%d) = %q, want %q", test.fileSize, got, test.want)
			}
		})
	}
}

func TestDocumentPreviewTaskLimitsFollowFileSize(t *testing.T) {
	tests := []struct {
		name        string
		fileSize    int64
		wantQueue   string
		wantTimeout time.Duration
	}{
		{name: "regular", fileSize: types.LargeDocumentThresholdBytes, wantQueue: "default", wantTimeout: 5 * time.Minute},
		{name: "large", fileSize: types.LargeDocumentThresholdBytes + 1, wantQueue: types.LargeDocumentQueue, wantTimeout: 15 * time.Minute},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := documentPreviewQueue(test.fileSize); got != test.wantQueue {
				t.Fatalf("documentPreviewQueue(%d) = %q, want %q", test.fileSize, got, test.wantQueue)
			}
			if got := documentPreviewTimeout(test.fileSize); got != test.wantTimeout {
				t.Fatalf("documentPreviewTimeout(%d) = %s, want %s", test.fileSize, got, test.wantTimeout)
			}
		})
	}
}
