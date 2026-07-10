# CLAUDE.md

Guidance for working in this repo. Keep this file short — link out for the "why", state the
rule and its enforcement here.

## Build / test

```bash
go build ./...
go vet ./...
go test ./...
```

## Invariants — do not reintroduce a violation of these

Each rule names the test that fails if it's violated. If you're about to change something that
would break the enforcement, stop and reconsider the change, don't delete the test.

1. **No mutating tool may ever be registered** (submit/retry/terminate/delete/suspend/resume).
   Enforced by `TestReadTools_ClosedAllowlist` and `TestReadTools_NoMutatingToolEver` in
   `mcp/tools_test.go`. If a guarded write is ever added, it must follow `hxdr mcp`'s `repair`
   tool discipline — one narrow tool, args constrained so the model can't supply a
   manifest/patch body, absent from the registry unless explicitly enabled at startup.
2. **Namespace/workflow-name arguments are validated before they reach kubectl argv.**
   Enforced by `TestValidateNamespace`/`TestValidateWorkflowName` in `internal/validate_test.go`
   (must reject anything starting with `-`, closing flag injection).
3. **Every tool result is redacted before it reaches the model.** Enforced by
   `TestWorkflowLogs_RedactsSecrets` in `internal/inspect_test.go`.
4. **Every tool is annotated read-only** (`ReadOnlyHint=true`, `DestructiveHint=false`) — a
   default `mcp-go` gets wrong. Enforced by `TestReadTools_AnnotateAsReadOnly`.
5. **Errors returned to the model carry a stable category prefix**
   (`internal.FormatToolError`/`ClassifyError`), not a bare Go error string. Enforced by
   `TestClassifyError` in `internal/errors_test.go`.
6. **Cluster-sourced content is framed to the model as untrusted data, not instructions** — every
   successful tool result is wrapped (`mcp/tools.go`'s `untrustedOpen`/`untrustedClose`).
   Enforced by `TestListWorkflows_SuccessIsWrappedAsUntrustedData`.
7. **A confirmed-but-uncertain result is marked degraded, never plain success.** Enforced by
   `TestWorkflowLogs_EmptyIsDegradedNotCleanSuccess` and
   `TestDiagnose_FailedWithoutMatchingNodeIsDegraded` in `internal/inspect_test.go`.
8. **Every tool invocation is audit-logged (name + redacted args) to stderr.** Enforced by
   `TestCallingATool_WritesAnAuditLine` in `mcp/handlers_test.go`.
9. **The opaque-blob redaction pattern's `=` is trailing-padding-only, never interior** — real
   base64 padding is always 0-2 trailing chars; an interior `=` used to glob our own
   `key=value` output formatting into the redaction (found via live-testing against a real
   cluster). Enforced by `TestRedact_KeyValueNotGlobbedIntoBlob` in `internal/redact_test.go`.
   Do not add `=` back into the interior character class.

## Design context

See `README.md` for what was borrowed from `hxdrenv-operator`'s `hxdr mcp` and what was
deliberately rejected. This is a Phase-1, read-only-only prototype — there is no flag that
enables writes, because there is no write tool defined anywhere in the binary.
