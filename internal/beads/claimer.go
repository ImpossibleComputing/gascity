package beads

import "errors"

// ErrClaimUnsupported reports that a routed claim found no backend able to
// atomically claim the bead — the owning backend implements neither [Claimer]
// nor [EnvActorClaimer]. Callers that must claim should treat this as a hard
// error rather than a lost race.
var ErrClaimUnsupported = errors.New("bead claim unsupported")

// Claimer atomically claims an open bead for an EXPLICIT assignee, returning the
// claimed bead with ok=true on success, ok=false when another actor won the race,
// and ErrNotFound when the bead is absent. Implemented by stores that accept the
// assignee per call — e.g. SQLiteStore, whose single write connection makes the
// claim a single-winner CAS.
type Claimer interface {
	Claim(id, assignee string) (Bead, bool, error)
}

// EnvActorClaimer atomically claims an open bead for the actor configured on the
// store ITSELF (not a per-call argument) — e.g. a BdStore whose CommandRunner
// bakes BEADS_ACTOR into the environment, so `bd update --claim` records that
// actor. Same ok/err contract as [Claimer]. Callers route the assignee by
// constructing the backend with the matching actor.
type EnvActorClaimer interface {
	Claim(id string) (Bead, bool, error)
}
