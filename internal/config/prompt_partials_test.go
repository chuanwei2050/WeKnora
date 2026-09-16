package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandPromptPartials(t *testing.T) {
	partials := map[string]string{
		"language_rule": "## CRITICAL: Language Rule\n- ALWAYS respond in {{language}}\n",
	}
	in := "Hello\n\n{{partial:language_rule}}\n\nBye"
	got, err := expandPromptPartials(in, partials)
	if err != nil {
		t.Fatal(err)
	}
	want := "Hello\n\n## CRITICAL: Language Rule\n- ALWAYS respond in {{language}}\n\nBye"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestExpandPromptPartialsUnknown(t *testing.T) {
	_, err := expandPromptPartials("{{partial:missing}}", map[string]string{"other": "x"})
	if err == nil {
		t.Fatal("expected error for unknown partial")
	}
}

func TestExpandPromptPartialsEmptyMapWithRefs(t *testing.T) {
	_, err := expandPromptPartials("{{partial:language_rule}}", nil)
	if err == nil {
		t.Fatal("expected error when partials map is empty but refs exist")
	}
}

func TestLoadPromptTemplatesExpandsPartials(t *testing.T) {
	// Resolve repo config dir relative to this test file's package location.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// internal/config -> repo root
	configDir := filepath.Clean(filepath.Join(wd, "..", "..", "config"))
	if _, err := os.Stat(filepath.Join(configDir, "prompt_templates", "partials.yaml")); err != nil {
		t.Skipf("prompt_templates not found at %s: %v", configDir, err)
	}

	pt, err := loadPromptTemplates(configDir)
	if err != nil {
		t.Fatal(err)
	}
	if pt == nil {
		t.Fatal("expected prompt templates")
	}

	// No unresolved partial markers should remain after load.
	for _, list := range [][]PromptTemplate{
		pt.SystemPrompt,
		pt.IntentPrompts,
		pt.Fallback,
		pt.ContextTemplate,
		pt.Rewrite,
		pt.AgentSystemPrompt,
	} {
		for _, tmpl := range list {
			if strings.Contains(tmpl.Content, "{{partial:") {
				t.Fatalf("template %q content still has unresolved partial: %s", tmpl.ID, tmpl.Content)
			}
			if strings.Contains(tmpl.User, "{{partial:") {
				t.Fatalf("template %q user still has unresolved partial", tmpl.ID)
			}
		}
	}

	// Spot-check expanded language rule and agent system status.
	var defaultKB *PromptTemplate
	for i := range pt.SystemPrompt {
		if pt.SystemPrompt[i].ID == "default_kb" {
			defaultKB = &pt.SystemPrompt[i]
			break
		}
	}
	if defaultKB == nil {
		t.Fatal("default_kb not found")
	}
	if !strings.Contains(defaultKB.Content, "## CRITICAL: Language Rule") {
		t.Fatalf("default_kb missing expanded language rule:\n%s", defaultKB.Content)
	}
	if !strings.Contains(defaultKB.Content, "ALWAYS respond in {{language}}") {
		t.Fatal("default_kb missing language placeholder after expand")
	}

	var pure *PromptTemplate
	for i := range pt.AgentSystemPrompt {
		if pt.AgentSystemPrompt[i].ID == "pure_agent" {
			pure = &pt.AgentSystemPrompt[i]
			break
		}
	}
	if pure == nil {
		t.Fatal("pure_agent not found")
	}
	if !strings.Contains(pure.Content, "### System Status") {
		t.Fatalf("pure_agent missing expanded system status:\n%s", pure.Content)
	}
	if !strings.Contains(pure.Content, "Web Search: {{web_search_status}}") {
		t.Fatal("pure_agent missing web_search_status after expand")
	}
}
