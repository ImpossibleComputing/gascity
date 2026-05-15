package doctor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gastownhall/gascity/internal/beads/contract"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/fsys"
	"github.com/gastownhall/gascity/internal/pgauth"
)

type postgresAuthResolver func(map[string]string, string, pgauth.Endpoint) (pgauth.Resolved, error)

// PostgresAuthCheck verifies that each Postgres-backed bead scope can resolve
// a password without printing the password value.
type PostgresAuthCheck struct {
	cityPath string
	cfg      *config.City
	resolve  postgresAuthResolver
	findings []postgresAuthFinding
}

// NewPostgresAuthCheck creates a check for city and rig Postgres credentials.
func NewPostgresAuthCheck(cityPath string, cfg *config.City) *PostgresAuthCheck {
	return &PostgresAuthCheck{
		cityPath: cityPath,
		cfg:      cfg,
		resolve:  pgauth.ResolveFromEnv,
	}
}

// HasPostgresBackedScope reports whether any city or rig scope has
// MetadataState.Backend == "postgres".
func HasPostgresBackedScope(cityPath string, cfg *config.City) bool {
	return len(postgresAuthScopes(cityPath, cfg)) > 0
}

// Name returns the check identifier.
func (c *PostgresAuthCheck) Name() string { return "postgres-auth" }

// Run resolves credentials for every Postgres-backed scope and aggregates the
// highest-severity result into one doctor line.
func (c *PostgresAuthCheck) Run(_ *CheckContext) *CheckResult {
	findings := c.evaluateScopes()
	c.findings = findings

	result := &CheckResult{Name: c.Name()}
	if len(findings) == 0 {
		result.Status = StatusOK
		result.Message = "no postgres-backed scopes"
		return result
	}

	if len(findings) == 1 {
		finding := findings[0]
		result.Status = finding.status
		result.Message = finding.fullMessage
		result.Details = finding.details
		result.FixHint = finding.fixHint
		return result
	}

	status := StatusOK
	for _, finding := range findings {
		if finding.status > status {
			status = finding.status
		}
	}
	result.Status = status
	for _, finding := range findings {
		result.Details = append(result.Details, finding.detailRow())
	}
	if status == StatusOK {
		result.Message = fmt.Sprintf("%d postgres-backed scope(s); all credentials resolvable", len(findings))
		return result
	}
	first := firstFindingAtStatus(findings, status)
	result.Message = fmt.Sprintf("%d postgres-backed scope(s); first issue: %s", len(findings), first.fullMessage)
	result.FixHint = firstFixHintAtStatus(findings, status)
	return result
}

// CanFix returns false because credential persistence is an operator choice.
func (c *PostgresAuthCheck) CanFix() bool { return false }

// Fix is a no-op.
func (c *PostgresAuthCheck) Fix(_ *CheckContext) error { return nil }

// RenderExtras prints the optional seven-tier credential-resolution table.
func (c *PostgresAuthCheck) RenderExtras(ctx *CheckContext, w io.Writer) {
	if ctx == nil || !ctx.ExplainPostgresAuth {
		return
	}
	findings := c.findings
	if findings == nil {
		findings = c.evaluateScopes()
	}
	if len(findings) == 0 {
		fmt.Fprintln(w, "  no postgres-backed scopes (this flag has no effect)") //nolint:errcheck // best-effort output
		return
	}
	for i, finding := range findings {
		if i > 0 {
			fmt.Fprintln(w) //nolint:errcheck // best-effort output
		}
		finding.renderExplain(w)
	}
}

func (c *PostgresAuthCheck) evaluateScopes() []postgresAuthFinding {
	resolver := c.resolve
	if resolver == nil {
		resolver = pgauth.ResolveFromEnv
	}
	scopes := postgresAuthScopes(c.cityPath, c.cfg)
	findings := make([]postgresAuthFinding, 0, len(scopes))
	for _, scope := range scopes {
		findings = append(findings, evaluatePostgresAuthScope(scope, resolver))
	}
	sortPostgresAuthFindings(findings)
	return findings
}

type postgresAuthScope struct {
	kind     string
	name     string
	root     string
	display  string
	envPath  string
	endpoint pgauth.Endpoint
	database string
}

type postgresAuthFinding struct {
	scope       postgresAuthScope
	status      CheckStatus
	fullMessage string
	short       string
	details     []string
	fixHint     string
	source      pgauth.Source
	errorTier   int
	errorReason string
}

