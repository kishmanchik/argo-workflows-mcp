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

## Tool catalog (closed allowlist — 4 tools, all read-only)

| Tool | kubectl verb | Purpose |
|---|---|---|
| `list_workflows` | `get` | Namespace-scoped list, optional phase filter |
| `get_workflow` | `get` | Full status for one workflow, including a per-node (step) breakdown |
| `workflow_logs` | `logs` | Tails every pod for one workflow, selected by Argo's own `workflows.argoproj.io/workflow` label — never a free-form pod name |
| `diagnose` | `get` | One-shot aggregator: every non-Succeeded workflow + first failed step per Failed/Error one |

## Build / test / run

```bash
go build ./...
go test ./...            # 21 tests: validate, redact, inspect (canned kubectl JSON), tool catalog
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
