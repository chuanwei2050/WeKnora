package session

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type attachmentProcessorFileService struct {
	interfaces.FileService
}

func (attachmentProcessorFileService) SaveBytes(context.Context, []byte, uint64, string, bool) (string, error) {
	return "local://attachment", nil
}

type attachmentProcessorDocumentReader struct {
	interfaces.DocumentReader
	received *types.ReadRequest
}

func (r *attachmentProcessorDocumentReader) Read(_ context.Context, req *types.ReadRequest) (*types.ReadResult, error) {
	r.received = req
	return &types.ReadResult{MarkdownContent: "股票代码：002967"}, nil
}

func TestProcessAttachmentNormalizesDocumentTypeAndKeepsContent(t *testing.T) {
	reader := &attachmentProcessorDocumentReader{}
	processor := NewAttachmentProcessor(attachmentProcessorFileService{}, reader, nil, nil)

	attachment, err := processor.ProcessAttachment(
		context.Background(),
		[]byte("docx bytes"),
		"company.docx",
		10,
		10483,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if reader.received == nil || reader.received.FileType != "docx" {
		t.Fatalf("document reader received file type %q, want docx", reader.received.FileType)
	}
	if attachment.Content != "股票代码：002967" {
		t.Fatalf("attachment content = %q", attachment.Content)
	}
}
