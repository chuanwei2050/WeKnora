package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactFeishuPublishRunStripsSecret(t *testing.T) {
	rev := FeishuPublishConfigRevision{
		AppID:           "cli_x",
		AppSecretCipher: "ciphertext-should-not-leak",
		SpaceID:         "space",
	}
	b, err := json.Marshal(rev)
	if err != nil {
		t.Fatal(err)
	}
	run := &FeishuPublishRun{
		ID:             "run-1",
		Status:         FeishuPublishRunQueued,
		ConfigRevision: JSON(b),
	}
	out := RedactFeishuPublishRun(run)
	if out == nil || out.ID != "run-1" {
		t.Fatalf("expected redacted copy, got %#v", out)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "ciphertext-should-not-leak") {
		t.Fatalf("secret cipher leaked in API payload: %s", raw)
	}
	if strings.Contains(string(run.ConfigRevision), "ciphertext-should-not-leak") == false {
		t.Fatal("original run must keep cipher for workers")
	}
}
