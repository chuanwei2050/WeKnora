package chatpipeline

import "testing"

func TestThinkTagFilterHidesEmbeddedThinkingAcrossChunks(t *testing.T) {
	filter := thinkTagFilter{}
	chunks := []struct {
		content string
		done    bool
	}{
		{content: "<thi"},
		{content: "nk>private reasoning</thi"},
		{content: "nk>visible answer", done: true},
	}
	var got string
	for _, chunk := range chunks {
		got += filter.Write(chunk.content, chunk.done)
	}
	if got != "visible answer" {
		t.Fatalf("unexpected visible content: %q", got)
	}
}

func TestThinkTagFilterKeepsOrdinaryAnswer(t *testing.T) {
	filter := thinkTagFilter{}
	got := filter.Write("ordinary ", false) + filter.Write("answer", true)
	if got != "ordinary answer" {
		t.Fatalf("unexpected visible content: %q", got)
	}
}
