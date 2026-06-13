package coordrouter

import (
	"github.com/gastownhall/gascity/internal/beads"
)

// This file declares the per-class store seams for the work-vs-infrastructure
// split (engdocs/design/beads-work-infra-split.md). Each [coordclass.Class] gets
// a named interface naming the surface its owning subsystem actually uses today,
// so a faster backend can later slot in behind the same interface.
//
// P0 contract (this commit): every interface is the MINIMAL set of operations a
// real current consumer performs on that class's beads — a pure extract-interface
// refactor with no speculative methods. For the five non-graph classes that means
// a faithful SUBSET of [beads.Store], proven by the compile-time assertions below:
// the bd-delegating "first implementation" of each is therefore any beads.Store,
// with no wrapper code. ClassGraph is the exception — its surface is the
// graph-apply capability, not a beads.Store method set — so it gets a real
// bd-delegating adapter (see bdgraphstore.go).
//
// The interfaces are deliberately thin. They GROW toward the richer,
// domain-shaped seams sketched in the design doc (FindOrCreateByKey, recency
// sweeps, ensure-by-key, change feeds) only as a consumer is migrated behind the
// seam in later phases — never ahead of a caller. Each interface's doc comment
// names that growth path so it is tracked, not lost.
//
// Class (which backend owns the data) is orthogonal to beads.StorageClass (which
// physical tier a bead lives in); these interfaces carry no tier knob.

// WorkStore is the real backlog: tasks, epics, bugs, features, merge-requests,
// and user/sling convoys (coordclass.ClassWork). It is a marker alias for
// [beads.Store], NOT a new interface: beads.Bead stays the wire type, so the
// federation surface, the OpenAPI spec, and the dashboard see zero churn. The
// alias documents intent at the seam without segregating the work backend, which
// remains the full store.
type WorkStore = beads.Store

// GraphStore is the persistence seam for ClassGraph — the formula-v2 execution
// engine's topology and control lane (molecule/step/gate/control beads, wisp
// roots, convergence roots, spec sidecars, synthetic convoys). This is the bead
// explosion the split primarily targets. Owner: internal/dispatch +
// internal/molecule + internal/formula.
//
// P0 surface: the atomic graph pour, the one graph operation external consumers
// invoke today (via beads.GraphApplyFor(store).ApplyGraphPlan at
// internal/molecule/graph_apply.go:60 and internal/dispatch/ralph.go:596). It
// embeds [beads.GraphApplyStore] rather than redeclaring the method so the pour
// contract has a single definition. The first bd-delegating implementation is
// [BdGraphStore]; a tier-aware backend additionally satisfies the optional
// [beads.StorageGraphApplyStore].
//
// Growth path (P2+, each promoted only when a graph consumer is migrated behind
// this seam — today every one of these is performed through the generic
// beads.Store, so none belongs in the interface yet):
//
//   - GetNode(ctx, id) — replaces the engine's generic store.Get of a node.
//   - ListNodesByRoot(ctx, rootID, opts) — replaces the runtime.go
//     List{Metadata: gc.root_bead_id} topology walks.
//   - ListNodeEdges(ctx, id, direction) — replaces the generic store.DepList
//     topology reads.
//   - CloseSubtree(ctx, rootID, md) — a class-owned finalize (no caller today).
//   - FindOrCreateByKey(ctx, idemField, key, plan) — promotes the racy
//     striped-mutex idempotency at drain.go / molecule.findExistingAttach.
//   - ReadyCandidates(ctx, q) — claimable steps by gc.routed_to (not type=step).
type GraphStore interface {
	beads.GraphApplyStore
}

// MessageStore is the persistence seam for ClassMessaging — mail (type=message)
// and the extmsg families (gc:extmsg-* labels). Owner: internal/mail (+ beadmail)
// and internal/extmsg; the mail/extmsg services sit ON TOP of this seam rather
// than folding into it. The first bd-delegating implementation is any
// [beads.Store] (beadmail already runs on one).
//
// P0 surface is the exact set of store operations beadmail and the extmsg
// services perform on message-class beads:
//
//   - Create — send a mail message / persist an extmsg record
//     (beadmail.go; cmd/gc/cmd_handoff.go:308).
//   - Get — point-read for archive-idempotence and reply resolution
//     (beadmail.go:268,277,373; relies on beads.ErrNotFound propagating).
//   - List — inbox/thread/label queries (beadmail.go:566; extmsg label lookups;
//     cmd/gc sweep readers).
//   - Update — read/unread label toggles and field edits.
//   - Close — soft-close stale extmsg records (delivery dedupe, retired
//     bindings).
//   - Delete — eager archive of mail beads (archive == delete).
//   - SetMetadata / SetMetadataBatch — extmsg record state refresh.
//
// Growth path: a richer transcript interface if extmsg ownership widens beyond
// the persistence seam (design-doc open question — narrow seam chosen first).
type MessageStore interface {
	Create(b beads.Bead) (beads.Bead, error)
	Get(id string) (beads.Bead, error)
	List(query beads.ListQuery) ([]beads.Bead, error)
	Update(id string, opts beads.UpdateOpts) error
	Close(id string) error
	Delete(id string) error
	SetMetadata(id, key, value string) error
	SetMetadataBatch(id string, kvs map[string]string) error
}

