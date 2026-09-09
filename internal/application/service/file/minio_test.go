package file

import (
	"context"
	"net/url"
	"testing"
)

func TestMinioDownloadURLUsesPublicEndpoint(t *testing.T) {
	t.Setenv("MINIO_PUBLIC_ENDPOINT", "http://localhost:9000")
	svc, err := newMinioClient("minio:9000", "access-key", "secret-key", "weknora", false)
	if err != nil {
		t.Fatalf("create MinIO client: %v", err)
	}

	downloadURL, err := svc.GetFileURL(context.Background(), "minio://weknora/tenant/document.docx")
	if err != nil {
		t.Fatalf("generate download URL: %v", err)
	}
	parsed, err := url.Parse(downloadURL)
	if err != nil {
		t.Fatalf("parse download URL: %v", err)
	}
	if parsed.Host != "localhost:9000" {
		t.Fatalf("download host = %q, want localhost:9000", parsed.Host)
	}
}
