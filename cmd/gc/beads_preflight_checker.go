package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gastownhall/gascity/internal/beads/contract"
	"github.com/gastownhall/gascity/internal/fsys"
)

func newBeadsPreflightChecker(cityPath, provider string) contract.PreflightChecker {
	return contract.PreflightChecker{
		FS:                        fsys.OSFS{},
		Provider:                  provider,
		BDContext:                 preflightBDContextReader(cityPath),
		DatabaseProjectID:         preflightDatabaseProjectIDReader(cityPath),
		DeferIdentityToNativeOpen: preflightIdentityDeferredReader(cityPath),
		DoltServerReachable:       preflightDoltServerReachableReader(cityPath),
	}
}

// preflightDoltServerReachableReader verifies a scope's Dolt backend is
// reachable in SERVER MODE with a responsive schema, by a direct SQL probe to
// the configured host:port. It is the fail-closed evidence used to activate the
// native store when bd context is unreachable and the direct root/plaintext
// identity probe cannot confirm project_id: opening a TCP MySQL connection to
// the configured target and answering SELECT 1 is only possible against a live
// Dolt server — an embedded Dolt exposes no listener — so a clean probe proves
// server mode, reachability, and that the named database responds. beadslib
// re-verifies project identity fail-closed at native-open time. Any target
// resolution, connection, ping, or query failure returns false (with the error
// for the caller's diagnostics), keeping the scope on the per-call BdStore.
func preflightDoltServerReachableReader(cityPath string) func(scope string) (bool, error) {
	return func(scope string) (bool, error) {
		target, ok, err := canonicalScopeDoltTarget(cityPath, scope)
		if err != nil || !ok {
			return false, err
		}
		db, err := managedDoltOpenDatabase(target.Host, target.Port, target.User, target.Database)
		if err != nil {
			return false, err
		}
		defer db.Close() //nolint:errcheck // read-only best-effort close

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			return false, err
		}
		var one int
		if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
			return false, err
		}
		return true, nil
	}
}

func preflightBDContextReader(cityPath string) func(scope string) (contract.PreflightBDContext, error) {
	return func(scope string) (contract.PreflightBDContext, error) {
		out, err := bdCommandRunnerForCity(cityPath)(scope, "bd", "context", "--json")
		if err != nil {
			return contract.PreflightBDContext{}, err
		}
		var raw struct {
			Backend       string `json:"backend"`
			DoltMode      string `json:"dolt_mode"`
			BDVersion     string `json:"bd_version"`
			SchemaVersion int    `json:"schema_version"`
		}
		if err := json.Unmarshal(out, &raw); err != nil {
			return contract.PreflightBDContext{}, fmt.Errorf("parse bd context --json: %w", err)
		}
		return contract.PreflightBDContext{
			Backend:       raw.Backend,
			DoltMode:      raw.DoltMode,
			BDVersion:     raw.BDVersion,
			SchemaVersion: raw.SchemaVersion,
		}, nil
	}
}

// preflightIdentityDeferredReader reports whether a scope resolves to an
// external Dolt endpoint (e.g. a hosted beads-gateway). The direct root/plaintext
// project_id probe cannot authenticate such endpoints, so when it comes back
// unconfirmed the identity check defers to beadslib's native-open verification
// (which authenticates via the credential command and refuses to connect on a
// _project_id mismatch) instead of degrading the scope off the native store.
func preflightIdentityDeferredReader(cityPath string) func(scope string) bool {
	return func(scope string) bool {
		target, ok, err := canonicalScopeDoltTarget(cityPath, scope)
		if err != nil || !ok {
			return false
		}
		return target.External
	}
}

func preflightDatabaseProjectIDReader(cityPath string) func(scope string) (string, bool, error) {
	return func(scope string) (string, bool, error) {
		target, ok, err := canonicalScopeDoltTarget(cityPath, scope)
		if err != nil || !ok {
			return "", false, err
		}
		db, err := managedDoltOpenDatabase(target.Host, target.Port, target.User, target.Database)
		if err != nil {
			return "", false, err
		}
		defer db.Close() //nolint:errcheck // read-only best-effort close

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			return "", false, err
		}
		return readDatabaseProjectID(ctx, db)
	}
}
