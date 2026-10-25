# Bulk ordinary-label commands

`wbd labels` applies ordinary-label changes to either explicit Bead IDs or the
immediate children of one epic. It never changes `ctx:` labels. Eligible
stored issue types are exactly `task`, `bug`, `feature`, `chore`, `todo`,
`epic`, and `decision`, subject to the existing stored-membership policy. A
task may also be a supported ephemeral wisp; the wisp does not change its
stored issue type. Backend-internal types such as `agent`, `message`, or `gate`
are not supported by these commands.

```text
wbd labels add     (<id>... | --epic <id>) --labels <label,...>... --json
wbd labels remove  (<id>... | --epic <id>) --labels <label,...>... --json
wbd labels replace (<id>... | --epic <id>) (--labels <label,...>... | --clear) --json
```

Choose exactly one selection mode: one or more canonical positional IDs, or
one canonical epic ID. Explicit IDs may cross registered repository contexts;
contextless todos and empty-context epics are eligible when accepted by the
existing Hub membership policy. Neither selector consults active-scope
membership, current directory, status, or a visibility filter. Explicit
selection targets exactly the supplied IDs.

Epic selection resolves and freezes all immediate children before any writes.
The selected root is excluded. Closed children and children outside the active
scope are included. A nested epic is itself a target; its children are not
traversed. A policy-valid task wisp is eligible. An unsupported or otherwise
invalid record anywhere in either selection rejects the entire selection
during preflight, before any label write. An epic with no children succeeds
with zero matches. Later child attachments are not included in an in-flight
invocation. There is no recursive mode or future-child inheritance.

Label values may be repeated and comma-separated. The command trims surrounding
whitespace, deduplicates and sorts IDs and labels, and treats labels as
case-sensitive. Empty elements, unsafe controls, quoted labels, overlength
labels, and every normalized label beginning with exact lowercase `ctx:` are
rejected before writes. Commas separate labels and cannot be part of a label.
For `replace`, `--labels` is the complete ordinary-label set; `--clear` removes
all ordinary labels. These options are exclusive, and an empty string is not a
clear operation.

For example:

```sh
wbd labels add --epic epic-example --labels "needs-review,priority" --json
wbd labels remove work-a work-b --labels stale --json
wbd labels replace --epic epic-example --labels "area-api,ready" --json
wbd labels replace work-a work-b --clear --json
```

Successful JSON contains the operation, frozen `matched` count, successful
`applied` count, error count, and one result per target in canonical-ID order.
Each successful result has `status: "applied"` and sorted ordinary labels read
from that backend transaction's post-state. `applied` includes successful
no-ops: adding existing labels, removing absent labels, and replacing with the
already-current set all succeed idempotently. Empty selection also exits zero.

Parse, policy, or preflight errors exit 1, emit no result object on stdout, and
write the structured error to stderr. All selected targets are validated
before the first mutation, so an invalid or missing target prevents every
write. During execution, each target is atomic, but the whole selection is not
a transaction: processing continues after per-target failures, a complete
result is emitted, and the command exits 1 with a `bulk_label_partial_failure`
stderr summary. Failed targets have `status: "error"`, a structured backend
error, and `outcome_unknown: true`; their labels are omitted because the
subprocess may have failed after committing. This does not claim rollback.

Retrying the same intent converges only while target membership and user intent
remain unchanged; it does not protect intervening ordinary-label edits. An
epic retry resolves membership again. Use the reported explicit IDs to retry
precisely the original target set. Replacement preserves the transaction's
current context labels while replacing ordinary labels.

Deploy backend support before deploying the writer. The backend must support
ordinary-only replacement. An unsupported backend rejects replacement; do not
fall back to full-label replacement, which could overwrite protected context
identity. Add/remove/replace each retain per-Bead transaction boundaries, not
batch-wide rollback.
