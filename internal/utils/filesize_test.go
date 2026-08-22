package utils

import "testing"

func TestResolveMaxFileSizeMB(t *testing.T) {
	t.Setenv("MAX_FILE_SIZE_MB", "")

	if got := ResolveMaxFileSizeMB(); got != DefaultMaxFileSizeMB {
		t.Fatalf("unset env: got %d want %d", got, DefaultMaxFileSizeMB)
	}

	t.Setenv("MAX_FILE_SIZE_MB", "0")
	if got := ResolveMaxFileSizeMB(); got != DefaultMaxFileSizeMB {
		t.Fatalf("zero env: got %d want %d", got, DefaultMaxFileSizeMB)
	}

	t.Setenv("MAX_FILE_SIZE_MB", "512")
	if got := ResolveMaxFileSizeMB(); got != 512 {
		t.Fatalf("512 env: got %d want 512", got)
	}

	t.Setenv("MAX_FILE_SIZE_MB", "4096")
	if got := ResolveMaxFileSizeMB(); got != DefaultMaxFileSizeMB {
		t.Fatalf("4096 env: got %d want %d", got, DefaultMaxFileSizeMB)
	}
}

func TestGetGRPCMaxMessageSize(t *testing.T) {
	t.Setenv("MAX_FILE_SIZE_MB", "2047")
	want2047 := 2047 * 1024 * 1024
	if got := GetGRPCMaxMessageSize(); got != want2047 {
		t.Fatalf("2047 MB: got %d want %d", got, want2047)
	}

	t.Setenv("MAX_FILE_SIZE_MB", "2048")
	if got := GetGRPCMaxMessageSize(); got != want2047 {
		t.Fatalf("2048 MB should cap to 2047 MB: got %d want %d", got, want2047)
	}
}
