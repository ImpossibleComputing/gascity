package doctor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/runtime/secretscrub"
	workdirutil "github.com/gastownhall/gascity/internal/workdir"
)

// ScopedWorkerCredentialFilesCheck validates configured broker-issued worker
// credential env files before a worker launch attempts to consume them. The
// runtime enforces the same file contract at launch; doctor surfaces mistakes
// earlier during the Phase-3 cutover. Findings never include credential values.
type ScopedWorkerCredentialFilesCheck struct {
	cfg      *config.City
	cityPath string
}

func NewScopedWorkerCredentialFilesCheck(cfg *config.City, cityPath string) *ScopedWorkerCredentialFilesCheck {
	return &ScopedWorkerCredentialFilesCheck{cfg: cfg, cityPath: cityPath}
}

func (c *ScopedWorkerCredentialFilesCheck) Name() string { return "scoped-worker-credential-files" }

func (c *ScopedWorkerCredentialFilesCheck) CanFix() bool { return false }

func (c *ScopedWorkerCredentialFilesCheck) WarmupEligible() bool { return false }

func (c *ScopedWorkerCredentialFilesCheck) Fix(_ *CheckContext) error { return nil }

func (c *ScopedWorkerCredentialFilesCheck) Run(_ *CheckContext) *CheckResult {
	r := &CheckResult{Name: c.Name(), Severity: SeverityAdvisory}
	if c.cfg == nil {
		r.Status = StatusOK
		r.Message = "no scoped worker credential env files configured"
		return r
	}
	configured := 0
	var details []string
	for _, agent := range c.cfg.Agents {
		path, err := c.agentScopedCredentialEnvFile(agent)
		if err != nil {
			details = append(details, fmt.Sprintf("agent %q: %v", agent.QualifiedName(), err))
			continue
		}
		if path == "" {
			continue
		}
		configured++
		if err := secretscrub.ValidateScopedCredentialEnvFile(path); err != nil {
			details = append(details, fmt.Sprintf("agent %q: %v", agent.QualifiedName(), err))
		}
	}
	sort.Strings(details)
	if len(details) > 0 {
		r.Status = StatusWarning
		r.Message = fmt.Sprintf("%d scoped worker credential env file issue(s)", len(details))
		r.Details = details
		r.FixHint = "write broker-issued credential env files as absolute 0600 dotenv files with only allowed credential keys and non-empty values"
		return r
	}
	r.Status = StatusOK
	if configured == 0 {
		r.Message = "no scoped worker credential env files configured"
	} else {
		r.Message = fmt.Sprintf("%d scoped worker credential env file(s) valid", configured)
	}
	return r
}

func (c *ScopedWorkerCredentialFilesCheck) agentScopedCredentialEnvFile(agent config.Agent) (string, error) {
	envPath := strings.TrimSpace(agent.Env[secretscrub.ScopedCredentialEnvFileEnv])
	fieldPath := strings.TrimSpace(agent.ScopedCredentialEnvFile)
	if envPath != "" && fieldPath != "" {
		return "", fmt.Errorf("sets both scoped_credential_env_file and env.%s", secretscrub.ScopedCredentialEnvFileEnv)
	}
	if fieldPath == "" {
		return envPath, nil
	}
	cityName := workdirutil.CityName(c.cityPath, c.cfg)
	qualifiedName := agent.QualifiedName()
	if strings.TrimSpace(agent.Scope) == "rig" && strings.TrimSpace(agent.Dir) == "" && len(c.cfg.Rigs) > 0 {
		// A rig-scoped template without a dir stamp will instantiate once per
		// rig. Doctor validates the first concrete expansion here so template
		// syntax and path shape fail early; launch-time validation still runs for
		// each concrete session.
		qualifiedName = c.cfg.Rigs[0].Name + "/" + agent.BindingQualifiedName()
	}
	ctx := workdirutil.PathContextForQualifiedName(c.cityPath, cityName, qualifiedName, agent, c.cfg.Rigs)
	expanded, err := workdirutil.ExpandTemplateStrict(fieldPath, ctx)
	if err != nil {
		return "", fmt.Errorf("expand scoped_credential_env_file %q: %w", fieldPath, err)
	}
	expanded = strings.TrimSpace(expanded)
	if expanded == "" {
		return "", nil
	}
	return workdirutil.ResolveDirPath(c.cityPath, expanded), nil
}
