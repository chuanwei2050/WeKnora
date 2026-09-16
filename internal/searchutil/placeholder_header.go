package searchutil

import (
	"regexp"
	"strings"
)

var (
	placeholderHeaderKey = regexp.MustCompile(`(?i)^(Unnamed:\s*\d+|未命名列\d+|col_\d+)$`)
	kvPairSplit          = regexp.MustCompile(`(?i)(?:^|,)((?:Unnamed:\s*\d+|未命名列\d+|col_\d+)|[^:,\n]+):`)
)

// IsPlaceholderHeaderKV reports Excel KV rows whose keys are mostly Unnamed/col_N
// placeholders (typical of title-banner / stats sheets), not record schemas.
func IsPlaceholderHeaderKV(content string) bool {
	keys := kvPairSplit.FindAllStringSubmatch(content, -1)
	if len(keys) < 2 {
		return false
	}
	placeholder := 0
	for _, match := range keys {
		name := strings.TrimSpace(match[1])
		if placeholderHeaderKey.MatchString(name) {
			placeholder++
		}
	}
	return float64(placeholder)/float64(len(keys)) >= 0.5
}
