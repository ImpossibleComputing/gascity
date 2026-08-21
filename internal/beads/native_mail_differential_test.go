package beads_test

import (
	"slices"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/mail"
	"github.com/gastownhall/gascity/internal/mail/beadmail"
	"github.com/gastownhall/gascity/internal/session"
)

// This file is the load-bearing correctness gate for restoring the native Dolt
// store on the mail read path. Enabling the native store must not change which
// mail a mailbox surfaces, because alias expansion happens in the beadmail
// PROVIDER (recipientRoutes), ABOVE the store: both the reference in-memory
// store and the NativeDoltStore receive the SAME already-expanded per-route
// ListQuery and filter by the indexed assignee OR the mail.to_session_id
// metadata leg. These tests prove that by seeding structurally identical
// mailboxes into both backends and asserting the provider's EXPANDING methods
// (Inbox/Check/All/Count) return byte-identical message sets and counts across
// the two — for every address-form of a mailbox renamed across aliases,
// including a stranded closed-predecessor mailbox, and a reply carrying the
// mail.to_session_id metadata IC's recipient predicate reads.
//
// The two backends assign their own bead ids, so a mailbox's id-address form is
// each backend's OWN id and cross-backend set comparison is by the stable
// message subject rather than the store-minted id. A divergence here is a REAL
// mail-completeness bug in the native path, not a test artifact.

// backendMailbox is one store backend seeded with an identical mailbox lineage,
// plus the beadmail provider over it and the store-assigned session ids.
type backendMailbox struct {
	name     string
	provider *beadmail.Provider
	liveID   string
	closedID string
}

// seedDifferentialMailbox seeds ONE mailbox renamed across aliases (current
// "mayor", historical "deacon", plus its stable session_name and bead id), a
// closed-predecessor mailbox ("steward", rotated out), and message beads
// addressed on the different address-forms of the live mailbox — historical
// alias, current alias, bead id, and a reply retargeted to the session id and
// carrying mail.to_session_id — mixing read and unread. Read state is the
// "read" label, the field beadmail filters on.
func seedDifferentialMailbox(t *testing.T, name string, store beads.Store) backendMailbox {
	t.Helper()
	mustCreate := func(b beads.Bead) beads.Bead {
		created, err := store.Create(b)
		if err != nil {
			t.Fatalf("%s: create %q: %v", name, b.Title, err)
		}
		return created
	}

	live := mustCreate(beads.Bead{
		Type:   session.BeadType,
		Labels: []string{session.LabelSession},
		Metadata: map[string]string{
			"alias":         "mayor",
			"alias_history": "deacon",
			"session_name":  "workflows__mayor-live",
		},
	})

	closed := mustCreate(beads.Bead{
		Type:   session.BeadType,
		Labels: []string{session.LabelSession},
		Metadata: map[string]string{
			"alias":        "steward",
			"session_name": "workflows__steward-old",
		},
	})
	if err := store.Close(closed.ID); err != nil {
		t.Fatalf("%s: close predecessor session: %v", name, err)
	}

	// Live mailbox mail, one per address-form, mixing read and unread.
	mustCreate(beads.Bead{Title: "hist-deacon", Type: "message", From: "human", Assignee: "deacon"})
	mustCreate(beads.Bead{Title: "curr-mayor", Type: "message", From: "human", Assignee: "mayor", Labels: []string{"read"}})
	mustCreate(beads.Bead{Title: "by-id", Type: "message", From: "human", Assignee: live.ID})
	mustCreate(beads.Bead{
		Title:    "reply-retarget",
		Type:     "message",
		From:     "human",
		Assignee: live.ID,
		Labels:   []string{"read"},
		Metadata: map[string]string{mail.ToSessionIDMetadataKey: live.ID},
	})

	// Mail stranded on the closed predecessor mailbox.
	mustCreate(beads.Bead{Title: "stranded-closed", Type: "message", From: "human", Assignee: closed.ID})

	return backendMailbox{name: name, provider: beadmail.New(store), liveID: live.ID, closedID: closed.ID}
}

func sortedSubjects(t *testing.T, msgs []mail.Message, err error) []string {
	t.Helper()
	if err != nil {
		t.Fatalf("listing mail: %v", err)
	}
	subjects := make([]string, 0, len(msgs))
	for _, m := range msgs {
		subjects = append(subjects, m.Subject)
	}
	slices.Sort(subjects)
	return subjects
}

