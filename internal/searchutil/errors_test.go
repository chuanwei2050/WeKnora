package searchutil

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestClassifyIndexError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		unavailable bool
	}{
		{name: "grpc unavailable", err: status.Error(codes.Unavailable, "down"), unavailable: true},
		{name: "missing index", err: status.Error(codes.NotFound, "missing"), unavailable: true},
		{name: "access denied", err: status.Error(codes.PermissionDenied, "denied")},
		{name: "generic failure", err: errors.New("unknown")},
		{name: "request timeout", err: context.DeadlineExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ClassifyIndexError(test.err)
			if errors.Is(got, ErrIndexUnavailable) != test.unavailable {
				t.Fatalf("classification mismatch: %v", got)
			}
		})
	}
}
