package main

import (
	"errors"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
)

func TestResolveTaskWorkDirToleratesPartial(t *testing.T) {
	dir := t.TempDir()
	rows := []beads.Bead{{
		ID:       "w-1",
		Type:     "task",
		Status:   "in_progress",
		Assignee: "worker",
		Metadata: map[string]string{"work_dir": dir},
	}}
	store := &partialGuardListStore{
		MemStore: beads.NewMemStore(),
		rows:     rows,
		err:      &beads.PartialResultError{Op: "bd list", Err: errors.New("graph leg down")},
	}

	got := resolveTaskWorkDir(store, "worker")
	if got != dir {
		t.Fatalf("resolveTaskWorkDir = %q, want %q (partial must not downgrade work_dir to default)", got, dir)
	}
}

func TestResolveTaskOptionOverridesToleratesPartial(t *testing.T) {
	rp := &config.ResolvedProvider{
		OptionsSchema: []config.ProviderOption{{
			Key:     "model",
			Type:    "select",
			Choices: []config.OptionChoice{{Value: "opus", FlagArgs: []string{"--model", "opus"}}},
		}},
	}
	rows := []beads.Bead{{
		ID:       "w-1",
		Type:     "task",
		Status:   "in_progress",
		Assignee: "worker",
		Metadata: map[string]string{dispatchOptionMetadataKey("model"): "opus"},
	}}
	store := &partialGuardListStore{
		MemStore: beads.NewMemStore(),
		rows:     rows,
		err:      &beads.PartialResultError{Op: "bd list", Err: errors.New("graph leg down")},
	}

	got := resolveTaskOptionOverrides(store, rp, "worker")
	if got["model"] != "opus" {
		t.Fatalf("resolveTaskOptionOverrides = %+v, want model=opus (partial must not drop overrides)", got)
	}
}
