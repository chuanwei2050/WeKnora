package types

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVoiceTicketIsSingleUseAndScoped(t *testing.T) {
	store := NewVoiceWSTicketStore()
	now := time.Unix(100, 0)
	ticket, err := store.Issue("u1", 7, "s1", "asr", time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Consume(ticket.Value, "u2", "s1", "asr", now); err == nil {
		t.Fatal("expected scope mismatch")
	}
	if _, err := store.Consume(ticket.Value, "u1", "s1", "asr", now); err == nil {
		t.Fatal("mismatched consume must burn the ticket")
	}
	ticket, _ = store.Issue("u1", 7, "s1", "asr", time.Minute, now)
	if _, err := store.Consume(ticket.Value, "u1", "s1", "asr", now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Consume(ticket.Value, "u1", "s1", "asr", now); err == nil {
		t.Fatal("expected replay rejection")
	}
}

func TestCleanupVoiceTempFilesHonorsMaxAge(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "old.webm")
	newFile := filepath.Join(root, "new.webm")
	if err := os.WriteFile(old, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(old, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	removed, err := CleanupVoiceTempFiles(root, now, time.Hour)
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Fatalf("new file should remain: %v", err)
	}
}

func TestVoiceTicketPurgeExpired(t *testing.T) {
	store := NewVoiceWSTicketStore()
	now := time.Unix(100, 0)
	if _, err := store.Issue("u", 7, "s", "asr", time.Minute, now); err != nil {
		t.Fatal(err)
	}
	if removed := store.PurgeExpired(now.Add(2 * time.Minute)); removed != 1 {
		t.Fatalf("expected one expired ticket, removed %d", removed)
	}
}

func TestVoiceRetentionRequiresExplicitPolicy(t *testing.T) {
	config := VoiceConfig{WSTicketTTL: time.Minute, TempMaxAge: time.Hour, Retention: VoiceRetentionPolicy{RetainTTS: true}}
	if err := config.Validate(); err == nil {
		t.Fatal("expected retention policy without prompt and roles to fail")
	}
	config.Retention = VoiceRetentionPolicy{RetainTTS: true, TTSMaxAge: time.Hour, AllowedRoles: []string{"owner"}, UserPrompt: "保留语音"}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestVoiceDefaultsKeepAudioEphemeralAndRejectLongCleanupAge(t *testing.T) {
	config := VoiceConfig{}
	config.EnsureDefaults()
	if config.Retention.RetainRecordings || config.Retention.RetainTTS {
		t.Fatal("voice audio retention must be opt-in")
	}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := CleanupVoiceTempFiles(t.TempDir(), time.Now(), 25*time.Hour); err == nil {
		t.Fatal("expected cleanup age over 24 hours to be rejected")
	}
}