// TestNativeAndReferenceMailboxesAgreeAcrossAddressForms is the cross-backend
// differential completeness gate. For every address-form of the live mailbox —
// current alias, historical alias, and bead id — the reference MemStore-backed
// provider and the NativeDoltStore-backed provider must surface byte-identical
// message-subject sets and identical (total, unread) counts through the
// EXPANDING methods, and each form must surface the FULL mailbox.
func TestNativeAndReferenceMailboxesAgreeAcrossAddressForms(t *testing.T) {
	ref := seedDifferentialMailbox(t, "MemStore", beads.NewMemStore())
	native := seedDifferentialMailbox(t, "NativeDoltStore", beads.NewNativeDoltStoreForConformance())

	// The bead-id form differs per backend, so each is queried with its own id.
	forms := []struct {
		name       string
		refAddr    string
		nativeAddr string
	}{
		{name: "current alias", refAddr: "mayor", nativeAddr: "mayor"},
		{name: "historical alias", refAddr: "deacon", nativeAddr: "deacon"},
		{name: "bead id", refAddr: ref.liveID, nativeAddr: native.liveID},
	}

	// All four messages belong to the live mailbox; two carry the "read" label.
	wantFull := []string{"by-id", "curr-mayor", "hist-deacon", "reply-retarget"}
	wantUnread := []string{"by-id", "hist-deacon"}

	expanding := []struct {
		name        string
		run         func(p *beadmail.Provider, addr string) ([]mail.Message, error)
		wantSubject []string
	}{
		{name: "Inbox", run: (*beadmail.Provider).Inbox, wantSubject: wantUnread},
		{name: "Check", run: (*beadmail.Provider).Check, wantSubject: wantUnread},
		{name: "All", run: (*beadmail.Provider).All, wantSubject: wantFull},
	}

	for _, form := range forms {
		t.Run(form.name, func(t *testing.T) {
			for _, method := range expanding {
				refMsgs, refErr := method.run(ref.provider, form.refAddr)
				nativeMsgs, nativeErr := method.run(native.provider, form.nativeAddr)
				refSubjects := sortedSubjects(t, refMsgs, refErr)
				nativeSubjects := sortedSubjects(t, nativeMsgs, nativeErr)

				if !slices.Equal(refSubjects, nativeSubjects) {
					t.Errorf("%s(%s): backends disagree: MemStore=%v NativeDoltStore=%v",
						method.name, form.name, refSubjects, nativeSubjects)
				}
				// Each backend must independently surface the full mailbox for the
				// form, so a shared-but-wrong answer cannot pass the parity check.
				if !slices.Equal(refSubjects, method.wantSubject) {
					t.Errorf("MemStore %s(%s) subjects = %v, want %v", method.name, form.name, refSubjects, method.wantSubject)
				}
				if !slices.Equal(nativeSubjects, method.wantSubject) {
					t.Errorf("NativeDoltStore %s(%s) subjects = %v, want %v", method.name, form.name, nativeSubjects, method.wantSubject)
				}
			}

			refTotal, refUnread := mustCount(t, ref.provider, form.refAddr)
			nativeTotal, nativeUnread := mustCount(t, native.provider, form.nativeAddr)
			if refTotal != nativeTotal || refUnread != nativeUnread {
				t.Errorf("Count(%s): backends disagree: MemStore=(%d,%d) NativeDoltStore=(%d,%d)",
					form.name, refTotal, refUnread, nativeTotal, nativeUnread)
			}
			if refTotal != 4 || refUnread != 2 {
				t.Errorf("MemStore Count(%s) = (%d,%d), want (4,2)", form.name, refTotal, refUnread)
			}
			if nativeTotal != 4 || nativeUnread != 2 {
				t.Errorf("NativeDoltStore Count(%s) = (%d,%d), want (4,2)", form.name, nativeTotal, nativeUnread)
			}
		})
	}

	// The stranded closed-predecessor mailbox, addressed by its own alias, must
	// also agree across backends and must not leak into the live mailbox above.
	t.Run("closed predecessor alias", func(t *testing.T) {
		refMsgs, refErr := ref.provider.Inbox("steward")
		nativeMsgs, nativeErr := native.provider.Inbox("steward")
		refSubjects := sortedSubjects(t, refMsgs, refErr)
		nativeSubjects := sortedSubjects(t, nativeMsgs, nativeErr)
		want := []string{"stranded-closed"}
		if !slices.Equal(refSubjects, nativeSubjects) {
			t.Errorf("Inbox(steward): backends disagree: MemStore=%v NativeDoltStore=%v", refSubjects, nativeSubjects)
		}
		if !slices.Equal(refSubjects, want) || !slices.Equal(nativeSubjects, want) {
			t.Errorf("Inbox(steward) = MemStore %v / Native %v, want %v", refSubjects, nativeSubjects, want)
		}
		refTotal, refUnread := mustCount(t, ref.provider, "steward")
		nativeTotal, nativeUnread := mustCount(t, native.provider, "steward")
		if refTotal != nativeTotal || refUnread != nativeUnread || refTotal != 1 || refUnread != 1 {
			t.Errorf("Count(steward) = MemStore (%d,%d) / Native (%d,%d), want (1,1) on both", refTotal, refUnread, nativeTotal, nativeUnread)
		}
	})
}

