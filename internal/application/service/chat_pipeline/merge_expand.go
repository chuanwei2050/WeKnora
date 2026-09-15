package chatpipeline

// runeLen returns the length of a string in runes
func runeLen(s string) int {
	return len([]rune(s))
}

func containsID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}
