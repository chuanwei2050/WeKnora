package handler

import "testing"

func TestIntegrationUTF8StreamBuffersSplitRunes(t *testing.T) {
	input := []byte("你好，流式回答")
	var stream integrationUTF8Stream
	var got string

	for _, b := range input {
		got += stream.Push(string([]byte{b}))
	}

	if got != string(input) {
		t.Fatalf("Push() = %q, want %q", got, string(input))
	}
	if len(stream.pending) != 0 {
		t.Fatalf("pending bytes = %d, want 0", len(stream.pending))
	}
}
