package core

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestCoreMaintenanceExecAssets(t *testing.T) {
	required := []string{
		"assets/scripts/_bd_trace.sh",
		"assets/scripts/dolt-target.sh",
		"assets/scripts/escalate.sh",
		"assets/scripts/jsonl-export.sh",
		"assets/scripts/reaper.sh",
		"orders/jsonl-export.toml",
		"orders/reaper.toml",
	}
	for _, path := range required {
		if _, err := fs.Stat(PackFS, path); err != nil {
			t.Fatalf("core pack missing %s: %v", path, err)
		}
	}

	retired := []string{
		"formulas/mol-dog-jsonl.toml",
		"formulas/mol-dog-reaper.toml",
		"orders/mol-dog-jsonl.toml",
		"orders/mol-dog-reaper.toml",
	}
	for _, path := range retired {
		if _, err := fs.Stat(PackFS, path); err == nil {
			t.Fatalf("core pack must not carry retired Dog maintenance asset %s", path)
		}
	}
}

func TestCoreControlDispatcherAgent(t *testing.T) {
	type agentFile struct {
		Description       string            `toml:"description"`
		StartCommand      string            `toml:"start_command"`
		PromptMode        string            `toml:"prompt_mode"`
		ProcessNames      []string          `toml:"process_names"`
		MaxActiveSessions *int              `toml:"max_active_sessions"`
		Scope             string            `toml:"scope"`
		SandboxProfile    string            `toml:"sandbox_profile"`
		Env               map[string]string `toml:"env"`
	}

	data, err := fs.ReadFile(PackFS, "agents/control-dispatcher/agent.toml")
	if err != nil {
		t.Fatalf("core pack missing control-dispatcher agent: %v", err)
	}
	var agent agentFile
	if _, err := toml.Decode(string(data), &agent); err != nil {
		t.Fatalf("Decode(control-dispatcher agent.toml): %v", err)
	}
	if agent.Description == "" {
		t.Fatal("control-dispatcher description is empty")
	}
	if agent.Scope != "" {
		t.Fatalf("control-dispatcher scope = %q, want empty so it expands at city and rig scope", agent.Scope)
	}
	if agent.SandboxProfile != "//.gc/security/worker-credential-deny.sb" {
		t.Fatalf("control-dispatcher sandbox_profile = %q, want worker credential deny profile", agent.SandboxProfile)
	}
	assertNonLLMMaintenanceSecretEnvScrubbed(t, agent.Env)
	wantStartCommand := `sh -c 'export GC_WORKFLOW_TRACE="${GC_WORKFLOW_TRACE:-${GC_CONTROL_DISPATCHER_TRACE_DEFAULT:-${GC_CITY}/.gc/runtime/control-dispatcher-trace.log}}"; trace_dir="${GC_WORKFLOW_TRACE%/*}"; if [ "$trace_dir" = "$GC_WORKFLOW_TRACE" ]; then trace_dir="."; elif [ -z "$trace_dir" ]; then trace_dir="/"; fi; mkdir -p "$trace_dir"; exec "${GC_BIN:-gc}" convoy control --serve --follow {{.Agent}}'`
	if agent.StartCommand != wantStartCommand {
		t.Fatalf("control-dispatcher start_command = %q, want templated dispatcher command", agent.StartCommand)
	}
	if agent.PromptMode != "none" {
		t.Fatalf("control-dispatcher prompt_mode = %q, want none", agent.PromptMode)
	}
	if !reflect.DeepEqual(agent.ProcessNames, []string{"gc"}) {
		t.Fatalf("control-dispatcher process_names = %v, want [gc]", agent.ProcessNames)
	}
	if agent.MaxActiveSessions == nil || *agent.MaxActiveSessions != 1 {
		t.Fatalf("control-dispatcher max_active_sessions = %v, want 1", agent.MaxActiveSessions)
	}
}

