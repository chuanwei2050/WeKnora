package utils

import (
	"os"
	"strconv"
)

const (
	// DefaultMaxFileSizeMB is the default upload/gRPC limit (~2GB, safe for gRPC int32).
	DefaultMaxFileSizeMB = 2047
	// GrpcMaxMessageBytes is the maximum gRPC message size supported by grpcio/go-grpc.
	GrpcMaxMessageBytes = 2147483647
)

// ResolveMaxFileSizeMB returns the configured upload limit in MB.
// Unset, empty, or non-positive values fall back to DefaultMaxFileSizeMB.
// Values above DefaultMaxFileSizeMB are capped to stay within gRPC limits.
func ResolveMaxFileSizeMB() int64 {
	if sizeStr := os.Getenv("MAX_FILE_SIZE_MB"); sizeStr != "" {
		if size, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
			if size <= 0 {
				return DefaultMaxFileSizeMB
			}
			if size > DefaultMaxFileSizeMB {
				return DefaultMaxFileSizeMB
			}
			return size
		}
	}
	return DefaultMaxFileSizeMB
}

// GetMaxFileSize returns the maximum file upload size in bytes.
func GetMaxFileSize() int64 {
	return ResolveMaxFileSizeMB() * 1024 * 1024
}

// GetMaxFileSizeMB returns the maximum file upload size in MB.
func GetMaxFileSizeMB() int64 {
	return ResolveMaxFileSizeMB()
}

// GetGRPCMaxMessageSize returns the gRPC max send/recv message size in bytes.
func GetGRPCMaxMessageSize() int {
	bytes := int(ResolveMaxFileSizeMB() * 1024 * 1024)
	if bytes > GrpcMaxMessageBytes {
		return GrpcMaxMessageBytes
	}
	return bytes
}