func postgresAuthScopes(cityPath string, cfg *config.City) []postgresAuthScope {
	if cfg == nil {
		return nil
	}
	cityPath = filepath.Clean(cityPath)
	var scopes []postgresAuthScope
	if meta, ok := loadPostgresAuthMetadata(cityPath); ok {
		scopes = append(scopes, newPostgresAuthScope("city", effectivePostgresAuthCityName(cityPath, cfg), cityPath, cityPath, meta))
	}
	for _, rig := range cfg.Rigs {
		if strings.TrimSpace(rig.Path) == "" {
			continue
		}
		root := resolvePostgresAuthScopeRoot(cityPath, rig.Path)
		meta, ok := loadPostgresAuthMetadata(root)
		if !ok {
			continue
		}
		name := strings.TrimSpace(rig.Name)
		if name == "" {
			name = filepath.Base(filepath.Clean(root))
		}
		scopes = append(scopes, newPostgresAuthScope("rig", name, root, cityPath, meta))
	}
	sort.Slice(scopes, func(i, j int) bool {
		return scopes[i].root < scopes[j].root
	})
	return scopes
}

func loadPostgresAuthMetadata(scopeRoot string) (contract.MetadataState, bool) {
	meta, ok, err := contract.LoadMetadataState(fsys.OSFS{}, filepath.Join(scopeRoot, ".beads", "metadata.json"))
	if err != nil || !ok || meta.Backend != "postgres" {
		return contract.MetadataState{}, false
	}
	return meta, true
}

func resolvePostgresAuthScopeRoot(cityPath, scopePath string) string {
	scopePath = strings.TrimSpace(scopePath)
	if filepath.IsAbs(scopePath) {
		return filepath.Clean(scopePath)
	}
	return filepath.Clean(filepath.Join(cityPath, scopePath))
}

func effectivePostgresAuthCityName(cityPath string, cfg *config.City) string {
	fallback := ""
	if strings.TrimSpace(cityPath) != "" {
		fallback = filepath.Base(filepath.Clean(cityPath))
	}
	return config.EffectiveCityName(cfg, fallback)
}

func newPostgresAuthScope(kind, name, root, cityPath string, meta contract.MetadataState) postgresAuthScope {
	hostPort := meta.PostgresHost + ":" + meta.PostgresPort
	relRoot := postgresAuthRelRoot(cityPath, root)
	envPath := postgresAuthScopeEnvPath(relRoot)
	display := "city (" + hostPort + ")"
	if kind == "rig" {
		display = "rigs/" + name + " (" + hostPort + ")"
	}
	return postgresAuthScope{
		kind:    kind,
		name:    name,
		root:    filepath.Clean(root),
		display: display,
		envPath: envPath,
		endpoint: pgauth.Endpoint{
			Host: meta.PostgresHost,
			Port: meta.PostgresPort,
			User: meta.PostgresUser,
		},
		database: meta.PostgresDatabase,
	}
}

func postgresAuthRelRoot(cityPath, root string) string {
	if cityPath == "" {
		return filepath.ToSlash(filepath.Clean(root))
	}
	rel, err := filepath.Rel(filepath.Clean(cityPath), filepath.Clean(root))
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(filepath.Clean(root))
	}
	if rel == "." {
		return "."
	}
	return filepath.ToSlash(rel)
}

func postgresAuthScopeEnvPath(relRoot string) string {
	if relRoot == "." || relRoot == "" {
		return ".beads/.env"
	}
	return relRoot + "/.beads/.env"
}