func assertNonLLMMaintenanceSecretEnvScrubbed(t *testing.T, env map[string]string) {
	t.Helper()
	for _, key := range defaultWorkerSecretEnvPreflightForbidKeys(t) {
		value, ok := env[key]
		if !ok {
			t.Fatalf("non-LLM maintenance agent must explicitly unset inherited %s", key)
		}
		if value != "" {
			t.Fatalf("non-LLM maintenance agent env[%s] = %q, want empty string so tmux starts with env -u", key, value)
		}
	}
}

func TestBDDogAgentSecretEnvScrubMatchesWorkerPreflight(t *testing.T) {
	type agentFile struct {
		Env map[string]string `toml:"env"`
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "examples", "bd", "dolt", "agents", "dog", "agent.toml"))
	if err != nil {
		t.Fatalf("ReadFile(examples/bd/dolt/agents/dog/agent.toml): %v", err)
	}
	var agent agentFile
	if _, err := toml.Decode(string(data), &agent); err != nil {
		t.Fatalf("Decode(examples/bd/dolt/agents/dog/agent.toml): %v", err)
	}
	assertNonLLMMaintenanceSecretEnvScrubbed(t, agent.Env)
}

func TestWorkerSecretEnvPreflightDefaultListStaysInSyncWithMaintenanceScrubs(t *testing.T) {
	keys := defaultWorkerSecretEnvPreflightForbidKeys(t)
	if len(keys) < 10 {
		t.Fatalf("default worker-secret-env-preflight forbid list is unexpectedly small: %v", keys)
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			t.Fatalf("default worker-secret-env-preflight forbid list contains duplicate %s: %v", key, keys)
		}
		seen[key] = true
	}
	for _, want := range []string{
		"OPENAI_API_KEY",
		"GEMINI_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
		"CLAUDE_CODE_OAUTH_TOKEN",
		"GOOGLE_API_KEY",
		"AWS_SECRET_ACCESS_KEY",
		"GH_TOKEN",
		"GITHUB_TOKEN",
	} {
		if !seen[want] {
			t.Fatalf("default worker-secret-env-preflight forbid list missing %s: %v", want, keys)
		}
	}
}

func defaultWorkerSecretEnvPreflightForbidKeys(t *testing.T) []string {
	t.Helper()
	data, err := fs.ReadFile(PackFS, "assets/scripts/worker-secret-env-preflight.sh")
	if err != nil {
		t.Fatalf("ReadFile(worker-secret-env-preflight.sh): %v", err)
	}
	script := string(data)
	start := strings.Index(script, "forbid=(")
	if start < 0 {
		t.Fatalf("worker-secret-env-preflight.sh missing default forbid=(...) block")
	}
	bodyStart := start + len("forbid=(")
	end := strings.Index(script[bodyStart:], "\n)")
	if end < 0 {
		t.Fatalf("worker-secret-env-preflight.sh missing end of default forbid block")
	}
	body := script[bodyStart : bodyStart+end]
	var keys []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.ContainsAny(line, " \t\"'$()") {
			t.Fatalf("unexpected non-literal env key %q in worker-secret-env-preflight.sh", line)
		}
		keys = append(keys, line)
	}
	return keys
}

func TestCoreMaintenanceOrdersCarryLegacySkipAliases(t *testing.T) {
	type orderFile struct {
		Order struct {
			SkipAliases []string `toml:"skip_aliases"`
		} `toml:"order"`
	}

	for _, tt := range []struct {
		path string
		want string
	}{
		{path: "orders/jsonl-export.toml", want: "mol-dog-jsonl"},
		{path: "orders/reaper.toml", want: "mol-dog-reaper"},
	} {
		data, err := fs.ReadFile(PackFS, tt.path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", tt.path, err)
		}
		var parsed orderFile
		if _, err := toml.Decode(string(data), &parsed); err != nil {
			t.Fatalf("Decode(%s): %v", tt.path, err)
		}
		if len(parsed.Order.SkipAliases) != 1 || parsed.Order.SkipAliases[0] != tt.want {
			t.Fatalf("%s skip_aliases = %#v, want [%q]", tt.path, parsed.Order.SkipAliases, tt.want)
		}
	}
}
