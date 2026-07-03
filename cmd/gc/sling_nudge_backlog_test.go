package main

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
)

type backlogSlowStore struct {
	beads.Store
	latency time.Duration
	ops     int64
}

func (s *backlogSlowStore) tick() { atomic.AddInt64(&s.ops, 1); time.Sleep(s.latency) }

func (s *backlogSlowStore) List(beads.ListQuery) ([]beads.Bead, error) {
	s.tick()
	return []beads.Bead{{ID: "shadow-open", Status: "open", Metadata: map[string]string{"state": "queued"}}}, nil
}

func (s *backlogSlowStore) Create(b beads.Bead) (beads.Bead, error) {
	s.tick()
	if b.ID == "" {
		b.ID = "created-shadow"
	}
	b.Status = "open"
	return b, nil
}

func (s *backlogSlowStore) Get(id string) (beads.Bead, error) {
	s.tick()
	return beads.Bead{ID: id, Status: "open", Metadata: map[string]string{"state": "queued"}}, nil
}

func (s *backlogSlowStore) Close(string) error { s.tick(); return nil }

func (s *backlogSlowStore) SetMetadata(string, string, string) error { s.tick(); return nil }

func (s *backlogSlowStore) SetMetadataBatch(string, map[string]string) error { s.tick(); return nil }

func seedDeadBacklog(t *testing.T, cityPath string, n int) {
	t.Helper()
	now := time.Now()
	if err := withNudgeQueueState(cityPath, func(state *nudgeQueueState) error {
		for i := 0; i < n; i++ {
			id := time.Duration(i).String()
			state.Dead = append(state.Dead, queuedNudge{
				ID: "nudge-dead-" + id, BeadID: "bead-dead-" + id,
				Agent: "gascity/deployer", Source: "sling", Message: "backlog",
				CreatedAt: now.Add(-2 * time.Hour), DeadAt: now.Add(-2 * time.Hour),
				LastError: "expired",
			})
		}
		return nil
	}); err != nil {
		t.Fatalf("seeding backlog: %v", err)
	}
}

func timeEnqueue(t *testing.T, backlog int, latency time.Duration) time.Duration {
	t.Helper()
	cityPath := t.TempDir()
	seedDeadBacklog(t, cityPath, backlog)
	store := &backlogSlowStore{latency: latency}
	item := queuedNudge{ID: "nudge-new", Agent: "gascity/deployer", Source: "sling", Message: "Work slung. Check your hook."}
	start := time.Now()
	if err := enqueueQueuedNudgeWithStore(cityPath, beads.NudgesStore{Store: store}, item); err != nil {
		t.Fatalf("enqueue (backlog=%d): %v", backlog, err)
	}
	return time.Since(start)
}

// The foreground `--nudge` enqueue must be bounded regardless of nudge-queue
// backlog. Current code violates this: it runs O(backlog) serial store ops under
// the withNudgeQueueState flock with no aggregate deadline.
func TestSlingNudgeEnqueueBoundedByBacklog(t *testing.T) {
	const latency = 20 * time.Millisecond
	small := timeEnqueue(t, 40, latency)
	big := timeEnqueue(t, 160, latency)
	t.Logf("enqueue backlog=40 -> %v ; backlog=160 -> %v", small.Round(time.Millisecond), big.Round(time.Millisecond))
	// After the fix, both are capped by the maintenance budget, so 120 extra
	// backlog items add ~no foreground time. Current code adds ~120*4*20ms ~= 9.6s.
	if marginal := big - small; marginal > 2*time.Second {
		t.Fatalf("foreground enqueue grows with backlog: +120 items added %v (>2s). "+
			"Bound the withNudgeQueueState maintenance loops.", marginal.Round(time.Millisecond))
	}
}

// The maintenance budget must never lose items: anything not reached before
// the deadline has to stay exactly where it was for the next enqueue, the
// per-session poller, or the doctor reaper to pick up.
func TestSlingNudgeEnqueueBudgetCapPreservesAllItems(t *testing.T) {
	cityPath := t.TempDir()
	const backlog = 50
	// Slow enough that Terminalize's two store ops per item (200ms) can't
	// finish all 50 (10s of work) inside the 2s budget, but fast enough that
	// more than zero complete — this run must land strictly between "cap
	// never engaged" and "cap didn't actually bound anything".
	const latency = 100 * time.Millisecond
	now := time.Now()
	if err := withNudgeQueueState(cityPath, func(state *nudgeQueueState) error {
		for i := 0; i < backlog; i++ {
			id := time.Duration(i).String()
			state.Pending = append(state.Pending, queuedNudge{
				ID: "nudge-expired-" + id, BeadID: "bead-expired-" + id,
				Agent: "gascity/deployer", Source: "sling", Message: "backlog",
				CreatedAt: now.Add(-2 * time.Hour), DeliverAfter: now.Add(-2 * time.Hour),
				ExpiresAt: now.Add(-time.Hour),
			})
		}
		return nil
	}); err != nil {
		t.Fatalf("seeding backlog: %v", err)
	}

	store := &backlogSlowStore{latency: latency}
	item := queuedNudge{ID: "nudge-new", Agent: "gascity/deployer", Source: "sling", Message: "Work slung. Check your hook."}
	if err := enqueueQueuedNudgeWithStore(cityPath, beads.NudgesStore{Store: store}, item); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	var pendingCount, deadCount int
	var foundNewItem bool
	if err := withNudgeQueueState(cityPath, func(state *nudgeQueueState) error {
		pendingCount = len(state.Pending)
		deadCount = len(state.Dead)
		for _, p := range state.Pending {
			if p.ID == item.ID {
				foundNewItem = true
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("reading back state: %v", err)
	}

	if total := pendingCount + deadCount; total != backlog+1 {
		t.Fatalf("pending(%d) + dead(%d) = %d, want %d: budget cap must not drop items",
			pendingCount, deadCount, total, backlog+1)
	}
	if !foundNewItem {
		t.Fatalf("new item %q missing from pending: it must always be enqueued regardless of the maintenance budget", item.ID)
	}
	if deadCount == 0 {
		t.Fatalf("dead = 0: budget never engaged, so this run doesn't exercise the cap (want some, not all, of %d expired items processed)", backlog)
	}
	if deadCount == backlog {
		t.Fatalf("dead = %d (all): budget didn't bound anything in this run (want some items left unprocessed in pending)", backlog)
	}
	t.Logf("pending=%d dead=%d (of %d expired + 1 new)", pendingCount, deadCount, backlog)
}
