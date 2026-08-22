package types

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type VoiceConfig struct {
	WSTicketTTL time.Duration        `yaml:"ws_ticket_ttl" json:"ws_ticket_ttl"`
	TempMaxAge  time.Duration        `yaml:"temp_max_age" json:"temp_max_age"`
	TempRoot    string               `yaml:"temp_root" json:"temp_root"`
	Retention   VoiceRetentionPolicy `yaml:"retention" json:"retention"`
}

// VoiceRetentionPolicy is opt-in and separates recording retention from TTS
// retention. The default keeps both kinds of audio ephemeral.
type VoiceRetentionPolicy struct {
	RetainRecordings bool          `yaml:"retain_recordings" json:"retain_recordings"`
	RetainTTS        bool          `yaml:"retain_tts" json:"retain_tts"`
	RecordingMaxAge  time.Duration `yaml:"recording_max_age" json:"recording_max_age"`
	TTSMaxAge        time.Duration `yaml:"tts_max_age" json:"tts_max_age"`
	AllowedRoles     []string      `yaml:"allowed_roles" json:"allowed_roles"`
	UserPrompt       string        `yaml:"user_prompt" json:"user_prompt"`
}

func (c *VoiceConfig) EnsureDefaults() {
	if c == nil {
		return
	}
	if c.WSTicketTTL == 0 {
		c.WSTicketTTL = time.Minute
	}
	if c.TempMaxAge == 0 {
		c.TempMaxAge = time.Hour
	}
	if c.TempRoot == "" {
		c.TempRoot = filepath.Join(os.TempDir(), "weknora-voice")
	}
}

func (c VoiceConfig) Validate() error {
	if c.WSTicketTTL <= 0 || c.WSTicketTTL > 5*time.Minute {
		return fmt.Errorf("voice.ws_ticket_ttl must be between 1 second and 5 minutes")
	}
	if c.TempMaxAge <= 0 || c.TempMaxAge > 24*time.Hour {
		return fmt.Errorf("voice.temp_max_age must be between 1 second and 24 hours")
	}
	if err := c.Retention.Validate(); err != nil {
		return err
	}
	return nil
}

func (p VoiceRetentionPolicy) Validate() error {
	if !p.RetainRecordings && !p.RetainTTS {
		return nil
	}
	if len(p.AllowedRoles) == 0 {
		return fmt.Errorf("voice retention requires allowed_roles")
	}
	for _, role := range p.AllowedRoles {
		if strings.TrimSpace(role) == "" {
			return fmt.Errorf("voice retention allowed_roles must not contain empty values")
		}
	}
	if strings.TrimSpace(p.UserPrompt) == "" {
		return fmt.Errorf("voice retention requires user_prompt")
	}
	if p.RetainRecordings && (p.RecordingMaxAge <= 0 || p.RecordingMaxAge > 24*time.Hour) {
		return fmt.Errorf("voice.retention.recording_max_age must be between 1 second and 24 hours")
	}
	if p.RetainTTS && (p.TTSMaxAge <= 0 || p.TTSMaxAge > 24*time.Hour) {
		return fmt.Errorf("voice.retention.tts_max_age must be between 1 second and 24 hours")
	}
	return nil
}

// StartVoiceTempCleaner performs an immediate cleanup and then repeats it
// until ctx is cancelled. It is safe to start during service boot and keeps
// cleanup ownership at the explicitly configured voice temp root.
func StartVoiceTempCleaner(ctx context.Context, root string, maxAge, interval time.Duration) error {
	if err := validateVoiceCleanupSchedule(root, maxAge, interval); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	if _, err := CleanupVoiceTempFiles(root, time.Now(), maxAge); err != nil {
		return err
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				_, _ = CleanupVoiceTempFiles(root, now, maxAge)
			}
		}
	}()
	return nil
}

func validateVoiceCleanupSchedule(root string, maxAge, interval time.Duration) error {
	if root == "" {
		return fmt.Errorf("voice temp root is required")
	}
	if maxAge <= 0 || maxAge > 24*time.Hour {
		return fmt.Errorf("voice temp max age must be between 1 second and 24 hours")
	}
	if interval <= 0 {
		return fmt.Errorf("voice cleanup interval must be positive")
	}
	return nil
}

