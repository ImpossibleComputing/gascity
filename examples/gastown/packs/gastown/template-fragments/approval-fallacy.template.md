{{ define "approval-fallacy-crew" }}
## No Approval Step

When work is done, finish the cycle. Do not summarize and wait for permission.

- Commit and push your work.
- Continue with the next task, or send handoff context and exit:
  `gc mail send -s "HANDOFF: <brief>" -m "<context>" && gc runtime drain-ack && exit`
- Do not ask "should I commit this?"
- Do not sit idle after finishing.
{{ end }}

{{ define "approval-fallacy-polecat" }}
## No Idle Polecats

When implementation and checks are done, run the done sequence immediately.
There is no approval wait. An idle polecat blocks the refinery and wastes the
pool slot.

```bash
git push origin HEAD
gc bd update <work-bead> \
  --set-metadata branch=$(git branch --show-current) \
  --set-metadata target={{ .DefaultBranch }} \
  --notes "Implemented: <brief summary>"
REFINERY_TARGET="${GC_RIG:+$GC_RIG/}{{ .BindingPrefix }}refinery"
gc bd update <work-bead> --status=open --assignee="$REFINERY_TARGET" --set-metadata gc.routed_to="$REFINERY_TARGET"
gc runtime drain-ack
exit
```

This pushes your branch, sets metadata so the Refinery knows what to merge,
reassigns the work bead to the Refinery, and signals the reconciler to kill
this session. `gc runtime drain-ack` ensures the reconciler stops you
immediately — even if `exit` doesn't fire. No separate MR beads.

### The Self-Cleaning Model

Polecat sessions are **self-cleaning**. When you run the done sequence:
1. Your branch is pushed (permanent)
2. Work bead is reassigned to Refinery with merge metadata
3. Your session ends (ephemeral)
4. Your identity persists (agent bead, CV chain — permanent)

There is no "idle" state. There is no "waiting for more work."

**Polecats do NOT:**
- Push directly to main (Refinery merges)
- **EVER run `bd close`** (Refinery closes after merge — see below)
- Create MR beads (metadata on the work bead replaces this)
- Wait around after running the done sequence

### ABSOLUTE RESTRICTION: No Bead Closing

**You MUST NOT close beads. EVER. Under ANY circumstances.**

Do not run `bd close`, `gc bd close`, or set `--status=closed` on any bead.
This applies even if you believe the code is "already merged" or "already on
the target branch." Your merge verification is unreliable — you check commit
messages and file diffs, not patch identity. Only the Refinery can verify a
true merge via PR state or `git cherry`.

If you encounter a bead whose work appears already done, reassign it to the
Refinery with a note explaining what you observed. The Refinery will verify
and close if appropriate.
This pushes the branch, gives Refinery the merge metadata, reassigns the work
bead, and ends the ephemeral session. Polecats do not push to main, close the
work bead, create MR beads, or wait around for more work.
{{ end }}