func mustCount(t *testing.T, p *beadmail.Provider, addr string) (int, int) {
	t.Helper()
	total, unread, err := p.Count(addr)
	if err != nil {
		t.Fatalf("Count(%s): %v", addr, err)
	}
	return total, unread
}

// TestNativeAndReferenceAgreeOnFailClosedRouting proves the ambiguity/fallback
// seam is backend-agnostic and fail-closed. An unambiguous historical alias is
// surfaced through an EXPANDING method (never the route-exact InboxRoutes),
// while an alias two live sessions share in their history degrades to the raw
// recipient — so an expanding read never fans out into either candidate
// mailbox. Both behaviors must be byte-identical across the reference and
// native backends.
func TestNativeAndReferenceAgreeOnFailClosedRouting(t *testing.T) {
	t.Run("unambiguous historical alias resolves via expanding scan", func(t *testing.T) {
		seed := func(store beads.Store) (*beadmail.Provider, string) {
			live, err := store.Create(beads.Bead{
				Type:   session.BeadType,
				Labels: []string{session.LabelSession},
				Metadata: map[string]string{
					"alias":         "new-worker",
					"alias_history": "worker",
					"session_name":  "workflows__worker-live",
				},
			})
			if err != nil {
				t.Fatalf("create session: %v", err)
			}
			if _, err := store.Create(beads.Bead{Title: "to-worker", Type: "message", From: "human", Assignee: live.ID}); err != nil {
				t.Fatalf("create mail: %v", err)
			}
			return beadmail.New(store), live.ID
		}

		refProvider, _ := seed(beads.NewMemStore())
		nativeProvider, _ := seed(beads.NewNativeDoltStoreForConformance())

		refMsgs, refErr := refProvider.Inbox("worker")
		nativeMsgs, nativeErr := nativeProvider.Inbox("worker")
		refSubjects := sortedSubjects(t, refMsgs, refErr)
		nativeSubjects := sortedSubjects(t, nativeMsgs, nativeErr)
		want := []string{"to-worker"}
		if !slices.Equal(refSubjects, nativeSubjects) {
			t.Errorf("Inbox(worker): backends disagree: MemStore=%v NativeDoltStore=%v", refSubjects, nativeSubjects)
		}
		if !slices.Equal(refSubjects, want) || !slices.Equal(nativeSubjects, want) {
			t.Errorf("Inbox(worker) = MemStore %v / Native %v, want the historical-alias mail %v", refSubjects, nativeSubjects, want)
		}
	})

	t.Run("ambiguous historical alias degrades to raw recipient", func(t *testing.T) {
		// Two live sessions share "ghost" in their alias history. recipientRoutes
		// degrades that to the literal recipient (len(matches)>1 guard), so an
		// expanding read surfaces only mail addressed to the bare "ghost" — never
		// either session's id-addressed mail, which would be a misdelivery.
		seed := func(store beads.Store) *beadmail.Provider {
			mk := func(alias string) string {
				b, err := store.Create(beads.Bead{
					Type:   session.BeadType,
					Labels: []string{session.LabelSession},
					Metadata: map[string]string{
						"alias":         alias,
						"alias_history": "ghost",
						"session_name":  "workflows__" + alias,
					},
				})
				if err != nil {
					t.Fatalf("create session %s: %v", alias, err)
				}
				return b.ID
			}
			s1 := mk("apricot")
			s2 := mk("cobalt")
			mustCreate := func(b beads.Bead) {
				if _, err := store.Create(b); err != nil {
					t.Fatalf("create %q: %v", b.Title, err)
				}
			}
			mustCreate(beads.Bead{Title: "ghost-literal", Type: "message", From: "human", Assignee: "ghost"})
			mustCreate(beads.Bead{Title: "s1-mail", Type: "message", From: "human", Assignee: s1})
			mustCreate(beads.Bead{Title: "s2-mail", Type: "message", From: "human", Assignee: s2})
			return beadmail.New(store)
		}

		refProvider := seed(beads.NewMemStore())
		nativeProvider := seed(beads.NewNativeDoltStoreForConformance())

		refMsgs, refErr := refProvider.Inbox("ghost")
		nativeMsgs, nativeErr := nativeProvider.Inbox("ghost")
		refSubjects := sortedSubjects(t, refMsgs, refErr)
		nativeSubjects := sortedSubjects(t, nativeMsgs, nativeErr)
		want := []string{"ghost-literal"}
		if !slices.Equal(refSubjects, nativeSubjects) {
			t.Errorf("Inbox(ghost): backends disagree: MemStore=%v NativeDoltStore=%v", refSubjects, nativeSubjects)
		}
		if !slices.Equal(refSubjects, want) || !slices.Equal(nativeSubjects, want) {
			t.Errorf("Inbox(ghost) = MemStore %v / Native %v, want raw-recipient-only %v (no fan-out to either shared-history mailbox)",
				refSubjects, nativeSubjects, want)
		}
	})
}
