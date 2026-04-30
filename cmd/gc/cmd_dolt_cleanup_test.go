package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/fsys"
)

func TestCleanupReportJSONShape(t *testing.T) {
	r := CleanupReport{
		Schema: "gc.dolt.cleanup.v1",
		Port: CleanupPortReport{
			Resolved: 28231,
			Source:   "/city/.beads/dolt-server.port",
			Fallback: false,
		},
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(data)

	wantKeys := []string{
		`"schema":"gc.dolt.cleanup.v1"`,
		`"port":{`,
		`"rigs_protected":[]`,
		`"dropped":{`,
		`"purge":{`,
		`"reaped":{`,
		`"summary":{`,
		`"errors":[]`,
	}
	for _, key := range wantKeys {
		if !strings.Contains(got, key) {
			t.Errorf("JSON missing %q\nfull JSON:\n%s", key, got)
		}
	}
}

func TestRunDoltCleanup_JSONOutputsResolvedPort(t *testing.T) {
	fs := fsys.NewFake()
	fs.Files["/city/.beads/dolt-server.port"] = []byte("28231\n")

	rigs := []resolverRig{{Name: "hq", Path: "/city", HQ: true}}

	var stdout, stderr bytes.Buffer
	opts := cleanupOptions{
		Flag:     "",
		CityPort: 0,
		Rigs:     rigs,
		FS:       fs,
		JSON:     true,
		Probe:    false, // skip TCP probe in unit tests
	}
	code := runDoltCleanup(opts, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runDoltCleanup exit=%d, stderr=%q", code, stderr.String())
	}

	var r CleanupReport
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("Unmarshal stdout: %v\nstdout: %s", err, stdout.String())
	}
	if r.Schema != "gc.dolt.cleanup.v1" {
		t.Errorf("Schema = %q", r.Schema)
	}
	if r.Port.Resolved != 28231 {
		t.Errorf("Port.Resolved = %d, want 28231", r.Port.Resolved)
	}
	if r.Port.Fallback {
		t.Errorf("Port.Fallback = true, want false")
	}
}

func TestRunDoltCleanup_HumanOutputShowsPortAndFallbackWarning(t *testing.T) {
	fs := fsys.NewFake()

	var stdout, stderr bytes.Buffer
	opts := cleanupOptions{
		FS:    fs,
		JSON:  false,
		Probe: false,
	}
	code := runDoltCleanup(opts, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d, stderr=%s", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "3307") {
		t.Errorf("stdout missing legacy port 3307: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "fallback") && !strings.Contains(strings.ToLower(out), "legacy default") {
		t.Errorf("stdout missing fallback indicator: %s", out)
	}
}

func TestRunDoltCleanup_FlagOverridesEverything(t *testing.T) {
	fs := fsys.NewFake()
	fs.Files["/city/.beads/dolt-server.port"] = []byte("28231\n")

	var stdout, stderr bytes.Buffer
	opts := cleanupOptions{
		Flag:     "9999",
		CityPort: 4242,
		Rigs:     []resolverRig{{Name: "hq", Path: "/city", HQ: true}},
		FS:       fs,
		JSON:     true,
		Probe:    false,
	}
	code := runDoltCleanup(opts, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	var r CleanupReport
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if r.Port.Resolved != 9999 {
		t.Errorf("Port.Resolved = %d, want 9999", r.Port.Resolved)
	}
	if r.Port.Source != "--port flag" {
		t.Errorf("Port.Source = %q", r.Port.Source)
	}
}
