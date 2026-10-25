# Ordinary-label manipulation

Use these instructions only for ordinary-label work through `wbd`. Never use
raw backend commands for Hub label mutation, and never add, remove, or replace
Hub-owned `ctx:` labels.

## Supported records and selection

The supported stored issue types are `task`, `bug`, `feature`, `chore`, `todo`,
`epic`, and `decision`, subject to existing Hub membership rules. A task wisp
is supported as a task with ephemeral storage. Internal backend types such as
`agent`, `message`, and `gate` are not supported. One unsupported, missing, or
policy-invalid target rejects the whole selection during preflight, before any
label write.

Choose exactly one selector:

```text
wbd labels add     (<id>... | --epic <id>) --labels <label,...>... --json
wbd labels remove  (<id>... | --epic <id>) --labels <label,...>... --json
wbd labels replace (<id>... | --epic <id>) (--labels <label,...>... | --clear) --json
```

Explicit IDs target exactly those canonical IDs and may cross repository
contexts. Epic selection freezes immediate children only: it excludes the
selected root, includes closed eligible children, and treats a nested epic as
a target without traversing its descendants. Both selectors ignore active
scope membership, current directory, status, and visibility filters. An empty
epic succeeds with zero targets; later attachments are not included in the
current invocation. There is no recursive selection or future-child
inheritance.

## Labels and safety

`--labels` can be repeated and values are comma-separated. Values are trimmed,
deduplicated, sorted, and case-sensitive; commas cannot be part of a label.
Empty elements, unsafe controls, quoted values, overlength labels, and any
normalized value beginning with exact lowercase `ctx:` are rejected before
writes, including attempts to remove an absent context label.

For `add` and `remove`, already-satisfied requests are successful no-ops. For
`replace`, `--labels` specifies the complete ordinary-label set; `--clear`
removes all ordinary labels. They are mutually exclusive, and an empty string
is not a clear shortcut. Replacement preserves the transaction-current
context labels.

```sh
wbd labels add --epic epic-example --labels "needs-review,priority" --json
wbd labels remove work-a work-b --labels stale --json
wbd labels replace --epic epic-example --labels "area-api,ready" --json
wbd labels replace work-a work-b --clear --json
```

## Results, failure, and retry

`--json` is required. Successful output reports `operation`, frozen `matched`,
successful `applied`, `errors`, and results sorted by canonical ID. A successful
result has `status: "applied"` and sorted ordinary `labels` from that update's
backend post-state. `applied` includes no-ops; it is not a changed-row count.
An empty epic exits zero.

Parse, policy, and preflight failures exit 1, write no result object to stdout,
and perform no target writes. Execution is atomic per target, not across the
whole selection. After a target-local error, processing continues and the
complete result is emitted; the command exits 1 with a
`bulk_label_partial_failure` stderr summary. Failed results have
`status: "error"`, a structured backend error, and `outcome_unknown: true`,
with `labels` omitted. A backend error or malformed response may occur after a
write committed; it does not mean rollback. Do not claim the whole selection
was rolled back.

Repeating the same add, remove, or replacement is idempotent if target
membership and intent have not changed. It does not isolate the operation from
intervening edits. An epic retry resolves membership again; to retry exactly
the original frozen target set, use the reported explicit IDs.

Deploy backend ordinary-only replacement support before the writer. An older
backend must reject replacement safely; never fall back to full-label
replacement, which can overwrite context identity. If replacement fails with
an uncertain outcome, inspect the reported targets before deciding whether to
retry.
