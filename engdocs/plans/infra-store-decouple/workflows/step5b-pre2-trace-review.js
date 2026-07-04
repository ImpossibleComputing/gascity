export const meta = {
  name: 'step5b-pre2-trace-review',
  description: 'Fable review of the store-authoritative gc-trace terminal read (5b precursor, take 2)',
  phases: [
    { title: 'Refute', detail: 'adversarial lenses try to break byte-identity / dispatch preservation' },
    { title: 'Synthesize', detail: 'verify surviving findings against code' },
  ],
}

const WORKTREE = '/data/projects/gascity/.claude/worktrees/object-front-doors'

const CONTEXT = [
  'You are adversarially reviewing an in-progress precursor commit on a LIVE session reconciler controller.',
  'Repo worktree: ' + WORKTREE,
  'Diff (read-only): cd ' + WORKTREE + ' && git --no-pager diff HEAD -- cmd/gc/city_runtime.go',
  'You may read any file. Do NOT modify anything.',
  '',
  'THE CHANGE (precursor to lockstep-drop Step 5b): recordReconcileTraceResults previously read each open',
  'session terminal trace field state/sleep_reason from the raw open bead metadata (bead.Metadata["state"],',
  'bead.Metadata["sleep_reason"]). It now reads them from the AUTHORITATIVE post-reconcile store snapshot',
  '(postReconcile.FindByID(bead.ID)) with a per-bead fallback to bead.Metadata when the ID is absent from the',
  'snapshot (loadSessionBeadSnapshot excludes closed beads, so a session closed this tick falls back). The caller',
  'beadReconcileTick was reordered: the post-reconcile loadSessionBeadSnapshot(store) (previously loaded only for',
  'wait-nudge dispatch, AFTER recordReconcileTraceResults) is now loaded ONCE before recordReconcileTraceResults',
  'and SHARED with the dispatch. On load error the dispatch is skipped (as before) and the trace falls back to',
  'open metadata; the load-error log message changed from "dispatching wait nudges" to "loading post-reconcile',
  'session snapshot".',
  '',
  'WHY it is byte-identical AT THIS COMMIT (all raw lockstep mirror writes are still present here — they are',
  'deleted in the NEXT commit, 5b): every in-memory metadata mirror in the reconciler is paired with a front-door',
  'ApplyPatch that persists the SAME batch to the store (e.g. preWakeCommit at session_wake.go:68-77 persists',
  'PreWakePatch then mirrors it; the drain-ack/sleep/heal/rebaseline helpers persist-then-mirror). So for every',
  'OPEN session, the post-reconcile store snapshot holds exactly what the open bead map holds, and for every',
  'session CLOSED this tick the snapshot excludes it so the fallback reads the open bead unchanged. Net: identical',
  'recorded values at this commit. The point is to make the trace source authoritative so deleting the mirror',
  'writes in 5b does not stale the trace.',
  '',
  'Claims to REFUTE:',
  'A. At THIS commit, for EVERY open session, postReconcile.FindByID(bead.ID) returns a bead whose',
  '   Metadata["state"]/["sleep_reason"] equals the open bead map values recordReconcileTraceResults read before.',
  '   In particular WOKEN sessions (preWakeCommit persists PreWakePatch state="creating" AND mirrors it) — the',
  '   store snapshot must show "creating" just like the open bead (this is the case that broke a prior attempt',
  '   that used the reconciler in-memory infoByID instead of the store).',
  'B. Sessions closed this tick (store-only closes failed-create/orphan; drain-ack close) are excluded from',
  '   loadSessionBeadSnapshot (it filters Status=="closed"), so FindByID misses and the fallback reproduces the',
  '   exact prior open-bead read. Confirm loadSessionBeadSnapshot/newSessionBeadSnapshot filters closed and',
  '   FindByID only returns open beads.',
  'C. The reorder preserves dispatch behavior EXACTLY: dispatch runs iff the load succeeded (err==nil), same',
  '   snapshot, same nudge call, same recordPhase("dispatch_wait_nudges") with traceSessionSnapshotFields(same',
  '   snapshot); on load error dispatch is skipped and traceSessionSnapshotFields(nil) is still called (as before).',
  '   The only behavioral delta is the load-error log string (no test asserts it — verify) and load timing moving',
  '   out of the dispatch phase. Trace record ORDER is unchanged (reconcile_sessions, record_trace_session_results,',
  '   dispatch_wait_nudges).',
  'D. No ADDED store query per tick: the snapshot load is the SAME one that dispatch already did, just moved',
  '   earlier and shared (the baseline trace is always-on, so this path runs every tick — a second List would be',
  '   a real regression). FindByID is an in-memory map lookup, not a store call.',
  'E. Nil-safety: on load error dispatchSessionBeads is nil; recordReconcileTraceResults guards postReconcile!=nil',
  '   and falls back; traceSessionSnapshotFields(nil) is safe (as before).',
].join('\n')

