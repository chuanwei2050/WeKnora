package session

import (
	"reflect"
	"testing"
	"unicode/utf8"
)

func TestSplitTTSContentUsesSentenceBoundaries(t *testing.T) {
	content := "第一句话。第二句话！Third sentence? 最后一段"
	want := []string{"第一句话。", "第二句话！", "Third sentence?", "最后一段"}

	if got := splitTTSContent(content, 120); !reflect.DeepEqual(got, want) {
		t.Fatalf("splitTTSContent() = %#v, want %#v", got, want)
	}
}

func TestSplitTTSContentLimitsLongParagraphs(t *testing.T) {
	content := "这是一段没有任何标点而且长度超过限制的文字"
	got := splitTTSContent(content, 8)
	if len(got) < 2 {
		t.Fatalf("splitTTSContent() returned %d chunk, want multiple chunks", len(got))
	}
	for _, chunk := range got {
		if utf8.RuneCountInString(chunk) > 8 {
			t.Fatalf("chunk %q exceeds rune limit", chunk)
		}
	}
}
