package contract

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/fsys"
)

func TestPreflightBlocksNativeOnMetadataPostgres(t *testing.T) {
	scope := "/city"
	checker := testPreflightChecker(preflightMetadataJSON(`{
		"backend": "postgres",
		"postgres_host": "db.example.com",
		"postgres_port": "5432",
		"postgres_user": "operator",
		"postgres_database": "gascity",
		"project_id": "gc-local"
	}`), PreflightBDContext{Backend: "postgres"}, "gc-local")

	result, err := checker.Check(scope)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	assertPreflightVerdict(t, result, PreflightVerdictBlocked, false)
	assertCheckOrder(t, result)
	assertCheckState(t, result, PreflightCheckMetadataBackend, PreflightCheckFail)
	assertCheckState(t, result, PreflightCheckBDContextAgreement, PreflightCheckPass)
	assertCheckState(t, result, PreflightCheckContractShape, PreflightCheckPass)
	assertPreflightReadOnly(t, checker.FS.(*fsys.Fake))
}

func TestPreflightRedactsPostgresDSN(t *testing.T) {
	scope := "/city"
	checker := testPreflightChecker(preflightMetadataJSON(`{
		"backend": "postgres",
		"postgres_dsn": "postgres://operator:swordfish@db.example.com/gascity",
		"project_id": "gc-local"
	}`), PreflightBDContext{Backend: "postgres"}, "gc-local")

	result, err := checker.Check(scope)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	assertPreflightVerdict(t, result, PreflightVerdictDegraded, false)
	assertCheckState(t, result, PreflightCheckMetadataBackend, PreflightCheckWarn)
	assertCheckState(t, result, PreflightCheckContractShape, PreflightCheckWarn)
	check := findPreflightCheck(t, result, PreflightCheckMetadataBackend)
	if check.Details.PostgresDSNRedacted != "postgres://[REDACTED]" {
		t.Fatalf("PostgresDSNRedacted = %q, want redacted DSN", check.Details.PostgresDSNRedacted)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if strings.Contains(string(data), "swordfish") || strings.Contains(string(data), "operator:swordfish") {
		t.Fatalf("serialized result leaked DSN secret: %s", data)
	}
}

func TestPreflightBlocksNativeOnContextDisagreement(t *testing.T) {
	scope := "/city"
	checker := testPreflightChecker(preflightMetadataJSON(`{
		"backend": "dolt",
		"dolt_mode": "server",
		"dolt_database": "gascity",
		"project_id": "gc-local"
	}`), PreflightBDContext{Backend: "postgres"}, "gc-local")

	result, err := checker.Check(scope)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	assertPreflightVerdict(t, result, PreflightVerdictBlocked, false)
	assertCheckState(t, result, PreflightCheckBDContextAgreement, PreflightCheckFail)
}

// An UNREACHABLE bd context (e.g. a non-git city root where `bd context` cannot
// run) is not evidence of a backend disagreement — it only means the native
// store's bd-context cross-checks cannot be verified. It must DEGRADE eligibility
// (operator opt-in) rather than hard-BLOCK it, so the bd-context-derived checks
// report WARN, not FAIL. (A real disagreement, with a readable bd context, still
// blocks — see TestPreflightBlocksNativeOnContextDisagreement.)
func TestPreflightDegradesNativeOnUnreachableBDContext(t *testing.T) {
	scope := "/city"
	fs := fsys.NewFake()
	fs.Dirs[filepath.Join(scope, ".beads")] = true
	fs.Files[filepath.Join(scope, ".beads", "metadata.json")] = []byte(`{
		"backend": "dolt",
		"dolt_mode": "server",
		"dolt_database": "gascity",
		"project_id": "gc-local"
	}`)
	checker := PreflightChecker{
		FS:                  fs,
		Provider:            "bd",
		BeadsLibraryVersion: "1.0.4",
		BDContext: func(string) (PreflightBDContext, error) {
			return PreflightBDContext{}, errors.New("bd context unavailable: not a git repository")
		},
		DatabaseProjectID: func(string) (string, bool, error) {
			return "gc-local", true, nil
		},
	}

	result, err := checker.Check(scope)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	// Unreachable (not disagreeing) bd context => DEGRADED + opt-in, never BLOCKED.
	assertPreflightVerdict(t, result, PreflightVerdictDegraded, false)
	assertCheckState(t, result, PreflightCheckBDContextAgreement, PreflightCheckWarn)
	assertCheckState(t, result, PreflightCheckDoltModeSafe, PreflightCheckWarn)
	assertCheckState(t, result, PreflightCheckVersionCompat, PreflightCheckWarn)
}

func TestPreflightBlocksNativeOnIdentityMismatch(t *testing.T) {
	scope := "/city"
	checker := testPreflightChecker(preflightMetadataJSON(`{
		"backend": "dolt",
		"dolt_mode": "server",
		"dolt_database": "gascity",
		"project_id": "metadata-id"
	}`), PreflightBDContext{Backend: "dolt", DoltMode: "server"}, "database-id")

	result, err := checker.Check(scope)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	assertPreflightVerdict(t, result, PreflightVerdictBlocked, false)
	assertCheckState(t, result, PreflightCheckIdentityMatch, PreflightCheckFail)
	check := findPreflightCheck(t, result, PreflightCheckIdentityMatch)
	if check.Details.MetadataProjectID != "metadata-id" || check.Details.DBProjectID != "database-id" {
		t.Fatalf("identity details = %+v, want both project ids visible", check.Details)
	}
}

func TestPreflightPassesOnHealthyDolt(t *testing.T) {
	scope := "/city"
	checker := testPreflightChecker(preflightMetadataJSON(`{
		"backend": "dolt",
		"dolt_mode": "server",
		"dolt_database": "gascity",
		"project_id": "gc-local"
	}`), PreflightBDContext{Backend: "dolt", DoltMode: "server"}, "gc-local")

	result, err := checker.Check(scope)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	assertPreflightVerdict(t, result, PreflightVerdictEligible, true)
	for _, check := range result.Checks {
		if check.State != PreflightCheckPass {
			t.Fatalf("check %s state = %s, want PASS in healthy case: %+v", check.ID, check.State, result.Checks)
		}
	}
	if result.Fallback != PreflightFallbackNone {
		t.Fatalf("Fallback = %q, want none", result.Fallback)
	}
}

func TestPreflightAcceptsExecGcBeadsBdProviderPath(t *testing.T) {
	scope := "/city"
	checker := testPreflightChecker(preflightMetadataJSON(`{
		"backend": "dolt",
		"dolt_mode": "server",
		"dolt_database": "gascity",
		"project_id": "gc-local"
	}`), PreflightBDContext{Backend: "dolt", DoltMode: "server"}, "gc-local")
	checker.Provider = "exec:/tmp/gc-beads-bd.sh"

	result, err := checker.Check(scope)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	assertPreflightVerdict(t, result, PreflightVerdictEligible, true)
	assertCheckState(t, result, PreflightCheckProviderContract, PreflightCheckPass)
}

func TestProviderUsesBDContract(t *testing.T) {
	tests := []struct {
		provider string
		want     bool
	}{
		{provider: "", want: true},
		{provider: "bd", want: true},
		{provider: " file ", want: false},
		{provider: "exec:gc-beads-bd", want: true},
		{provider: "exec:/tmp/gc-beads-bd", want: true},
		{provider: "exec:/tmp/gc-beads-bd.sh", want: true},
		{provider: "exec:/tmp/gc-beads-k8s", want: false},
		{provider: "exec:/tmp/custom", want: false},
	}
	for _, tt := range tests {
		if got := ProviderUsesBDContract(tt.provider); got != tt.want {
			t.Fatalf("ProviderUsesBDContract(%q) = %v, want %v", tt.provider, got, tt.want)
		}
	}
}

func TestPreflightRespectsSkipOverrideAsRecoveryOnly(t *testing.T) {
	t.Setenv("BEADS_SKIP_IDENTITY_CHECK", "1")
	scope := "/city"
	checker := testPreflightChecker(preflightMetadataJSON(`{
		"backend": "dolt",
		"dolt_mode": "server",
		"dolt_database": "gascity",
		"project_id": "metadata-id"
	}`), PreflightBDContext{Backend: "dolt", DoltMode: "server"}, "database-id")

	result, err := checker.Check(scope)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	assertPreflightVerdict(t, result, PreflightVerdictBlocked, false)
	assertCheckState(t, result, PreflightCheckIdentityMatch, PreflightCheckFail)
}

func TestPreflightWarnsWhenDatabaseIdentityUnavailable(t *testing.T) {
	scope := "/city"
	checker := testPreflightChecker(preflightMetadataJSON(`{
		"backend": "dolt",
		"dolt_mode": "server",
		"dolt_database": "gascity",
		"project_id": "metadata-id"
	}`), PreflightBDContext{Backend: "dolt", DoltMode: "server"}, "")
	checker.DatabaseProjectID = func(string) (string, bool, error) {
		return "", false, errors.New("dial dolt")
	}

	result, err := checker.Check(scope)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	assertPreflightVerdict(t, result, PreflightVerdictDegraded, false)
	assertCheckState(t, result, PreflightCheckIdentityMatch, PreflightCheckWarn)
}

// unreachableBDContextDoltChecker builds a checker for a dolt scope whose
// `bd context` cannot run (the non-git store root that motivates the whole
// server-verify path). Callers set DatabaseProjectID / DoltServerReachable to
// pick the identity + server-probe outcome under test.
func unreachableBDContextDoltChecker(scope string) PreflightChecker {
	fs := fsys.NewFake()
	fs.Dirs[filepath.Join(scope, ".beads")] = true
	fs.Files[filepath.Join(scope, ".beads", "metadata.json")] = []byte(`{
		"backend": "dolt",
		"dolt_mode": "server",
		"dolt_database": "gascity",
		"project_id": "gc-local"
	}`)
	return PreflightChecker{
		FS:                  fs,
		Provider:            "bd",
		BeadsLibraryVersion: "1.0.4",
		BDContext: func(string) (PreflightBDContext, error) {
			return PreflightBDContext{}, errors.New("bd context unavailable: not a git repository")
		},
	}
}

// TestPreflightEligibleViaServerVerifyWhenIdentityUnconfirmed is the RED→GREEN
// guard for the native-store restore: bd context is unreachable AND the direct
// project_id probe cannot CONFIRM project_id (it WARNs — the server has no
// _project_id row for the probe to read), so no identity-PASS upgrade is
// available. A direct SELECT 1 probe nonetheless proves the Dolt backend is
// reachable in SERVER MODE, which is the real safety property the bd
// cross-checks proxy, so the scope is ELIGIBLE (native identity is re-verified
// fail-closed at native-open). Before the fix this scope was INELIGIBLE
// (DEGRADED → per-call bd).
func TestPreflightEligibleViaServerVerifyWhenIdentityUnconfirmed(t *testing.T) {
	scope := "/city"
	checker := unreachableBDContextDoltChecker(scope)
	// The project_id probe reaches nothing it can confirm project_id from.
	checker.DatabaseProjectID = func(string) (string, bool, error) {
		return "", false, nil
	}
	// ...but a direct SELECT 1 probe answers, which only a live SERVER-mode Dolt
	// can do (an embedded Dolt exposes no listener).
	checker.DoltServerReachable = func(string) (bool, error) {
		return true, nil
	}

	result, err := checker.Check(scope)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	assertPreflightVerdict(t, result, PreflightVerdictEligible, true)
	// The bd-context cross-checks and identity_match still WARN; the verdict is
	// upgraded on the strength of the direct server-mode probe.
	assertCheckState(t, result, PreflightCheckBDContextAgreement, PreflightCheckWarn)
	assertCheckState(t, result, PreflightCheckDoltModeSafe, PreflightCheckWarn)
	assertCheckState(t, result, PreflightCheckVersionCompat, PreflightCheckWarn)
	assertCheckState(t, result, PreflightCheckIdentityMatch, PreflightCheckWarn)
	if !result.NativeEligibleViaServerVerify {
		t.Errorf("NativeEligibleViaServerVerify = false, want true on the server-verified upgrade")
	}
}

// TestPreflightServerVerifyIsFailClosed pins the blast-radius crux: the direct
// server-mode upgrade fires ONLY on a clean (true, nil) probe. A nil reader, an
// unreachable server, a probe that declines to verify, or a late error all keep
// the scope DEGRADED on the per-call bd store — the native store is never
// activated against an embedded or unreachable Dolt.
func TestPreflightServerVerifyIsFailClosed(t *testing.T) {
	cases := []struct {
		name  string
		probe func(string) (bool, error)
	}{
		{name: "no probe configured", probe: nil},
		{name: "server unreachable", probe: func(string) (bool, error) { return false, errors.New("dial dolt: connection refused") }},
		{name: "server not verified", probe: func(string) (bool, error) { return false, nil }},
		{name: "verified but late error", probe: func(string) (bool, error) { return true, errors.New("probe closed mid-verify") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope := "/city"
			checker := unreachableBDContextDoltChecker(scope)
			checker.DatabaseProjectID = func(string) (string, bool, error) { return "", false, nil }
			checker.DoltServerReachable = tc.probe

			result, err := checker.Check(scope)
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			assertPreflightVerdict(t, result, PreflightVerdictDegraded, false)
			if result.NativeEligibleViaServerVerify {
				t.Errorf("NativeEligibleViaServerVerify = true, want false when the server probe did not cleanly verify")
			}
		})
	}
}

// TestPreflightServerVerifyNeverOverridesGenuineFail proves a reachable server
// cannot rescue a scope with a real blocker: a postgres backend gc does not
// implement FAILs metadata_backend/contract_shape, so the verdict is BLOCKED and
// the server-mode upgrade — which only ever acts on a DEGRADED verdict — never
// runs.
func TestPreflightServerVerifyNeverOverridesGenuineFail(t *testing.T) {
	scope := "/city"
	fs := fsys.NewFake()
	fs.Dirs[filepath.Join(scope, ".beads")] = true
	fs.Files[filepath.Join(scope, ".beads", "metadata.json")] = []byte(`{
		"backend": "postgres",
		"project_id": "gc-local"
	}`)
	checker := PreflightChecker{
		FS:                  fs,
		Provider:            "bd",
		BeadsLibraryVersion: "1.0.4",
		BDContext: func(string) (PreflightBDContext, error) {
			return PreflightBDContext{}, errors.New("bd context unavailable: not a git repository")
		},
		DoltServerReachable: func(string) (bool, error) { return true, nil },
	}

	result, err := checker.Check(scope)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	assertPreflightVerdict(t, result, PreflightVerdictBlocked, false)
	if result.NativeEligibleViaServerVerify {
		t.Errorf("NativeEligibleViaServerVerify = true, want false when a genuine FAIL blocks the scope")
	}
}

// TestPreflightServerVerifyStillBlocksOnIdentityMismatch guards the one identity
// outcome that must never be deferred to native-open: a CONFIRMED project_id
// MISMATCH is a genuine cross-project FAIL, so even a reachable server keeps the
// scope BLOCKED. (An unconfirmed identity is a WARN and may defer; a mismatch is
// a FAIL and may not.)
func TestPreflightServerVerifyStillBlocksOnIdentityMismatch(t *testing.T) {
	scope := "/city"
	fs := fsys.NewFake()
	fs.Dirs[filepath.Join(scope, ".beads")] = true
	fs.Files[filepath.Join(scope, ".beads", "metadata.json")] = []byte(`{
		"backend": "dolt",
		"dolt_mode": "server",
		"dolt_database": "gascity",
		"project_id": "metadata-id"
	}`)
	checker := PreflightChecker{
		FS:                  fs,
		Provider:            "bd",
		BeadsLibraryVersion: "1.0.4",
		BDContext: func(string) (PreflightBDContext, error) {
			return PreflightBDContext{}, errors.New("bd context unavailable: not a git repository")
		},
		DatabaseProjectID:   func(string) (string, bool, error) { return "database-id", true, nil },
		DoltServerReachable: func(string) (bool, error) { return true, nil },
	}

	result, err := checker.Check(scope)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	assertPreflightVerdict(t, result, PreflightVerdictBlocked, false)
	assertCheckState(t, result, PreflightCheckIdentityMatch, PreflightCheckFail)
	if result.NativeEligibleViaServerVerify {
		t.Errorf("NativeEligibleViaServerVerify = true, want false on a confirmed identity mismatch")
	}
}

func TestPreflightUnreadableScopeReturnsError(t *testing.T) {
	scope := "/city"
	fs := fsys.NewFake()
	fs.Errors[filepath.Join(scope, ".beads", "metadata.json")] = os.ErrPermission
	checker := PreflightChecker{
		FS:                  fs,
		Provider:            "bd",
		BeadsLibraryVersion: "1.0.4",
		BDContext: func(string) (PreflightBDContext, error) {
			return PreflightBDContext{Backend: "dolt", DoltMode: "server", BDVersion: "1.0.4", SchemaVersion: 1}, nil
		},
		DatabaseProjectID: func(string) (string, bool, error) {
			return "gc-local", true, nil
		},
	}

	if _, err := checker.Check(scope); err == nil || !strings.Contains(err.Error(), "read preflight metadata") {
		t.Fatalf("Check() error = %v, want unreadable metadata error", err)
	}
	assertPreflightReadOnly(t, fs)
}

func testPreflightChecker(metadata string, ctx PreflightBDContext, dbProjectID string) PreflightChecker {
	scope := "/city"
	fs := fsys.NewFake()
	fs.Dirs[filepath.Join(scope, ".beads")] = true
	fs.Files[filepath.Join(scope, ".beads", "metadata.json")] = []byte(metadata)
	if ctx.BDVersion == "" {
		ctx.BDVersion = "1.0.4"
	}
	if ctx.SchemaVersion == 0 {
		ctx.SchemaVersion = 1
	}
	return PreflightChecker{
		FS:                  fs,
		Provider:            "bd",
		BeadsLibraryVersion: "1.0.4",
		BDContext: func(string) (PreflightBDContext, error) {
			return ctx, nil
		},
		DatabaseProjectID: func(string) (string, bool, error) {
			return dbProjectID, dbProjectID != "", nil
		},
	}
}

func preflightMetadataJSON(body string) string {
	return strings.ReplaceAll(body, "\t", "")
}

func assertPreflightVerdict(t *testing.T, result PreflightResult, want PreflightVerdict, wantEligible bool) {
	t.Helper()
	if result.Verdict != want {
		t.Fatalf("Verdict = %q, want %q; checks=%+v", result.Verdict, want, result.Checks)
	}
	if result.NativeStoreEligible != wantEligible {
		t.Fatalf("NativeStoreEligible = %v, want %v", result.NativeStoreEligible, wantEligible)
	}
}

func assertCheckOrder(t *testing.T, result PreflightResult) {
	t.Helper()
	want := []PreflightCheckID{
		PreflightCheckProviderContract,
		PreflightCheckMetadataBackend,
		PreflightCheckBDContextAgreement,
		PreflightCheckDoltModeSafe,
		PreflightCheckIdentityMatch,
		PreflightCheckVersionCompat,
		PreflightCheckContractShape,
	}
	if len(result.Checks) != len(want) {
		t.Fatalf("Checks len = %d, want %d: %+v", len(result.Checks), len(want), result.Checks)
	}
	for i, id := range want {
		if result.Checks[i].ID != id {
			t.Fatalf("Checks[%d].ID = %q, want %q; checks=%+v", i, result.Checks[i].ID, id, result.Checks)
		}
	}
}

func assertCheckState(t *testing.T, result PreflightResult, id PreflightCheckID, want PreflightCheckState) {
	t.Helper()
	check := findPreflightCheck(t, result, id)
	if check.State != want {
		t.Fatalf("check %s state = %q, want %q; check=%+v", id, check.State, want, check)
	}
}

func findPreflightCheck(t *testing.T, result PreflightResult, id PreflightCheckID) PreflightCheckResult {
	t.Helper()
	for _, check := range result.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("missing check %s in %+v", id, result.Checks)
	return PreflightCheckResult{}
}

func assertPreflightReadOnly(t *testing.T, fs *fsys.Fake) {
	t.Helper()
	for _, call := range fs.Calls {
		switch call.Method {
		case "WriteFile", "MkdirAll", "Rename", "Remove", "Chmod":
			t.Fatalf("preflight checker must be read-only; saw %s on %s", call.Method, call.Path)
		}
	}
}

// TestCheckVersionCompatSourceBuild verifies that a source (local-path/replace)
// build of the linked beads library — which reports "(devel)" as its module
// version — does not take the native store offline. The schema version is the
// real compatibility signal; only a *confirmed* version mismatch should fail.
func TestCheckVersionCompatSourceBuild(t *testing.T) {
	validCtx := func(bdVersion string) PreflightBDContext {
		return PreflightBDContext{Backend: "dolt", DoltMode: "server", BDVersion: bdVersion, SchemaVersion: 50}
	}
	tests := []struct {
		name       string
		libVersion string
		ctx        PreflightBDContext
		want       PreflightCheckState
	}{
		{"source build reports (devel) — schema is the signal, pass", "(devel)", validCtx("1.0.5"), PreflightCheckPass},
		{"confirmed version mismatch still fails", "1.0.5", validCtx("1.0.4"), PreflightCheckFail},
		{"matching versions pass", "1.0.5", validCtx("1.0.5"), PreflightCheckPass},
		{"missing bd version is unconfirmable — warn", "1.0.5", validCtx(""), PreflightCheckWarn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := PreflightChecker{BeadsLibraryVersion: tt.libVersion}
			got := c.checkVersionCompat(tt.ctx, nil)
			if got.ID != PreflightCheckVersionCompat {
				t.Fatalf("ID = %q, want %q", got.ID, PreflightCheckVersionCompat)
			}
			if got.State != tt.want {
				t.Fatalf("state = %q, want %q (summary: %q)", got.State, tt.want, got.Summary)
			}
		})
	}
}
