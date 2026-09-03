package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHandleDuplicateKnowledgeErrorCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	knowledge := &types.Knowledge{ID: "existing", FileName: "example.pdf", DirectoryBreadcrumb: []types.PathNode{{ID: "directory", Name: "现有目录"}}}

	tests := []struct {
		name string
		err  error
		code string
	}{
		{
			name: "duplicate file",
			err:  types.NewDuplicateFileError(knowledge),
			code: "duplicate_file",
		},
		{
			name: "file being deleted",
			err:  types.NewDeletingFileError(knowledge),
			code: "file_deleting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(response)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/knowledge", nil)

			handled := (&KnowledgeHandler{}).handleDuplicateKnowledgeError(ctx, tt.err, knowledge, "file")

			require.True(t, handled)
			require.Equal(t, http.StatusConflict, response.Code)
			var body struct {
				Code string           `json:"code"`
				Data *types.Knowledge `json:"data"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, tt.code, body.Code)
			require.Equal(t, "existing", body.Data.ID)
			require.Equal(t, "现有目录", body.Data.DirectoryBreadcrumb[0].Name)
		})
	}
}
