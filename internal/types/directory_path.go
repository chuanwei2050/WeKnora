package types

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	MaxDirectoryDepth     = 32
	MaxDirectoryNameBytes = 255
	MaxDirectoryPathBytes = 1024
)

var ErrInvalidDirectoryPath = errors.New("invalid document directory path")

func NormalizeDirectoryName(name string) (displayName, normalizedName string, err error) {
	displayName = strings.TrimSpace(norm.NFC.String(name))
	invalid := displayName == "" || displayName == "." || displayName == ".." ||
		strings.ContainsAny(displayName, `/\`) || strings.ContainsRune(displayName, rune(0)) ||
		strings.ContainsFunc(displayName, unicode.IsControl) ||
		!utf8.ValidString(displayName) || len([]byte(displayName)) > MaxDirectoryNameBytes
	if invalid {
		return "", "", ErrInvalidDirectoryPath
	}
	return displayName, cases.Fold().String(displayName), nil
}

func ParseDirectoryPath(path string) ([]string, error) {
	path = strings.ReplaceAll(path, `\`, "/")
	if path == "" {
		return nil, nil
	}
	if strings.HasPrefix(path, "/") || len([]byte(path)) > MaxDirectoryPathBytes || (len(path) >= 2 && path[1] == ':') {
		return nil, ErrInvalidDirectoryPath
	}
	raw := strings.Split(path, "/")
	if len(raw) > MaxDirectoryDepth {
		return nil, ErrInvalidDirectoryPath
	}
	segments := make([]string, 0, len(raw))
	for _, segment := range raw {
		displayName, _, err := NormalizeDirectoryName(segment)
		if err != nil {
			return nil, err
		}
		segments = append(segments, displayName)
	}
	return segments, nil
}
