// argo-workflows-mcp runs a local stdio MCP server over the argoproj.io/v1alpha1
// Workflow CRD, so any Claude surface (CLI, IDE, desktop) can drive read-only
// Argo Workflows investigation through typed tools instead of `argo`/`kubectl`
// output-scraping.
//
// Design, learned from hxdrenv-operator's `hxdr mcp` (see internal_docs/ there):
//   - Phase 1 is read-only ONLY. No submit/retry/terminate/delete tool is
//     defined anywhere in this binary — not gated by a flag, simply absent.
//   - Stdio-only: binds nothing on the network, accepts no kubeconfig/context
//     param. It inherits the ambient kubeconfig context — that is the documented
//     blast-radius boundary.
//   - Every kubectl call is 15s-bounded (internal/kubectl.go) and every result is
//     redacted (internal/redact.go) before it reaches the model.
//   - Namespace/workflow-name args are validated against an RFC1123 regex
//     BEFORE they reach kubectl argv (internal/validate.go) — closes flag
//     injection, e.g. a name of "--all-namespaces".
package main

import (
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/term"

	"argo-workflows-mcp/mcp"
)

const version = "0.1.0"

func main() {
	s := server.NewMCPServer("argo-workflows-mcp", version)
	mcp.Register(s)

	// A human running this directly sees a silent, blocking process — it's a
	// stdio server waiting for an MCP client to speak JSON-RPC on stdin. Hint on
	// STDERR only; stdout is the JSON-RPC stream and must never carry anything else.
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "argo-workflows-mcp: stdio MCP server ready — waiting for an MCP client on stdin.")
		fmt.Fprintln(os.Stderr, "Register it once in .mcp.json:")
		fmt.Fprintln(os.Stderr, `  { "mcpServers": { "argo-workflows": { "command": "argo-workflows-mcp" } } }`)
		fmt.Fprintln(os.Stderr, "Read-only: list_workflows, get_workflow, workflow_logs, diagnose. Press Ctrl-C to exit.")
	}

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintln(os.Stderr, "argo-workflows-mcp: fatal:", err)
		os.Exit(1)
	}
}
