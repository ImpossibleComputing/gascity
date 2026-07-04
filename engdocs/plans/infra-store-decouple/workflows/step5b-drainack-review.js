export const meta = {
  name: 'step5b-drainack-review',
  description: 'Fable adversarial byte-identity review of lockstep-drop Step 5b (drain-ack finalize family off raw metadata)',
  phases: [
    { title: 'Refute', detail: 'independent adversarial lenses try to break byte-identity' },
    { title: 'Synthesize', detail: 'aggregate surviving findings, rank by severity' },
  ],
}

const WORKTREE = '/data/projects/gascity/.claude/worktrees/object-front-doors'

const CONTEXT = [
  'You are adversarially reviewing an in-progress refactor commit on a LIVE session reconciler.',
  'Repo worktree: ' + WORKTREE,
  'The change is lockstep-drop Step 5b: taking the drain-ack finalize family OFF raw session-bead',
  'metadata reads and deleting its now-read-dead raw metadata mirror WRITE loops.',
  '',
  'To see the exact diff under review, run (read-only):',
  '  cd ' + WORKTREE + ' && git --no-pager diff HEAD -- cmd/gc/session_reconciler.go cmd/gc/session_reconciler_test.go cmd/gc/telemetry_lifecycle_metrics_test.go',
  'You may read any file in the repo to check surrounding context. Do NOT modify anything.',
  '',
  'What Step 5b did (the claims you must try to REFUTE):',
  '1. markDrainAckStopPending: signature changed from (session *beads.Bead, ...) to (info sessionpkg.Info, ...).',
  '   Reads info.ID / info.SessionNameMetadata instead of session.ID / session.Metadata["session_name"].',
  '   DELETED its raw metadata mirror loop (for key,value := range batch { session.Metadata[key]=value }).',
  '   The two callers (in the forward-pass loop) already fold DrainAckStopPendingPatch onto infoByID and continue.',
  '2. finalizeDrainAckStoppedSession: ADDED an info sessionpkg.Info param (KEPT session *beads.Bead — it is still',
  '   needed for the whole-bead raw-by-design helpers sessionHasOpenAssignedWorkForReachableStore,',
  '   closeSessionBeadIfReachableStoreUnassigned, recordDrainAckAssignedWorkEvent, sessionAgentMetricIdentity,',
  '   and the store.Get witness). Converted its metadata bracket READS (session_name, template, wake_mode,',
  '   restart_requested) to info.SessionNameMetadata / normalizedSessionTemplateInfo(info)+info.Template /',
  '   info.WakeMode / info.RestartRequested. DELETED the closePatch mirror loop and the drain-ack ApplyPatch',
  '   mirror loop. KEPT the raw struct writes session.Status="closed" and the witness session.Status=latest.Status',
  '   / session.Metadata=latest.Metadata (these are NOT Metadata-bracket writes and are asserted by telemetry tests).',
  '3. reconcileDrainAckStopPending: ADDED info param, converted its reads, threads info to finalize.',
  '4. finalizeDrainAckStopPendingSessions (non-reconciler, city_runtime caller): added a boundary per-bead',
  '   projection info := InfoFromPersistedBead(*session) and feeds the helpers off it.',
  '5. Call sites pass the coherent info: :1486 passes the top-of-loop local info (== infoByID[session.ID]);',
  '   the orphan-arm finalize passes infoByID[session.ID] (post-heal coherent); the zombie-arm finalize and',
  '   markDrainAckStopPending pass infoByID[session.ID] (post-zombie coherent).',
  '',
  'The load-bearing byte-identity claims:',
  'A. Info fields are VERBATIM raw mirrors: Info.SessionNameMetadata=Metadata["session_name"],',
  '   Info.WakeMode=Metadata["wake_mode"], Info.RestartRequested=Metadata["restart_requested"],',
  '   Info.Template=Metadata["template"] (verify in internal/session/info_store.go).',
  'B. At every call site, the passed info equals InfoFromPersistedBead(*session) at that point',
  '   (no un-folded same-tick mutation between snapshot coherence and the drain-ack call).',
  'C. Deleting the raw metadata mirror writes is safe because NO later this-tick reader consumes the raw',
  '   session bead (== &ordered[i]) for the mirrored keys: a drain-acked session hits continue BEFORE the',
  '   wakeTargets append (session_reconciler.go ~:2997) so it never becomes a startCandidate, and the only',
  '   post-forward-pass raw read of ordered[i] is ordered[i].ID (the sessionInfos rebuild ~:3026).',
  'D. normalizedSessionTemplateInfo(info,cfg) == normalizedSessionTemplate(*session,cfg) for coherent info',
  '   (equivalence-proven, used elsewhere at ~:1714/:1782/:1794).',
].join('\n')