type VoiceWSTicket struct {
	Value     string
	UserID    string
	TenantID  uint64
	SessionID string
	Purpose   string
	ExpiresAt time.Time
}

type VoiceWSTicketStore struct {
	mu      sync.Mutex
	tickets map[string]VoiceWSTicket
}

func NewVoiceWSTicketStore() *VoiceWSTicketStore {
	return &VoiceWSTicketStore{tickets: make(map[string]VoiceWSTicket)}
}

func (s *VoiceWSTicketStore) Issue(userID string, tenantID uint64, sessionID, purpose string, ttl time.Duration, now time.Time) (VoiceWSTicket, error) {
	if s == nil {
		return VoiceWSTicket{}, fmt.Errorf("voice ticket store is required")
	}
	if ttl <= 0 || ttl > 5*time.Minute {
		return VoiceWSTicket{}, fmt.Errorf("voice ticket TTL exceeds server limit")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return VoiceWSTicket{}, err
	}
	ticket := VoiceWSTicket{Value: base64.RawURLEncoding.EncodeToString(buf), UserID: userID, TenantID: tenantID, SessionID: sessionID, Purpose: purpose, ExpiresAt: now.Add(ttl)}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tickets[ticket.Value] = ticket
	return ticket, nil
}

// Consume atomically validates and deletes a ticket. A replay can therefore
// never reach the WebSocket session handler.
func (s *VoiceWSTicketStore) Consume(value, userID, sessionID, purpose string, now time.Time) (VoiceWSTicket, error) {
	return s.consume(value, userID, sessionID, purpose, true, now)
}

// ConsumeForSession authenticates a WebSocket using only its short-lived
// ticket. The ticket itself carries the user and tenant identity established
// by the authenticated HTTP request that issued it.
func (s *VoiceWSTicketStore) ConsumeForSession(value, sessionID, purpose string, now time.Time) (VoiceWSTicket, error) {
	return s.consume(value, "", sessionID, purpose, false, now)
}

func (s *VoiceWSTicketStore) consume(value, userID, sessionID, purpose string, verifyUser bool, now time.Time) (VoiceWSTicket, error) {
	if s == nil {
		return VoiceWSTicket{}, fmt.Errorf("voice ticket store is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ticket, ok := s.tickets[value]
	if !ok {
		return VoiceWSTicket{}, fmt.Errorf("voice ticket is invalid or already consumed")
	}
	delete(s.tickets, value)
	if !now.Before(ticket.ExpiresAt) {
		return VoiceWSTicket{}, fmt.Errorf("voice ticket expired")
	}
	if (verifyUser && ticket.UserID != userID) || ticket.SessionID != sessionID || ticket.Purpose != purpose {
		return VoiceWSTicket{}, fmt.Errorf("voice ticket scope mismatch")
	}
	return ticket, nil
}

func (s *VoiceWSTicketStore) PurgeExpired(now time.Time) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for value, ticket := range s.tickets {
		if !now.Before(ticket.ExpiresAt) {
			delete(s.tickets, value)
			removed++
		}
	}
	return removed
}

// CleanupVoiceTempFiles removes only regular files below the explicitly
// supplied voice temp directory. It is safe to call at startup and from a
// periodic job; the batch path itself keeps audio in memory and therefore
// normally leaves nothing to clean.
func CleanupVoiceTempFiles(root string, now time.Time, maxAge time.Duration) (int, error) {
	if root == "" {
		return 0, fmt.Errorf("voice temp root is required")
	}
	if maxAge <= 0 || maxAge > 24*time.Hour {
		return 0, fmt.Errorf("voice temp max age must be between 1 second and 24 hours")
	}
	if info, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	} else if !info.IsDir() {
		return 0, fmt.Errorf("voice temp root is not a directory")
	}
	removed := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		if now.Sub(info.ModTime()) >= maxAge {
			if removeErr := os.Remove(path); removeErr != nil {
				return removeErr
			}
			removed++
		}
		return nil
	})
	return removed, err
}
