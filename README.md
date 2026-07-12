# argo-workflows-mcp

A local **stdio** MCP server over the `argoproj.io/v1alpha1` Workflow CRD, so a Claude surface
(CLI, IDE, desktop) can investigate Argo Workflows through typed tools instead of scraping
`argo`/`kubectl` output.

**Status: prototype / learning project.** Built after surveying the existing community Argo
Workflows MCP servers and the official Argo CD MCP server — see
[`../hxdrenv-operator/internal_docs/argo-workflows-mcp-landscape.md`](../hxdrenv-operator/internal_docs/argo-workflows-mcp-landscape.md)
for that research. None of the surveyed projects had a closed, tested tool allowlist, so this
one is built from scratch following the design `hxdrenv-operator`'s own `hxdr mcp` already
ships (see [`../hxdrenv-operator/internal_docs/hxdr-mcp-current-state.md`](../hxdrenv-operator/internal_docs/hxdr-mcp-current-state.md)).

## Design — what was actually borrowed vs. deliberately not

| Practice | Borrowed from | Applied here |
|---|---|---|
| Phase 1 is read-only, full stop | `hxdr mcp`'s "read-only first, blast radius zero" | No submit/retry/terminate tool exists anywhere in this binary — not flag-gated, just absent |
| Closed allowlist enforced by a test, not a doc | `TestMCPReadTools_ClosedAllowlist` | `TestReadTools_ClosedAllowlist` + `TestReadTools_NoMutatingToolEver` in `mcp/tools_test.go` |
| Validate at the boundary before any kubectl call | `envNameRe`/`serviceNameRe` closing flag-injection | `internal/validate.go` — namespace/name must be a bare RFC1123 label; rejects e.g. `--all-namespaces` before it reaches argv |
| Redaction as the single chokepoint | `redactString` | `internal/redact.go` — every tool result passes through `Redact` |
| Bounded kubectl, mockable for tests | `kubectlExec` package var | `internal/kubectl.go` — 15s timeout, swappable in tests |
| Stdio-only, no network bind, no kubeconfig param | `hxdr mcp`'s "inherits the ambient kubeconfig context" | Same here — the active kubeconfig context is the blast-radius boundary |
| **Not** borrowed: auto-generate one tool per OpenAPI endpoint | *(anti-pattern, from `kushthedude/argo-workflows-mcp`)* | Explicitly rejected — every tool here is hand-curated |
| **Not** borrowed: HTTP/SSE as the default transport | *(from `Heapy/argo-workflows-mcp`)* | stdio only, for the same "no new network attack surface" reason `hxdr mcp` chose it |

One correctness fix worth calling out: `mark3labs/mcp-go` defaults every tool's `DestructiveHint`
annotation to `true` unless a tool opts out. A read-only tool advertised as destructive makes an
MCP client over-prompt for confirmation — `mcp/tools.go`'s `readTool()` helper sets
`ReadOnlyHint=true, DestructiveHint=false, IdempotentHint=true` on all four tools, tested by
`TestReadTools_AnnotateAsReadOnly`. Verified end-to-end with a live `tools/list` call, not just
unit tests.

Two more practices adopted from surveying other mature MCP servers (kept intentionally generic —
none of this is tied to a specific one):

- **Categorized errors, not bare Go error strings.** `internal/errors.go`'s `ClassifyError` maps
  any error onto a small stable set (`invalid_input`, `not_found`, `timeout`, `unavailable`,
  `other`); `mcp/tools.go` prefixes every error result with `[category]` via `FormatToolError`,
  so the model gets a consistent, inspectable shape instead of guessing from prose. Tested by
  `internal/errors_test.go` and, end-to-end through the actual handlers, `mcp/handlers_test.go`.
- **Untrusted-data framing on every successful result.** `mcp/tools.go` wraps all cluster-sourced
  content (workflow status messages, pod logs) in `<untrusted_cluster_data>` delimiters — the
  same principle `hxdr mcp`'s own design doc states ("tool output is framed to the model as
  untrusted data, never instructions") but as an explicit, testable wrapper here. Defense in
  depth on top of `Redact`, not a substitute for it. Tested by
  `TestListWorkflows_SuccessIsWrappedAsUntrustedData`.
- **A degraded status marker, distinct from error/success.** `internal/status.go`'s
  `Degraded()` marks a confirmed-but-uncertain observation (e.g. empty logs that might mean
  "never logged" or "pods already GC'd", or a workflow reporting Failed/Error with no matching
  node in `status.nodes` to corroborate it) — the call succeeded, but the result shouldn't read
  as confirmed-healthy. Tested by `TestWorkflowLogs_EmptyIsDegradedNotCleanSuccess` and
  `TestDiagnose_FailedWithoutMatchingNodeIsDegraded`.
- **Per-call audit logging.** `internal/audit.go`'s `AuditLog` records every tool invocation
  (timestamp, tool name, redacted arguments) to stderr — cheap, and there was previously zero
  record of what got called with what. Tested by `internal/audit_test.go` and, end-to-end,
  `TestCallingATool_WritesAnAuditLine`.

Both of the above were validated, and one real bug was found and fixed, by live-testing against
a real Argo Workflows install (a disposable local `kind` cluster, then a genuine live QA
environment): `internal/redact.go`'s catch-all "long opaque blob" pattern was catching real
workflow/node names. Two contributing causes, both fixed:
1. `=` was part of the opaque-blob character class (meant for base64 padding), so
   `workflow=<name>` collapsed into one match — the `=` glued our own `key=value` output
   formatting into the redaction. Fixed by restricting `=` to trailing padding only (0-2 chars,
   which is the only place real base64 padding ever appears) — zero loss of real-secret
   detection, since genuine base64 never has interior `=` anyway.
