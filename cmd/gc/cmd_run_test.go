package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFormula(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hello.toml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunOneShotRejectsNonToml(t *testing.T) {
	var out, errOut bytes.Buffer
	err := runOneShot(&out, &errOut, "graph.lumen", nil, nil, false, false, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("expected unsupported-file-type error, got %v", err)
	}
}

func TestRunOneShotRejectsEmptyFormula(t *testing.T) {
	formula := writeFormula(t, "   \n")
	var out, errOut bytes.Buffer
	if err := runOneShot(&out, &errOut, formula, nil, nil, false, true, t.TempDir()); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("expected empty-formula error, got %v", err)
	}
}

func TestRunOneShotRejectsInvalidTOML(t *testing.T) {
	formula := writeFormula(t, "[[[not valid")
	var out, errOut bytes.Buffer
	if err := runOneShot(&out, &errOut, formula, nil, nil, false, true, t.TempDir()); err == nil || !strings.Contains(err.Error(), "not valid TOML") {
		t.Fatalf("expected invalid-TOML error, got %v", err)
	}
}

func TestRunOneShotDryRunPrintsBoundCityAndReaps(t *testing.T) {
	repo := t.TempDir()
	formula := writeFormula(t, "# hello\n")

	var out, errOut bytes.Buffer
	if err := runOneShot(&out, &errOut, formula, []string{"w=" + repo}, nil, false, true, t.TempDir()); err != nil {
		t.Fatalf("dry-run: %v\nstderr: %s", err, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"--- city.toml ---", "--- .gc/site.toml ---", "rig \"w\" ->"} {
		if !strings.Contains(got, want) {
			t.Errorf("dry-run output missing %q\n---\n%s", want, got)
		}
	}
	// The printed root should have been reaped by the deferred Teardown.
	root := cityRootFromOutput(t, got)
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("dry-run should reap the city dir %q, stat err=%v", root, err)
	}
}

func TestRunOneShotDefaultKeepsDirAndReportsNotWired(t *testing.T) {
	formula := writeFormula(t, "# hello\n")
	var out, errOut bytes.Buffer
	err := runOneShot(&out, &errOut, formula, nil, nil, false, false, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not wired") {
		t.Fatalf("non-dry-run should return a not-wired error, got %v", err)
	}
	root := cityRootFromOutput(t, out.String())
	if _, statErr := os.Stat(root); statErr != nil {
		t.Errorf("non-dry-run should keep the city dir %q: %v", root, statErr)
	}
	_ = os.RemoveAll(root)
}

func cityRootFromOutput(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "transient city") {
			fields := strings.Fields(line)
			return fields[len(fields)-1] // the path is the last token ("...city: /p" or "...city at /p")
		}
	}
	t.Fatalf("no city root in output:\n%s", out)
	return ""
}

func TestParseFolderFlags(t *testing.T) {
	repo := t.TempDir()
	t.Run("valid", func(t *testing.T) {
		got, err := parseFolderFlags([]string{"w=" + repo})
		if err != nil {
			t.Fatal(err)
		}
		want, _ := filepath.EvalSymlinks(repo)
		if len(got) != 1 || got[0].Name != "w" || got[0].Path != want {
			t.Errorf("unexpected folders: %+v (want path %s)", got, want)
		}
		if got[0].Prefix != "" {
			t.Errorf("prefix should be left to the forge, got %q", got[0].Prefix)
		}
	})
	t.Run("missing equals", func(t *testing.T) {
		if _, err := parseFolderFlags([]string{"justname"}); err == nil {
			t.Error("expected error for missing =")
		}
	})
	t.Run("nonexistent dir", func(t *testing.T) {
		if _, err := parseFolderFlags([]string{"w=/no/such/dir/here"}); err == nil {
			t.Error("expected error for nonexistent directory")
		}
	})
	t.Run("duplicate name", func(t *testing.T) {
		if _, err := parseFolderFlags([]string{"w=" + repo, "w=" + repo}); err == nil {
			t.Error("expected error for duplicate folder name")
		}
	})
}

func TestParseKeyValueFlags(t *testing.T) {
	got, err := parseKeyValueFlags([]string{"a=1", "b=two"})
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != "1" || got["b"] != "two" {
		t.Errorf("unexpected vars: %+v", got)
	}
	if _, err := parseKeyValueFlags([]string{"nokey"}); err == nil {
		t.Error("expected error for key with no =")
	}
}
