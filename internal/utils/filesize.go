package utils

import (
	"os"
	"strconv"
)

// GetMaxFileSize returns the maximum file upload size in bytes.
// 0 means unlimited. Can be configured via MAX_FILE_SIZE_MB environment variable.
func GetMaxFileSize() int64 {
	if sizeStr := os.Getenv("MAX_FILE_SIZE_MB"); sizeStr != "" {
		if size, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
			if size <= 0 {
				return 0
			}
			return size * 1024 * 1024
		}
	}
	return 0
}

// GetMaxFileSizeMB returns the maximum file upload size in MB.
// 0 means unlimited.
func GetMaxFileSizeMB() int64 {
	if sizeStr := os.Getenv("MAX_FILE_SIZE_MB"); sizeStr != "" {
		if size, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
			return size
		}
	}
	return 0
}