const SCHEMA = {
  type: 'object', additionalProperties: false,
  properties: {
    findings: { type: 'array', items: { type: 'object', additionalProperties: false,
      properties: {
        severity: { type: 'string', enum: ['HIGH', 'MEDIUM', 'LOW', 'NIT'] },
        file: { type: 'string' }, line: { type: 'integer' },
        claim_broken: { type: 'string' }, detail: { type: 'string' },
        confidence: { type: 'string', enum: ['CONFIRMED', 'PLAUSIBLE', 'SPECULATIVE'] },
      }, required: ['severity', 'file', 'detail', 'confidence'] } },
    verdict: { type: 'string', enum: ['CLEAN', 'FINDINGS'] },
    notes: { type: 'string' },
  }, required: ['findings', 'verdict'],
}

const LENSES = [
  {
    key: 'snapshot-equivalence',
    focus: [
      'REFUTE claims A and B. Enumerate every kind of session transition the reconciler performs this tick (wake/',
      'preWakeCommit, sleep, drain-ack non-close, drain-ack close, heal, rebaseline, config-drift, store-only',
      'close, churn) and for each prove whether the post-reconcile store snapshot value for state/sleep_reason',
      'equals the open-bead map value at THIS commit (mirrors present). Focus hardest on WOKEN sessions: confirm',
      'preWakeCommit persists PreWakePatch to the STORE (so the fresh snapshot shows it) as well as mirroring it —',
      'so unlike an in-memory infoByID snapshot, the STORE snapshot is NOT stale for woken sessions. Then confirm',
      'closed-this-tick sessions are excluded from loadSessionBeadSnapshot (Status=="closed" filter) so the',
      'fallback fires and reproduces the old read. Find ANY transition where store snapshot != open bead at this',
      'commit (that would be a real byte-identity break).',
    ],
  },
  {
    key: 'reorder-and-dispatch',
    focus: [
      'REFUTE claims C, D, E. Diff the control flow of beadReconcileTick before/after: prove dispatch runs under',
      'exactly the same condition (load success), with the same arguments and the same recordPhase call and fields.',
      'Confirm no ADDED loadSessionBeadSnapshot call (count them before/after — must be the same). Confirm',
      'traceSessionSnapshotFields handles the nil snapshot on load error (read its body). Confirm the trace record',
      'sequence order is unchanged. Grep the tests for any assertion on the old "dispatching wait nudges" load-error',
      'string or on the phase timing/order that the reorder could break. Confirm FindByID is a pure in-memory',
      'lookup (no store I/O). Flag any behavior change beyond the intended trace-source + log-string.',
    ],
  },
]

phase('Refute')
const lensResults = await parallel(LENSES.map(l => () =>
  agent(
    [CONTEXT, '', '=== YOUR LENS: ' + l.key + ' ===', l.focus.join('\n'), '',
     'Be a skeptic; report a finding only with a concrete divergence or regression. Else verdict CLEAN, empty',
     'findings, one-line note. Return ONLY the structured object.'].join('\n'),
    { label: 'refute:' + l.key, phase: 'Refute', schema: SCHEMA, model: 'fable', effort: 'high' }
  ).then(r => ({ lens: l.key, ...r }))
))

phase('Synthesize')
const packed = lensResults.filter(Boolean).map(r =>
  '## Lens ' + r.lens + ' (verdict=' + r.verdict + ')\n' + (r.notes ? 'notes: ' + r.notes + '\n' : '') +
  (r.findings || []).map(f => '- [' + f.severity + '/' + f.confidence + '] ' + f.file + (f.line ? ':' + f.line : '') + ' :: ' + f.detail).join('\n')
).join('\n\n')

const synthesis = await agent(
  [CONTEXT, '', 'The lenses reported below. Independently verify each finding against the code; discard refuted/',
   'speculative ones. Give the final verdict for whether this precursor is byte-identical at this commit and safe',
   'to commit.', '', packed, '', 'Return ONLY the structured object (findings = CONFIRMED survivors only).'].join('\n'),
  { label: 'synthesize', phase: 'Synthesize', schema: SCHEMA, model: 'fable', effort: 'high' }
)

return { lensResults, synthesis }
