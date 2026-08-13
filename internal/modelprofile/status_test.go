package modelprofile

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResolveProfile(t *testing.T) {
	t.Run("blank defaults online valid", func(t *testing.T) {
		t.Setenv("MODEL_PROFILE", "  ")
		got := ResolveProfile()
		if got.Profile != "online" || !got.ProfileValid || got.ProfileRaw != "  " {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("offline", func(t *testing.T) {
		t.Setenv("MODEL_PROFILE", "OFFLINE")
		got := ResolveProfile()
		if got.Profile != "offline" || !got.ProfileValid {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("invalid falls back", func(t *testing.T) {
		t.Setenv("MODEL_PROFILE", "staging")
		got := ResolveProfile()
		if got.Profile != "online" || got.ProfileValid || got.ProfileRaw != "staging" {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestAirGapped(t *testing.T) {
	t.Setenv("AIR_GAPPED_MODE", "")
	if AirGapped() {
		t.Fatal("expected false")
	}
	t.Setenv("AIR_GAPPED_MODE", "true")
	if !AirGapped() {
		t.Fatal("expected true")
	}
}

func TestExpandEnvRefs(t *testing.T) {
	t.Setenv("ONLINE_MODEL_BASE_URL", "https://api.example.com/v1")
	if got := ExpandEnvRefs("${ONLINE_MODEL_BASE_URL}"); got != "https://api.example.com/v1" {
		t.Fatalf("got %q", got)
	}
	if got := ExpandEnvRefs("${MISSING_VAR}"); got != "${MISSING_VAR}" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandEnvRefsRefusesSecrets(t *testing.T) {
	t.Setenv("ONLINE_MODEL_API_KEY", "sk-should-not-leak")
	t.Setenv("ONLINE_LLM_MODEL_API_KEY", "sk-also-secret")
	got := ExpandEnvRefs("${ONLINE_MODEL_API_KEY}")
	if got != "${ONLINE_MODEL_API_KEY}" {
		t.Fatalf("secret env must not expand, got %q", got)
	}
	clearOnlineOfflineEnv(t)
	t.Setenv("MODEL_PROFILE", "online")
	t.Setenv("ONLINE_LLM_MODEL_NAME", "${ONLINE_MODEL_API_KEY}")
	t.Setenv("ONLINE_LLM_MODEL_BASE_URL", "http://127.0.0.1/v1")
	status := Build(nil)
	raw, _ := json.Marshal(status)
	if strings.Contains(string(raw), "sk-should-not-leak") || strings.Contains(string(raw), "sk-also-secret") {
		t.Fatalf("leaked secret in response: %s", raw)
	}
	for _, r := range status.Roles {
		if r.Role == "chat" && r.Status != "missing_env" {
			t.Fatalf("chat should be missing_env when name refs api key, got %+v", r)
		}
	}
}

func TestBuildOnlineOK(t *testing.T) {
	clearOnlineOfflineEnv(t)
	t.Setenv("MODEL_PROFILE", "online")
	t.Setenv("ONLINE_MODEL_BASE_URL", "https://api.siliconflow.cn/v1")
	t.Setenv("ONLINE_LLM_MODEL_NAME", "Qwen/Qwen3.6-27B")
	t.Setenv("ONLINE_LLM_MODEL_BASE_URL", "${ONLINE_MODEL_BASE_URL}")
	t.Setenv("ONLINE_VERIFIER_MODEL_1_NAME", "deepseek-ai/DeepSeek-V3.1-Terminus")
	t.Setenv("ONLINE_VERIFIER_MODEL_1_BASE_URL", "${ONLINE_MODEL_BASE_URL}")
	t.Setenv("ONLINE_VERIFIER_MODEL_2_NAME", "Qwen/Qwen3.5-9B")
	t.Setenv("ONLINE_VERIFIER_MODEL_2_BASE_URL", "${ONLINE_MODEL_BASE_URL}")
	t.Setenv("ONLINE_EVALUATION_JUDGE_MODEL_NAME", "Qwen/Qwen3.6-27B")
	t.Setenv("ONLINE_EVALUATION_JUDGE_MODEL_BASE_URL", "${ONLINE_MODEL_BASE_URL}")
	t.Setenv("ONLINE_EMBEDDING_MODEL_NAME", "Qwen/Qwen3-Embedding-4B")
	t.Setenv("ONLINE_EMBEDDING_MODEL_DIMENSION", "2560")
	t.Setenv("ONLINE_EMBEDDING_MODEL_BASE_URL", "${ONLINE_MODEL_BASE_URL}")
	t.Setenv("ONLINE_RERANK_MODEL_NAME", "BAAI/bge-reranker-v2-m3")
	t.Setenv("ONLINE_RERANK_MODEL_BASE_URL", "${ONLINE_MODEL_BASE_URL}")
	t.Setenv("ONLINE_VLM_MODEL_NAME", "Qwen/Qwen3.6-27B")
	t.Setenv("ONLINE_VLM_MODEL_BASE_URL", "${ONLINE_MODEL_BASE_URL}")
	t.Setenv("ONLINE_ASR_MODEL_NAME", "FunAudioLLM/SenseVoiceSmall")
	t.Setenv("ONLINE_ASR_MODEL_BASE_URL", "${ONLINE_MODEL_BASE_URL}")
	t.Setenv("ONLINE_TTS_MODEL_NAME", "FunAudioLLM/CosyVoice2-0.5B")
	t.Setenv("ONLINE_TTS_MODEL_BASE_URL", "${ONLINE_MODEL_BASE_URL}")

	models := []ModelView{
		{ID: "chat-1", Name: "Qwen/Qwen3.6-27B", Type: "KnowledgeQA", CreatedAt: time.Unix(1, 0)},
		{ID: "emb-1", Name: "Qwen/Qwen3-Embedding-4B", Type: "Embedding", CreatedAt: time.Unix(1, 0), EmbeddingDimension: 2560},
		{ID: "rerank-1", Name: "BAAI/bge-reranker-v2-m3", Type: "Rerank", CreatedAt: time.Unix(1, 0)},
		{ID: "vlm-1", Name: "Qwen/Qwen3.6-27B", Type: "VLLM", CreatedAt: time.Unix(1, 0)},
		{ID: "asr-1", Name: "FunAudioLLM/SenseVoiceSmall", Type: "ASR", CreatedAt: time.Unix(1, 0)},
		{ID: "tts-1", Name: "FunAudioLLM/CosyVoice2-0.5B", Type: "TTS", CreatedAt: time.Unix(1, 0)},
		{ID: "v1-qa", Name: "deepseek-ai/DeepSeek-V3.1-Terminus", Type: "KnowledgeQA", CreatedAt: time.Unix(1, 0)},
		{ID: "v2-qa", Name: "Qwen/Qwen3.5-9B", Type: "KnowledgeQA", CreatedAt: time.Unix(1, 0)},
	}

	status := Build(models)
	if status.Summary.MissingRegistration != 0 || status.Summary.MissingEnv != 0 || status.Summary.Mismatch != 0 || status.Summary.OK != 9 {
		t.Fatalf("summary %+v", status.Summary)
	}
	raw, _ := json.Marshal(status)
	if strings.Contains(strings.ToLower(string(raw)), "api_key") {
		t.Fatalf("leaked api_key: %s", raw)
	}
}

func TestBuildGaps(t *testing.T) {
	clearOnlineOfflineEnv(t)
	t.Setenv("MODEL_PROFILE", "offline")
	t.Setenv("OFFLINE_LLM_MODEL_NAME", "Qwen/Qwen3.6-27B")
	t.Setenv("OFFLINE_LLM_MODEL_BASE_URL", "http://__FILL_PRIVATE_MAIN_MODEL_HOST__:8000/v1")
	t.Setenv("OFFLINE_EMBEDDING_MODEL_NAME", "Qwen/Qwen3-Embedding-4B")
	t.Setenv("OFFLINE_EMBEDDING_MODEL_DIMENSION", "2560")
	t.Setenv("OFFLINE_EMBEDDING_MODEL_BASE_URL", "http://127.0.0.1:8001/v1")
	t.Setenv("OFFLINE_VERIFIER_MODEL_1_NAME", "Qwen/Qwen3.6-27B")
	t.Setenv("OFFLINE_VERIFIER_MODEL_1_BASE_URL", "http://127.0.0.1:8000/v1")
	t.Setenv("OFFLINE_VERIFIER_MODEL_2_NAME", "")
	t.Setenv("OFFLINE_RERANK_MODEL_NAME", "")
	t.Setenv("OFFLINE_VLM_MODEL_NAME", "Qwen/Qwen3.6-27B")
	t.Setenv("OFFLINE_VLM_MODEL_BASE_URL", "${UNRESOLVED_BASE}")
	t.Setenv("OFFLINE_ASR_MODEL_NAME", "")
	t.Setenv("OFFLINE_TTS_MODEL_NAME", "")
	t.Setenv("OFFLINE_EVALUATION_JUDGE_MODEL_NAME", "Qwen/Qwen3.5-9B")
	t.Setenv("OFFLINE_EVALUATION_JUDGE_MODEL_BASE_URL", "http://127.0.0.1:8000/v1")

	models := []ModelView{
		{ID: "emb-wrong-dim", Name: "Qwen/Qwen3-Embedding-4B", Type: "Embedding", CreatedAt: time.Unix(2, 0), EmbeddingDimension: 1024},
		{ID: "chat-old", Name: "Qwen/Qwen3.6-27B", Type: "KnowledgeQA", CreatedAt: time.Unix(1, 0)},
		{ID: "ver-special", Name: "Qwen/Qwen3.6-27B", Type: "Verifier", CreatedAt: time.Unix(3, 0)},
		{ID: "vlm-vllm", Name: "Qwen/Qwen3.6-27B", Type: "VLLM", CreatedAt: time.Unix(1, 0)},
	}

	status := Build(models)
	byRole := map[string]RoleStatus{}
	for _, r := range status.Roles {
		byRole[r.Role] = r
	}
	if byRole["chat"].Status != "missing_env" {
		t.Fatalf("chat %+v", byRole["chat"])
	}
	if byRole["verifier_1"].MatchedModelID != "ver-special" {
		t.Fatalf("verifier_1 %+v", byRole["verifier_1"])
	}
	if byRole["embedding"].Status != "mismatch" {
		t.Fatalf("embedding %+v", byRole["embedding"])
	}
	if byRole["evaluation_judge"].Status != "missing_registration" {
		t.Fatalf("judge %+v", byRole["evaluation_judge"])
	}
	if byRole["vlm"].Status != "missing_env" {
		t.Fatalf("vlm %+v", byRole["vlm"])
	}

	var editEmb, addJudge bool
	for _, a := range status.Actions {
		if a.Role == "embedding" && a.Intent == "edit" && a.MatchedModelID == "emb-wrong-dim" {
			editEmb = true
		}
		if a.Role == "evaluation_judge" && a.Intent == "add" {
			addJudge = true
		}
		if a.Role == "chat" || a.Role == "vlm" {
			t.Fatalf("unexpected action %+v", a)
		}
	}
	if !editEmb || !addJudge {
		t.Fatalf("actions %+v", status.Actions)
	}
}

func TestStableSort(t *testing.T) {
	clearOnlineOfflineEnv(t)
	t.Setenv("MODEL_PROFILE", "online")
	t.Setenv("ONLINE_LLM_MODEL_NAME", "same-model")
	t.Setenv("ONLINE_LLM_MODEL_BASE_URL", "http://127.0.0.1/v1")
	models := []ModelView{
		{ID: "b", Name: "same-model", Type: "KnowledgeQA", CreatedAt: time.Unix(2, 0)},
		{ID: "a", Name: "same-model", Type: "KnowledgeQA", CreatedAt: time.Unix(1, 0)},
	}
	status := Build(models)
	for _, r := range status.Roles {
		if r.Role == "chat" {
			if r.MatchedModelID != "a" {
				t.Fatalf("got %+v", r)
			}
			return
		}
	}
	t.Fatal("missing chat")
}

func TestJSONContractNoSecretsAndRequiredFields(t *testing.T) {
	clearOnlineOfflineEnv(t)
	t.Setenv("MODEL_PROFILE", "online")
	t.Setenv("ONLINE_LLM_MODEL_NAME", "m1")
	t.Setenv("ONLINE_LLM_MODEL_BASE_URL", "http://127.0.0.1/v1")

	status := Build([]ModelView{{ID: "1", Name: "m1", Type: "KnowledgeQA"}})
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"profile", "profile_raw", "profile_valid", "air_gapped", "summary", "roles", "actions"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("missing field %s in %s", key, raw)
		}
	}
	lower := strings.ToLower(string(raw))
	for _, bad := range []string{"api_key", "apikey", "\"secret\""} {
		if strings.Contains(lower, bad) {
			t.Fatalf("unexpected secret-like field %q in %s", bad, raw)
		}
	}
}

func TestOfflineProfileChangesChecklistWithoutMutatingModels(t *testing.T) {
	clearOnlineOfflineEnv(t)
	models := []ModelView{{ID: "1", Name: "Qwen/Qwen3.6-27B", Type: "KnowledgeQA"}}

	t.Setenv("MODEL_PROFILE", "online")
	t.Setenv("ONLINE_LLM_MODEL_NAME", "Qwen/Qwen3.6-27B")
	t.Setenv("ONLINE_LLM_MODEL_BASE_URL", "https://api.example.com/v1")
	online := Build(models)

	t.Setenv("MODEL_PROFILE", "offline")
	t.Setenv("OFFLINE_LLM_MODEL_NAME", "Qwen/Qwen3.6-27B")
	t.Setenv("OFFLINE_LLM_MODEL_BASE_URL", "http://__FILL_PRIVATE_MAIN_MODEL_HOST__:8000/v1")
	offline := Build(models)

	if online.Profile == offline.Profile {
		t.Fatal("profile should differ")
	}
	var onlineChat, offlineChat string
	for _, r := range online.Roles {
		if r.Role == "chat" {
			onlineChat = r.Status
		}
	}
	for _, r := range offline.Roles {
		if r.Role == "chat" {
			offlineChat = r.Status
		}
	}
	if onlineChat != "ok" {
		t.Fatalf("online chat=%s", onlineChat)
	}
	if offlineChat != "missing_env" {
		t.Fatalf("offline chat=%s", offlineChat)
	}
	if models[0].Name != "Qwen/Qwen3.6-27B" {
		t.Fatal("input models were mutated")
	}
}

func clearOnlineOfflineEnv(t *testing.T) {
	t.Helper()
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "ONLINE_") || strings.HasPrefix(e, "OFFLINE_") || strings.HasPrefix(e, "MODEL_PROFILE=") || strings.HasPrefix(e, "AIR_GAPPED_MODE=") {
			_ = os.Unsetenv(strings.SplitN(e, "=", 2)[0])
		}
	}
}
