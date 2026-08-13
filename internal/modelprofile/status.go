package modelprofile

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var envRefPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// Status is the read-only checklist payload for MODEL_PROFILE.
type Status struct {
	Profile      string       `json:"profile"`
	ProfileRaw   string       `json:"profile_raw"`
	ProfileValid bool         `json:"profile_valid"`
	AirGapped    bool         `json:"air_gapped"`
	Summary      Summary      `json:"summary"`
	Roles        []RoleStatus `json:"roles"`
	Actions      []Action     `json:"actions"`
}

type Summary struct {
	OK                  int `json:"ok"`
	MissingEnv          int `json:"missing_env"`
	MissingRegistration int `json:"missing_registration"`
	Mismatch            int `json:"mismatch"`
}

type RoleStatus struct {
	Role              string `json:"role"`
	ExpectedName      string `json:"expected_name"`
	ExpectedSource    string `json:"expected_source,omitempty"`
	ExpectedBaseURL   string `json:"expected_base_url,omitempty"`
	ExpectedDimension int    `json:"expected_dimension,omitempty"`
	Status            string `json:"status"`
	GapReason         string `json:"gap_reason,omitempty"`
	MatchedModelID    string `json:"matched_model_id,omitempty"`
	MatchedModelName  string `json:"matched_model_name,omitempty"`
	MatchedModelType  string `json:"matched_model_type,omitempty"`
}

type Action struct {
	ID             string `json:"id"`
	Role           string `json:"role"`
	Intent         string `json:"intent"`
	AddDialogType  string `json:"add_dialog_type"`
	MatchedModelID string `json:"matched_model_id,omitempty"`
}

// ModelView is a dependency-free snapshot of a registered model.
type ModelView struct {
	ID                 string
	Name               string
	Type               string
	CreatedAt          time.Time
	EmbeddingDimension int
}

type ResolvedProfile struct {
	Profile      string
	ProfileRaw   string
	ProfileValid bool
}

type roleSpec struct {
	Role          string
	Stem          string
	AcceptTypes   []string
	AddDialogType string
	HasDimension  bool
}

var roleSpecs = []roleSpec{
	{Role: "chat", Stem: "LLM_MODEL", AcceptTypes: []string{"KnowledgeQA"}, AddDialogType: "chat"},
	{Role: "verifier_1", Stem: "VERIFIER_MODEL_1", AcceptTypes: []string{"Verifier", "KnowledgeQA"}, AddDialogType: "chat"},
	{Role: "verifier_2", Stem: "VERIFIER_MODEL_2", AcceptTypes: []string{"Verifier", "KnowledgeQA"}, AddDialogType: "chat"},
	{Role: "evaluation_judge", Stem: "EVALUATION_JUDGE_MODEL", AcceptTypes: []string{"EvaluationJudge", "KnowledgeQA"}, AddDialogType: "chat"},
	{Role: "embedding", Stem: "EMBEDDING_MODEL", AcceptTypes: []string{"Embedding"}, AddDialogType: "embedding", HasDimension: true},
	{Role: "rerank", Stem: "RERANK_MODEL", AcceptTypes: []string{"Rerank"}, AddDialogType: "rerank"},
	{Role: "vlm", Stem: "VLM_MODEL", AcceptTypes: []string{"VLM", "VLLM"}, AddDialogType: "vllm"},
	{Role: "asr", Stem: "ASR_MODEL", AcceptTypes: []string{"ASR"}, AddDialogType: "asr"},
	{Role: "tts", Stem: "TTS_MODEL", AcceptTypes: []string{"TTS"}, AddDialogType: "tts"},
}

func ResolveProfile() ResolvedProfile {
	raw := os.Getenv("MODEL_PROFILE")
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ResolvedProfile{Profile: "online", ProfileRaw: raw, ProfileValid: true}
	}
	lower := strings.ToLower(trimmed)
	if lower == "online" || lower == "offline" {
		return ResolvedProfile{Profile: lower, ProfileRaw: raw, ProfileValid: true}
	}
	return ResolvedProfile{Profile: "online", ProfileRaw: raw, ProfileValid: false}
}

func AirGapped() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("AIR_GAPPED_MODE")), "true")
}

func ExpandEnvRefs(value string) string {
	cur := value
	for i := 0; i < 8; i++ {
		next := envRefPattern.ReplaceAllStringFunc(cur, func(match string) string {
			name := match[2 : len(match)-1]
			if isSecretEnvName(name) {
				// Keep placeholder so callers treat it as missing_env instead of leaking secrets.
				return match
			}
			if v := os.Getenv(name); v != "" {
				return v
			}
			return match
		})
		if next == cur {
			return next
		}
		cur = next
	}
	return cur
}