func evaluatePostgresAuthScope(scope postgresAuthScope, resolve postgresAuthResolver) postgresAuthFinding {
	resolved, err := resolve(nil, scope.root, scope.endpoint)
	if err == nil {
		return postgresAuthSuccessFinding(scope, resolved)
	}

	finding := postgresAuthFinding{
		scope:       scope,
		status:      StatusError,
		source:      pgauth.SourceNone,
		fullMessage: scope.display + ": pgauth returned unrecognized error: " + err.Error(),
		short:       "pgauth returned unrecognized error: " + err.Error(),
		fixHint:     "please file a bug — postgres-auth check did not recognize this error shape",
	}
	if errors.Is(err, pgauth.ErrNoPasswordResolvable) {
		finding.fullMessage = scope.display + ": no password resolvable"
		finding.short = "no password resolvable"
		finding.fixHint = fmt.Sprintf("set BEADS_POSTGRES_PASSWORD in %s (chmod 600)\nor add a [%s:%s] section to ~/.config/beads/credentials.", scope.envPath, scope.endpoint.Host, scope.endpoint.Port)
		finding.details = []string{fmt.Sprintf("scope=%s  source=%s  user=%s", scope.root, pgauth.SourceNone.String(), scope.endpoint.User)}
		return finding
	}

	var permErr *pgauth.PermissivePermissionError
	if errors.As(err, &permErr) {
		tierSource := postgresAuthSourceForErrorPath(scope, permErr.Path)
		finding.fullMessage = fmt.Sprintf("%s: credentials file mode %#o (group/other readable)", scope.display, permErr.Mode.Perm())
		finding.short = fmt.Sprintf("credentials file mode %#o (group/other readable)", permErr.Mode.Perm())
		finding.fixHint = "chmod 600 " + permErr.Path
		finding.details = []string{fmt.Sprintf("tier=%s  path=%s", tierSource.String(), permErr.Path)}
		finding.errorTier = postgresAuthSourceTier(tierSource)
		finding.errorReason = fmt.Sprintf("mode %#o (group/other readable) — chmod 600 to enable", permErr.Mode.Perm())
		return finding
	}

	var parseErr *pgauth.CredentialsParseError
	if errors.As(err, &parseErr) {
		tierSource := postgresAuthSourceForErrorPath(scope, parseErr.Path)
		finding.fullMessage = fmt.Sprintf("%s: parse %s at line %d: %s", scope.display, parseErr.Path, parseErr.Line, parseErr.Reason)
		finding.short = fmt.Sprintf("parse %s at line %d: %s", parseErr.Path, parseErr.Line, parseErr.Reason)
		finding.fixHint = fmt.Sprintf("edit %s line %d — see slice-2 reason vocabulary", parseErr.Path, parseErr.Line)
		finding.details = []string{fmt.Sprintf("tier=%s  path=%s", tierSource.String(), parseErr.Path)}
		finding.errorTier = postgresAuthSourceTier(tierSource)
		finding.errorReason = fmt.Sprintf("parse %s at line %d: %s", parseErr.Path, parseErr.Line, parseErr.Reason)
		return finding
	}

	return finding
}

func postgresAuthSuccessFinding(scope postgresAuthScope, resolved pgauth.Resolved) postgresAuthFinding {
	finding := postgresAuthFinding{
		scope:  scope,
		status: StatusOK,
		source: resolved.Source,
		short:  "password from " + postgresAuthHumanSourceLabel(resolved.Source),
		details: []string{
			fmt.Sprintf("scope=%s  source=%s  user=%s", scope.root, resolved.Source.String(), resolved.User),
		},
	}
	finding.fullMessage = scope.display + ": " + finding.short
	switch resolved.Source {
	case pgauth.SourceProcessEnvGC, pgauth.SourceProcessEnvBeads:
		finding.status = StatusWarning
		finding.short = "password from parent shell env"
		finding.fullMessage = scope.display + ": " + finding.short
		finding.fixHint = fmt.Sprintf("parent-shell env works for the current shell only. Persist via %s (chmod 600) for non-interactive use.", scope.envPath)
	}
	return finding
}

func postgresAuthHumanSourceLabel(source pgauth.Source) string {
	switch source {
	case pgauth.SourceProjectedGC:
		return "projected env (GC_POSTGRES_PASSWORD)"
	case pgauth.SourceProjectedBeads:
		return "projected env (BEADS_POSTGRES_PASSWORD)"
	case pgauth.SourceProcessEnvGC:
		return "parent shell env (GC_POSTGRES_PASSWORD)"
	case pgauth.SourceScopeFile:
		return "scope file"
	case pgauth.SourceProcessEnvBeads:
		return "parent shell env (BEADS_POSTGRES_PASSWORD)"
	case pgauth.SourceCredentialsFileEnv:
		return "$BEADS_CREDENTIALS_FILE"
	case pgauth.SourceCredentialsFileHome:
		return "~/.config/beads/credentials"
	default:
		return source.String()
	}
}

func postgresAuthSourceForErrorPath(scope postgresAuthScope, path string) pgauth.Source {
	clean := filepath.Clean(path)
	switch {
	case clean == filepath.Clean(filepath.Join(scope.root, ".beads", ".env")):
		return pgauth.SourceScopeFile
	case clean == filepath.Clean(os.Getenv("BEADS_CREDENTIALS_FILE")):
		return pgauth.SourceCredentialsFileEnv
	case clean == filepath.Clean(pgauth.DefaultCredentialsPath()):
		return pgauth.SourceCredentialsFileHome
	default:
		return pgauth.SourceNone
	}
}

