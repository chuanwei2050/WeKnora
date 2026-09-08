package service

import (
	"testing"

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
