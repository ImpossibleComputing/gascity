package secretscrub

import "testing"

func TestApplyDefaultUnsetsRequiresTruthyControl(t *testing.T) {
	env := map[string]string{"GH_TOKEN": "shared"}
	got := ApplyDefaultUnsets(env)
	if got["GH_TOKEN"] != "shared" {
		t.Fatalf("GH_TOKEN = %q, want unchanged when scrub control is absent", got["GH_TOKEN"])
	}
}

func TestApplyDefaultUnsetsScrubsAbsentDefaultsAndPreservesExplicitScopedValues(t *testing.T) {
	env := map[string]string{
		EnableDefaultScrubEnv: "true",
		"OPENAI_API_KEY":      "scoped-worker-token",
		"LANG":                "en_US.UTF-8",
	}
	got := ApplyDefaultUnsets(env)
	got["LANG"] = "mutated"
	if env["LANG"] != "en_US.UTF-8" {
		t.Fatalf("ApplyDefaultUnsets mutated original map; LANG=%q", env["LANG"])
	}
	if got["OPENAI_API_KEY"] != "scoped-worker-token" {
		t.Fatalf("OPENAI_API_KEY = %q, want explicit scoped value preserved", got["OPENAI_API_KEY"])
	}
	if got["GH_TOKEN"] != "" || got["GEMINI_API_KEY"] != "" {
		t.Fatalf("default secrets not scrubbed: GH_TOKEN=%q GEMINI_API_KEY=%q", got["GH_TOKEN"], got["GEMINI_API_KEY"])
	}
	if got[EnableDefaultScrubEnv] != "" {
		t.Fatalf("%s = %q, want scrub control unset in launched env", EnableDefaultScrubEnv, got[EnableDefaultScrubEnv])
	}
	if _, ok := env["GH_TOKEN"]; ok {
		t.Fatalf("original env mutated: %#v", env)
	}
}
