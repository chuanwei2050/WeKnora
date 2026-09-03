package types

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeDirectoryName(t *testing.T) {
	display, key, err := NormalizeDirectoryName("  Ａ项目  ")
	require.NoError(t, err)
	require.Equal(t, "Ａ项目", display)
	require.Equal(t, "ａ项目", key)

	for _, invalid := range []string{"", ".", "..", "a/b", `a\b`, "a" + string(rune(0)), "header\r\ninjection"} {
		_, _, err := NormalizeDirectoryName(invalid)
		require.ErrorIs(t, err, ErrInvalidDirectoryPath)
	}
}

func TestParseDirectoryPath(t *testing.T) {
	segments, err := ParseDirectoryPath(`项目\规范/标准`)
	require.NoError(t, err)
	require.Equal(t, []string{"项目", "规范", "标准"}, segments)

	for _, invalid := range []string{"/absolute", `C:\absolute`, "a//b", "a/../b", strings.Repeat("x/", MaxDirectoryDepth) + "x"} {
		_, err := ParseDirectoryPath(invalid)
		require.ErrorIs(t, err, ErrInvalidDirectoryPath)
	}
}