// SessionsStore is the persistence seam for ClassSessions — session lifecycle
// beads (type=session, gc:session) and durable session waits (type=gate,
// gc:wait). Owner: internal/session. The first bd-delegating implementation is
// any [beads.Store].
//
// P0 surface is what the session lifecycle and wait paths perform today:
//
//   - Create — mint a session bead (pool create supplies an explicit ID, the
//     keyed-upsert seed) or a durable wait bead.
//   - Get — ResolveSessionID's by-exact-ID fast path.
//   - List / ListByLabel / ListByMetadata — session/wait discovery by
//     gc:session, gc:wait, session_name, alias (cmd/gc/cmd_stop.go:328,
//     session_lifecycle_parallel.go:611).
//   - Update — empty-Type repair, status reopen, coherent state transitions.
//   - SetMetadata / SetMetadataBatch — single- and multi-key session/wait state.
//   - Close — terminalize one session or wait bead.
//   - CloseAll — batch-cancel a session's open waits with cancellation metadata.
//
// Growth path: ReleaseIfCurrent (the CAS today expressed as
// beads.ConditionalAssignmentReleaser), keyed-upsert-by-session-name, and a
// per-class change feed (today the shared bd-hook event stream).
type SessionsStore interface {
	Create(b beads.Bead) (beads.Bead, error)
	Get(id string) (beads.Bead, error)
	List(query beads.ListQuery) ([]beads.Bead, error)
	ListByLabel(label string, limit int, opts ...beads.QueryOpt) ([]beads.Bead, error)
	ListByMetadata(filters map[string]string, limit int, opts ...beads.QueryOpt) ([]beads.Bead, error)
	Update(id string, opts beads.UpdateOpts) error
	SetMetadata(id, key, value string) error
	SetMetadataBatch(id string, kvs map[string]string) error
	Close(id string) error
	CloseAll(ids []string, metadata map[string]string) (int, error)
}

// OrdersStore is the persistence seam for ClassOrders — order-dispatch tracking
// beads (label order-tracking, plus per-run order-run:<scoped> labels) that gate
// repeat order firing. Owner: internal/orders + the order-dispatch path
// (cmd/gc/order_dispatch.go, cmd/gc/cmd_order.go). The first bd-delegating
// implementation is any [beads.Store].
//
// P0 surface is the store operations the order-dispatch gate, recency lookup,
// and retention sweep perform today:
//
//   - Create — the find-or-create-by-key CREATE leg (NoHistory tracking bead).
//   - Get — close-verification point-read (ErrNotFound == already gone).
//   - List / ListByLabel — open-tracking gate scan, recency lookup by
//     order-run:<scoped>, and the closed-tracking retention scan
//     (order_dispatch.go:1436,1852,2030).
//   - Update — stamp outcome/cursor labels and gc.routed_to after dispatch.
//   - Close — close a single manual-run tracking bead (cmd_order.go:770).
//   - CloseAll — batch-close on dispatch completion / stale sweep.
//   - DepList / DepRemove / Delete — the retention prune of a closed tracking
//     bead (detach edges, then delete).
//
// Growth path: FindOrCreateByKey (promotes today's racy List(open)+Create gate)
// and a class-owned SweepStale entry point.
type OrdersStore interface {
	Create(b beads.Bead) (beads.Bead, error)
	Get(id string) (beads.Bead, error)
	List(query beads.ListQuery) ([]beads.Bead, error)
	ListByLabel(label string, limit int, opts ...beads.QueryOpt) ([]beads.Bead, error)
	Update(id string, opts beads.UpdateOpts) error
	Close(id string) error
	CloseAll(ids []string, metadata map[string]string) (int, error)
	DepList(id, direction string) ([]beads.Dep, error)
	DepRemove(issueID, dependsOnID string) error
	Delete(id string) error
}

// NudgesStore is the persistence seam for ClassNudges — the durability mirror of
// the nudge queue (type=chore, gc:nudge, with a per-item nudge:<id> label). The
// live queue is a flock-guarded file; these beads are its persistent shadow.
// Owner: the nudge-queue subsystem (cmd/gc nudge beads + internal/nudgequeue
// terminalization). The first bd-delegating implementation is any [beads.Store].
//
// P0 surface is what the nudge ensure/terminalize/sweep paths perform today:
//
//   - Create — mint a nudge shadow bead (the ensure-by-nudge_id create leg).
//   - Get — the open/closed guard for terminalize-by-bead-id (waits.go:215).
//   - List — resolve by nudge:<id> label and the both-tier TTL sweep scan
//     (nudge_beads.go:57; waits.go:153; nudge_mail_sweep.go:59).
//   - SetMetadata — the enqueue-rollback close-reason stamp (cmd_nudge.go:1690).
//   - SetMetadataBatch — terminal metadata transition (state, terminal_reason,
//     commit_boundary, timestamps).
//   - Close — terminalize / TTL-sweep close.
//
// Growth path: EnsureByNudgeID and SweepStale as class-owned upsert/sweep
// entry points, and DeleteTerminalized hard-delete of long-dead shadows.
type NudgesStore interface {
	Create(b beads.Bead) (beads.Bead, error)
	Get(id string) (beads.Bead, error)
	List(query beads.ListQuery) ([]beads.Bead, error)
	SetMetadata(id, key, value string) error
	SetMetadataBatch(id string, kvs map[string]string) error
	Close(id string) error
}

// Compile-time proof that each non-graph class interface is a faithful SUBSET of
// beads.Store: the bd-delegating first implementation of every one is therefore
// any beads.Store, with no adapter code. If a future edit adds a method to one of
// these interfaces that beads.Store does not provide, the corresponding line
// fails to compile — forcing an explicit decision (extend beads.Store, or move
// the method to a class-specific adapter) rather than silently widening the seam.
var (
	_ WorkStore     = beads.Store(nil)
	_ MessageStore  = beads.Store(nil)
	_ SessionsStore = beads.Store(nil)
	_ OrdersStore   = beads.Store(nil)
	_ NudgesStore   = beads.Store(nil)
)
