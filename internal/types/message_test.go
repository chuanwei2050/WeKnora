package types

import "testing"

func TestMessageBeforeCreateInitializesJSONMetadata(t *testing.T) {
	message := &Message{}
	if err := message.BeforeCreate(nil); err != nil {
		t.Fatal(err)
	}
	if string(message.VoiceMetadata) != "{}" {
		t.Fatalf("voice metadata = %q, want {}", message.VoiceMetadata)
	}
	if string(message.ResponseTiming) != "{}" {
		t.Fatalf("response timing = %q, want {}", message.ResponseTiming)
	}
}
