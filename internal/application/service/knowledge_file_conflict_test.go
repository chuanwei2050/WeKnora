package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestExistingFileConflict(t *testing.T) {
	tests := []struct {
		name             string
		parseStatus      string
		wantCode         string
		refreshCreatedAt bool
	}{
		{
			name:             "existing file",
			parseStatus:      types.ParseStatusCompleted,
			wantCode:         "duplicate_file",
			refreshCreatedAt: true,
		},
		{
			name:             "file being deleted",
			parseStatus:      types.ParseStatusDeleting,
			wantCode:         "file_deleting",
			refreshCreatedAt: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			knowledge := &types.Knowledge{FileName: "example.pdf", ParseStatus: tt.parseStatus}

			err, refreshCreatedAt := existingFileConflict(knowledge)

			require.Equal(t, tt.wantCode, err.Code)
			require.Equal(t, tt.refreshCreatedAt, refreshCreatedAt)
		})
	}
}