func postgresAuthSourceTier(source pgauth.Source) int {
	switch source {
	case pgauth.SourceProjectedGC:
		return 1
	case pgauth.SourceProjectedBeads:
		return 2
	case pgauth.SourceProcessEnvGC:
		return 3
	case pgauth.SourceScopeFile:
		return 4
	case pgauth.SourceProcessEnvBeads:
		return 5
	case pgauth.SourceCredentialsFileEnv:
		return 6
	case pgauth.SourceCredentialsFileHome:
		return 7
	default:
		return 0
	}
}

func sortPostgresAuthFindings(findings []postgresAuthFinding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].status != findings[j].status {
			return findings[i].status > findings[j].status
		}
		return findings[i].scope.root < findings[j].scope.root
	})
}

func firstFindingAtStatus(findings []postgresAuthFinding, status CheckStatus) postgresAuthFinding {
	for _, finding := range findings {
		if finding.status == status {
			return finding
		}
	}
	return findings[0]
}

func firstFixHintAtStatus(findings []postgresAuthFinding, status CheckStatus) string {
	for _, finding := range findings {
		if finding.status == status && finding.fixHint != "" {
			return finding.fixHint
		}
	}
	return ""
}

func (f postgresAuthFinding) detailRow() string {
	return fmt.Sprintf("%s %s — %s", postgresAuthStatusGlyph(f.status), f.scope.display, f.short)
}

func postgresAuthStatusGlyph(status CheckStatus) string {
	switch status {
	case StatusError:
		return "✗"
	case StatusWarning:
		return "⚠"
	default:
		return "✓"
	}
}

type postgresAuthExplainTier struct {
	number int
	label  string
	ident  string
}

func (f postgresAuthFinding) renderExplain(w io.Writer) {
	fmt.Fprintf(w, "PG-backed scope: %s  (host=%s:%s user=%s database=%s)\n", f.scope.display, f.scope.endpoint.Host, f.scope.endpoint.Port, f.scope.endpoint.User, f.scope.database) //nolint:errcheck // best-effort output
	winnerTier := postgresAuthSourceTier(f.source)
	for _, tier := range f.explainTiers() {
		status := "[no]"
		winner := false
		switch {
		case f.errorTier > 0:
			if tier.number == f.errorTier {
				status = "[ERR]"
			} else if tier.number > f.errorTier {
				status = "[skip]"
			}
		case winnerTier > 0:
			if tier.number == winnerTier {
				status = "[YES]"
				winner = true
			} else if tier.number > winnerTier {
				status = "[skip]"
			}
		}
		fmt.Fprintln(w, formatPostgresAuthExplainRow(tier.number, tier.label, tier.ident, status, winner)) //nolint:errcheck // best-effort output
		if tier.number == f.errorTier && f.errorReason != "" {
			fmt.Fprintf(w, "          %s\n", f.errorReason) //nolint:errcheck // best-effort output
		}
	}
	switch {
	case f.errorTier > 0:
		fmt.Fprintf(w, "\n  Source identifier: none   Resolution stopped at tier %d (see error above).\n", f.errorTier) //nolint:errcheck // best-effort output
	case winnerTier > 0:
		fmt.Fprintf(w, "\n  Source identifier: %s   Source position: tier %d of 7\n", f.source.String(), winnerTier) //nolint:errcheck // best-effort output
	default:
		fmt.Fprintln(w, "\n  Source identifier: none   No password resolvable. See: gc doctor (errors).") //nolint:errcheck // best-effort output
	}
}

func (f postgresAuthFinding) explainTiers() []postgresAuthExplainTier {
	hostPort := f.scope.endpoint.Host + ":" + f.scope.endpoint.Port
	return []postgresAuthExplainTier{
		{number: 1, label: "projected env", ident: "GC_POSTGRES_PASSWORD"},
		{number: 2, label: "projected env", ident: "BEADS_POSTGRES_PASSWORD"},
		{number: 3, label: "os.Getenv", ident: "GC_POSTGRES_PASSWORD"},
		{number: 4, label: "scope file", ident: f.scope.envPath + " BEADS_POSTGRES_PASSWORD"},
		{number: 5, label: "os.Getenv", ident: "BEADS_POSTGRES_PASSWORD"},
		{number: 6, label: "$BEADS_CREDENTIALS_FILE", ident: "[" + hostPort + "]"},
		{number: 7, label: "~/.config/beads/credentials", ident: "[" + hostPort + "]"},
	}
}

func formatPostgresAuthExplainRow(tier int, label, ident, status string, winner bool) string {
	prefix := fmt.Sprintf("  Tier %-2d %-14s %s", tier, label, ident)
	if len(prefix) < 69 {
		prefix += strings.Repeat(" ", 69-len(prefix))
	} else {
		prefix += " "
	}
	row := prefix + status
	if winner {
		row += "  ← winner"
	}
	return row
}
