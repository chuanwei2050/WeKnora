package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeKnowledgeBaseCode(t *testing.T) {
	display, key, err := normalizeKnowledgeBaseCode("  Abc-_19  ")
	require.NoError(t, err)
	require.Equal(t, "Abc-_19", display)
	require.NotNil(t, key)
	require.Equal(t, "ABC-_19", *key)

	display, key, err = normalizeKnowledgeBaseCode("  ")
	require.NoError(t, err)
	require.Empty(t, display)
	require.Nil(t, key)

	for _, invalid := range []string{"含中文", "has space", "slash/", "dot.code"} {
		_, _, err = normalizeKnowledgeBaseCode(invalid)
		require.True(t, errors.Is(err, ErrInvalidKnowledgeBaseCode), invalid)
	}
}