2. The 40-char floor was too low: Argo's generated node/pod names (workflow + template +
   retry/expansion suffixes) routinely exceed 40 chars in real deployments. Raised to 64 — JWTs
   /AWS keys/GitHub tokens are already caught by their own dedicated patterns above, so this
   catch-all mainly exists for generic long opaque strings, and 64 gives real identifiers
   (observed up to ~58 chars live) room to not false-trigger.

Tested by `TestRedact_KeyValueNotGlobbedIntoBlob`, `TestRedact_RealisticArgoNodeNamesSurvive`,
and `TestRedact_TrailingPaddingOnlyStillCaught` (a real base64 secret is still redacted).

## Correctness fixes from reading the real Argo Workflows source

After live-testing surfaced the redaction bug above, the actual `argoproj/argo-workflows` Go
source (not just observed live behavior) was read to check this tool's assumptions against
ground truth. Two real gaps found and fixed, both in `internal/nodes.go`:

- **`status.compressedNodes`.** When a workflow has many nodes, Argo's controller gzip+base64
  packs them into `status.compressedNodes` and leaves `status.nodes` empty (mirrors the
  controller's own `workflow/packer.DecompressWorkflow`: `base64.StdEncoding` then `gzip`).
  Without decoding this, `get_workflow`/`diagnose` would show "(no node status yet)" for a
  workflow that actually has full node data — misleading. `resolveNodes()` now decompresses it
  transparently. Only gzip (the default) is supported; a workflow using a non-default
  `WORKFLOW_COMPRESSION_ALGORITHM` (zstd/brotli) surfaces as an explicit degraded reason rather
  than silently showing nothing or pulling in extra non-stdlib dependencies for a rare case.
- **`status.offloadNodeStatusVersion`.** When node-status offload to an external DB is enabled,
  neither `nodes` nor `compressedNodes` carries the data at all — genuinely unreachable via
  kubectl. `resolveNodes()` reports this explicitly ("node status is offloaded to an external
  database... not visible via kubectl") instead of the same misleading "hasn't started yet".

Also added: `status.conditions` (`SpecWarning`/`SpecError`/`MetricsError`/`ArtifactGCError`) can
be true on a workflow whose `phase` alone looks healthy (`Succeeded`/`Running`). `get_workflow`
now surfaces any true problem condition, and `diagnose`'s definition of "unhealthy" now includes
a phase-healthy workflow flagged by one — a workflow that looks fine by phase but isn't. Tested
by `TestGetWorkflow_DecompressesCompressedNodes`, `TestGetWorkflow_OffloadedNodesAreDegradedNotEmpty`,
`TestDiagnose_SucceededWithProblemConditionIsSurfaced`, and `internal/nodes_test.go` generally.

Confirmed as **not** a gap: the real `argo-server`'s own live-log endpoint
(`server/workflow/workflow_server.go`) uses the exact same `workflows.argoproj.io/workflow`
label selector this tool already does, and has no archive fallback either — that only exists in
the UI layer via S3/artifact-repo credentials this tool deliberately doesn't manage. The
existing "degraded: pods may be GC'd" message for empty logs is accurate, not a corner cut.

## Minimal RBAC for a read-only ServiceAccount

Sourced from Argo's own `docs/security.md` (the `ui-user-read-only` Role) and the aggregated
`argo-aggregate-to-view` ClusterRole manifest — combine both for exactly what this tool needs:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: argo-workflows-mcp-readonly
  namespace: <namespace workflows run in>
rules:
- apiGroups: ["argoproj.io"]
  resources: ["workflows", "workflows/finalizers"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["pods", "pods/log"]
  verbs: ["get", "list", "watch"]
```

## Tool catalog (closed allowlist — 4 tools, all read-only)

| Tool | kubectl verb | Purpose |
|---|---|---|
| `list_workflows` | `get` | Namespace-scoped list, optional phase filter |
| `get_workflow` | `get` | Full status for one workflow, including a per-node (step) breakdown, decompressed transparently, plus any true problem condition |
| `workflow_logs` | `logs` | Tails every pod for one workflow, selected by Argo's own `workflows.argoproj.io/workflow` label — never a free-form pod name |
| `diagnose` | `get` | One-shot aggregator: every non-Succeeded workflow, plus any phase-healthy one flagged by a problem condition, plus first failed step per Failed/Error one |

## Build / test / run

```bash
go build ./...
go test ./...            # 47 tests: validate, redact, errors, status, audit, nodes, inspect, handlers, tool catalog
go build -o argo-workflows-mcp .
```

Wire it into a project's `.mcp.json`:

```json
{ "mcpServers": { "argo-workflows": { "command": "argo-workflows-mcp" } } }
```

It inherits whatever kubeconfig context is ambient on the machine — same model as `hxdr mcp`.

## Explicitly out of scope for this prototype

- **No write tools.** `submit_workflow`, `retry_workflow`, `terminate_workflow` are all
  unimplemented by design. If a guarded write is ever needed, it should follow `hxdr`'s
  `repair` tool discipline exactly: *one* narrow tool, args constrained so the model can't
  supply a manifest/patch body, absent from the registry unless explicitly enabled at startup
  (flag preferred over a bare env var, per `mcp-integration.md` §5.4), and audited.
- **No auth/token handling.** Unlike the official `argoproj-labs/mcp-for-argocd` (bearer token +
  multi-instance registry), this talks to the Workflow CRD directly via the ambient kubeconfig,
  so there's no separate Argo-server credential to manage — consistent with `hxdr mcp` never
  accepting a kubeconfig/context/role parameter.
- **No `argo` CLI dependency.** Everything goes through plain `kubectl get`/`kubectl logs` against
  the CRD and its pods, so the only runtime dependency is `kubectl` on PATH.