func isSecretEnvName(name string) bool {
	upper := strings.ToUpper(strings.TrimSpace(name))
	switch {
	case strings.Contains(upper, "API_KEY"),
		strings.Contains(upper, "APIKEY"),
		strings.Contains(upper, "SECRET"),
		strings.Contains(upper, "PASSWORD"),
		strings.Contains(upper, "TOKEN"),
		strings.Contains(upper, "CREDENTIAL"):
		return true
	default:
		return false
	}
}

func isMissingEnvValue(name, baseURL string) bool {
	name = strings.TrimSpace(name)
	baseURL = strings.TrimSpace(baseURL)
	if name == "" {
		return true
	}
	if strings.Contains(name, "__FILL_") || strings.Contains(baseURL, "__FILL_") {
		return true
	}
	if strings.Contains(name, "${") || strings.Contains(baseURL, "${") {
		return true
	}
	return false
}

// Build compares env expectations for the active profile to registered models.
func Build(models []ModelView) *Status {
	resolved := ResolveProfile()
	prefix := "ONLINE"
	if resolved.Profile == "offline" {
		prefix = "OFFLINE"
	}

	out := &Status{
		Profile:      resolved.Profile,
		ProfileRaw:   resolved.ProfileRaw,
		ProfileValid: resolved.ProfileValid,
		AirGapped:    AirGapped(),
		Roles:        make([]RoleStatus, 0, len(roleSpecs)),
		Actions:      make([]Action, 0),
	}

	for _, spec := range roleSpecs {
		role := evaluateRole(prefix, spec, models)
		out.Roles = append(out.Roles, role)
		switch role.Status {
		case "ok":
			out.Summary.OK++
		case "missing_env":
			out.Summary.MissingEnv++
		case "missing_registration":
			out.Summary.MissingRegistration++
			out.Actions = append(out.Actions, Action{
				ID:            "add_" + role.Role,
				Role:          role.Role,
				Intent:        "add",
				AddDialogType: spec.AddDialogType,
			})
		case "mismatch":
			out.Summary.Mismatch++
			out.Actions = append(out.Actions, Action{
				ID:             "edit_" + role.Role,
				Role:           role.Role,
				Intent:         "edit",
				AddDialogType:  spec.AddDialogType,
				MatchedModelID: role.MatchedModelID,
			})
		}
	}
	return out
}

func evaluateRole(prefix string, spec roleSpec, models []ModelView) RoleStatus {
	expectedName := ExpandEnvRefs(strings.TrimSpace(os.Getenv(prefix + "_" + spec.Stem + "_NAME")))
	expectedSource := ExpandEnvRefs(strings.TrimSpace(os.Getenv(prefix + "_" + spec.Stem + "_SOURCE")))
	expectedBase := ExpandEnvRefs(strings.TrimSpace(os.Getenv(prefix + "_" + spec.Stem + "_BASE_URL")))
	expectedDim := 0
	if spec.HasDimension {
		dimRaw := ExpandEnvRefs(strings.TrimSpace(os.Getenv(prefix + "_" + spec.Stem + "_DIMENSION")))
		if dimRaw != "" {
			if n, err := strconv.Atoi(dimRaw); err == nil {
				expectedDim = n
			}
		}
	}

	role := RoleStatus{
		Role:              spec.Role,
		ExpectedName:      expectedName,
		ExpectedSource:    expectedSource,
		ExpectedBaseURL:   expectedBase,
		ExpectedDimension: expectedDim,
	}

	if isMissingEnvValue(expectedName, expectedBase) {
		role.Status = "missing_env"
		role.GapReason = "env name/base_url empty or still contains placeholders"
		return role
	}

	matched := matchModel(expectedName, spec.AcceptTypes, models)
	if matched == nil {
		role.Status = "missing_registration"
		role.GapReason = "no registered model with matching name and acceptable type"
		return role
	}

	role.MatchedModelID = matched.ID
	role.MatchedModelName = matched.Name
	role.MatchedModelType = matched.Type

	if spec.HasDimension && expectedDim > 0 {
		gotDim := matched.EmbeddingDimension
		if gotDim > 0 && gotDim != expectedDim {
			role.Status = "mismatch"
			role.GapReason = "embedding dimension differs from env; rebuild index may be required"
			return role
		}
	}

	role.Status = "ok"
	return role
}

func matchModel(expectedName string, accept []string, models []ModelView) *ModelView {
	name := strings.TrimSpace(expectedName)
	typeRank := map[string]int{}
	for i, t := range accept {
		typeRank[t] = i
	}

	var candidates []ModelView
	for _, m := range models {
		if strings.TrimSpace(m.Name) != name {
			continue
		}
		if _, ok := typeRank[m.Type]; !ok {
			continue
		}
		candidates = append(candidates, m)
	}
	if len(candidates) == 0 {
		return nil
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		ri, rj := typeRank[candidates[i].Type], typeRank[candidates[j].Type]
		if ri != rj {
			return ri < rj
		}
		if !candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
		}
		return candidates[i].ID < candidates[j].ID
	})
	chosen := candidates[0]
	return &chosen
}