const FINDINGS_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  properties: {
    findings: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: false,
        properties: {
          severity: { type: 'string', enum: ['HIGH', 'MEDIUM', 'LOW', 'NIT'] },
          file: { type: 'string' },
          line: { type: 'integer' },
          claim_broken: { type: 'string', description: 'which byte-identity claim (A-D) or safety property is violated' },
          detail: { type: 'string', description: 'concrete input/state -> divergent behavior vs the pre-5b code' },
          confidence: { type: 'string', enum: ['CONFIRMED', 'PLAUSIBLE', 'SPECULATIVE'] },
        },
        required: ['severity', 'file', 'detail', 'confidence'],
      },
    },
    verdict: { type: 'string', enum: ['CLEAN', 'FINDINGS'] },
    notes: { type: 'string' },
  },
  required: ['findings', 'verdict'],
}

const LENSES = [
  {
    key: 'census-later-reader',
    focus: [
      'REFUTE claim C (mirror-write deletion safety). Hunt for ANY later-this-tick raw reader of the drain-acked',
      'session bead (session == &ordered[i]) that consumes a mirrored key after the drain-ack branch runs.',
      'Trace the forward-pass loop (for i := range ordered { session := &ordered[i]; ...) in session_reconciler.go.',
      'Verify EVERY drain-ack exit (markDrainAckStopPending true-arm, finalize orphan-arm, finalize zombie-arm,',
      'reconcileDrainAckStopPending handled-arm at :1486) reaches a continue BEFORE the wakeTargets append (~:2997).',
      'Then verify no post-loop phase reads ordered[i].Metadata (only ordered[i].ID). Check startCandidates',
      'membership: could a drain-acked bead be appended to startCandidates or wakeTargets this tick? Check the',
      'async-stop path (queueDrainAckAsyncStop) — does it read the raw bead metadata or re-load from the store?',
      'A CONFIRMED finding requires naming the exact later read site and the key it reads.',
    ],
  },
  {
    key: 'info-coherence',
    focus: [
      'REFUTE claim B (info coherence at call sites). For EACH of these finalize/mark/reconcile call sites, prove',
      'or disprove that the passed info equals InfoFromPersistedBead(*session) at that instant:',
      '  - reconcileDrainAckStopPending call at ~:1486 (passes the :1473 local info)',
      '  - orphan-arm finalize (passes infoByID[session.ID], post-heal)',
      '  - zombie-arm finalize + markDrainAckStopPending (passes infoByID[session.ID], post-zombie)',
      '  - reconcileDrainAckStopPending -> finalize internal call (passes its info param through)',
      '  - finalizeDrainAckStopPendingSessions boundary projection (InfoFromPersistedBead(*session))',
      'Look for any mutation of *session or a missing fold of infoByID[session.ID] on the path from the last',
      'snapshot refresh to the drain-ack call. A stale info here silently flips wake_mode/restart_requested/name.',
      'Especially: does anything mutate wake_mode or restart_requested on ordered[i] earlier this iteration?',
    ],
  },
  {
    key: 'read-timing-and-equivalence',
    focus: [
      'REFUTE claims A and D (verbatim field mirrors + template equivalence).',
      'Open internal/session/info_store.go and confirm SessionNameMetadata/WakeMode/RestartRequested/Template are',
      'verbatim (untrimmed) mirrors of the raw metadata keys. Confirm normalizedSessionTemplateInfo is a faithful',
      'sibling of normalizedSessionTemplate (session_name_lookup.go). Inside finalizeDrainAckStoppedSession, confirm',
      'nothing mutates session.Metadata between function entry and the info.WakeMode read (line ~461) or the',
      'info.RestartRequested read (~472) that would have made the OLD raw read differ from the pre-call snapshot',
      '(the close path returns early; verify the non-close fall-through does not rewrite wake_mode/restart_requested).',
      'Also confirm the restart_requested consumption still works: the batch sets restart_requested="" and the',
      'caller-side overlay-clear is unchanged (#2574 phantom-restart guard).',
    ],
  },
  {
    key: 'kept-writes-and-tests',
    focus: [
      'REFUTE the test-safety and kept-write properties. Confirm the raw struct write session.Status="closed"',
      '(close path) and the witness session.Status=latest.Status are RETAINED (not deleted) — the telemetry tests',
      'telemetry_lifecycle_metrics_test.go assert session.Status=="closed" after finalize on both the close path',
      'and the witness path. Confirm the drainAckFinalizeResult return values (batch/closed/witnessInfo) are',
      'unchanged so caller folds are byte-identical. Then scan ALL tests that call finalizeDrainAckStoppedSession /',
      'reconcileDrainAckStopPending / markDrainAckStopPending / finalizeDrainAckStopPendingSessions for any',
      'assertion on the raw in-memory session.Metadata (NOT the store via Get) that the deleted mirror loops would',
      'have satisfied. Flag any such assertion as a break. Confirm the 4 updated test call sites pass a correct',
      'InfoFromPersistedBead(session) matching the mutated fixture state.',
    ],
  },
  {
    key: 'signature-and-callgraph',
    focus: [
      'REFUTE completeness. Independently re-grep the whole repo for callers of the four changed functions',
      '(finalizeDrainAckStoppedSession, reconcileDrainAckStopPending, markDrainAckStopPending,',
      'finalizeDrainAckStopPendingSessions) and confirm EVERY caller (prod + test) was updated to the new',
      'signature/argument order, and that no caller now passes a wrong-typed or mis-ordered argument that happens',
      'to compile. Confirm finalizeDrainAckStopPendingSessions signature is UNCHANGED (only its body added a',
      'boundary projection) so city_runtime.go:1153 and its test still compile. Confirm the info param position',
      '(right after session *beads.Bead) is consistent across all four functions and all call sites.',
    ],
  },
]

