export const meta = {
  name: 'step5b-pre-trace-review',
  description: 'Fable adversarial review of the 5b precursor: reconciler returns final Info snapshot; gc-trace terminal read off it',
  phases: [
    { title: 'Refute', detail: 'adversarial lenses try to break byte-identity of the trace re-source' },
    { title: 'Synthesize', detail: 'verify surviving findings against code' },
  ],
}

const WORKTREE = '/data/projects/gascity/.claude/worktrees/object-front-doors'

const CONTEXT = [
  'You are adversarially reviewing an in-progress precursor commit on a LIVE session reconciler.',
  'Repo worktree: ' + WORKTREE,
  'Diff (read-only): cd ' + WORKTREE + ' && git --no-pager diff HEAD -- cmd/gc/session_reconciler.go cmd/gc/city_runtime.go',
  'You may read any file. Do NOT modify anything.',
  '',
  'THE CHANGE (precursor to lockstep-drop Step 5b):',
  'reconcileSessionBeadsTracedWithNamedDemand now returns (int, map[string]sessionpkg.Info) instead of int.',
  'The second value is the reconciler final infoByID snapshot. Its two wrapper functions',
  '(reconcileSessionBeads, reconcileSessionBeadsTraced) discard the map and still return int. The prod caller',
  'beadReconcileTick (city_runtime.go) captures it as finalSessionInfo and passes it to',
  'recordReconcileTraceResults, which previously read the per-session terminal trace fields',
  'state/sleep_reason from the raw open bead metadata (open[i].Metadata["state"]/["sleep_reason"]) and now reads',
  'them from finalSessionInfo[bead.ID].MetadataState / .SleepReason, with a per-bead fallback to bead.Metadata',
  'when the ID is absent from the map (or the map is nil).',
  '',
  'WHY it is byte-identical AT THIS COMMIT (the mirror writes are all still present here — they are deleted in',
  'the NEXT commit, 5b): open shares Metadata maps with the reconciler ordered working set, and infoByID tracks',
  'the same forward-pass folds, so for every open bead finalSessionInfo[bead.ID].MetadataState equals',
  'open[bead].Metadata["state"] (and likewise sleep_reason) at return time. So the trace records the same values',
  'as before. This commit is a pure re-source that unlocks deleting the mirror writes later.',
  '',
  'Claims to REFUTE:',
  'A. Info.MetadataState is a verbatim mirror of Metadata["state"] and Info.SleepReason of Metadata["sleep_reason"]',
  '   (internal/session/info_store.go). NOT Info.State (normalized/closed-blanked) — that would be a bug.',
  'B. Every return path of the reconciler returns a correct second value: the early pre-snapshot abort returns nil',
  '   (infoByID not built), every post-snapshot abort and the normal path return infoByID. No return was missed',
  '   (the function has nested closures with their own returns — confirm none of the reconciler own-returns still',
  '   returns a single value, and no closure return was wrongly changed).',
  'C. infoByID keys by session bead ID and covers every open bead (built at tick entry from ordered=topoOrder(open),',
  '   never deleted), so finalSessionInfo[open[i].ID] hits for every open session; the fallback only triggers on',
  '   the nil-map pre-abort or a genuinely absent bead, and there it reproduces the exact old read.',
  'D. At THIS commit (mirrors intact) the recorded state/sleep_reason are byte-identical to pre-change for every',
  '   open bead, including drain-ack/close-transitioned ones and sessions closed mid-tick.',
  'E. The two wrappers correctly discard the map and preserve their int return + all their callers still compile;',
  '   the prod caller passes finalSessionInfo through; the trace test bare-call still compiles (Go allows',
  '   discarding all returns in an expression statement).',
].join('\n')

const SCHEMA = {
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
          claim_broken: { type: 'string' },
          detail: { type: 'string' },
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
    key: 'returns-and-coverage',
    focus: [
      'REFUTE claims B and C. Enumerate EVERY return statement inside reconcileSessionBeadsTracedWithNamedDemand',
      '(not its nested closures like the rollback helper, infoLookup, cachedSessionPeek). Confirm each now returns',
      'two values with the correct second value (nil only where infoByID is not yet in scope; infoByID otherwise).',
      'Confirm no closure return was accidentally changed and no own-return was missed (would fail to compile — but',
      'verify the intent too: an abort that returns nil where infoByID exists is acceptable but note it). Then',
      'verify infoByID is built from ordered=topoOrder(open) covering every open bead ID, keyed by ID, never',
      'deleted, so finalSessionInfo covers all open sessions.',
    ],
  },
  {
    key: 'equivalence-and-fallback',
    focus: [
      'REFUTE claims A and D. Confirm in internal/session/info_store.go that Info.MetadataState = Metadata["state"]',
      'and Info.SleepReason = Metadata["sleep_reason"] VERBATIM (not Info.State, which is normalized). Confirm the',
      'new recordReconcileTraceResults reads info.MetadataState/info.SleepReason (not info.State) and the fallback',
      'reads bead.Metadata["state"]/["sleep_reason"] exactly as before. Prove that at THIS commit (mirror writes',
      'still present) the value recorded per open bead is identical to the pre-change value — including the case of',
      'a session that was drain-ack finalized or closed this tick (open shares maps with ordered; infoByID folds).',
      'Try to find ANY open bead whose finalSessionInfo state would differ from its shared-map metadata at return.',
    ],
  },
]

phase('Refute')
const lensResults = await parallel(LENSES.map(l => () =>
  agent(
    [CONTEXT, '', '=== YOUR LENS: ' + l.key + ' ===', l.focus.join('\n'), '',
     'Be a skeptic; report a finding only with a concrete divergence or compile/coverage gap. Else verdict CLEAN,',
     'empty findings, one-line note. Return ONLY the structured object.'].join('\n'),
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
   'speculative ones. Give the final verdict for whether this precursor is byte-identical and safe to commit.', '',
   packed, '', 'Return ONLY the structured object (findings = CONFIRMED survivors only).'].join('\n'),
  { label: 'synthesize', phase: 'Synthesize', schema: SCHEMA, model: 'fable', effort: 'high' }
)

return { lensResults, synthesis }