phase('Refute')
const lensResults = await parallel(LENSES.map(l => () =>
  agent(
    [
      CONTEXT,
      '',
      '=== YOUR LENS: ' + l.key + ' ===',
      l.focus.join('\n'),
      '',
      'Be a skeptic. Default to reporting a finding only if you can name a concrete divergence from the pre-5b',
      'behavior (inputs/state -> wrong output) or a concrete compile/coverage gap. If you cannot break it, return',
      'verdict CLEAN with an empty findings array and a one-line note on what you checked and why it holds.',
      'Return ONLY the structured object.',
    ].join('\n'),
    { label: 'refute:' + l.key, phase: 'Refute', schema: FINDINGS_SCHEMA, model: 'fable', effort: 'high' }
  ).then(r => ({ lens: l.key, ...r }))
))

phase('Synthesize')
const packed = lensResults.filter(Boolean).map(r =>
  '## Lens ' + r.lens + ' (verdict=' + r.verdict + ')\n' +
  (r.notes ? 'notes: ' + r.notes + '\n' : '') +
  (r.findings || []).map(f =>
    '- [' + f.severity + '/' + f.confidence + '] ' + f.file + (f.line ? ':' + f.line : '') +
    ' broke=' + (f.claim_broken || '?') + ' :: ' + f.detail
  ).join('\n')
).join('\n\n')

const synthesis = await agent(
  [
    CONTEXT,
    '',
    'The five adversarial lenses reported the results below. Your job: for EACH reported finding, independently',
    'verify it against the actual code (read the files, do not trust the lens). Discard refuted/speculative ones.',
    'Produce the final verdict for whether Step 5b is byte-identical and safe to commit. Rank surviving findings',
    'most-severe first. If a HIGH/MEDIUM survives, state the exact fix. If all clear, verdict CLEAN.',
    '',
    packed,
    '',
    'Return ONLY the structured object (findings = only CONFIRMED survivors after your own verification).',
  ].join('\n'),
  { label: 'synthesize', phase: 'Synthesize', schema: FINDINGS_SCHEMA, model: 'fable', effort: 'high' }
)

return { lensResults, synthesis }
