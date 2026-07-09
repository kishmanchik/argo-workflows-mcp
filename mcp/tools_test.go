package mcp

import "testing"

// TestReadTools_ClosedAllowlist is the drift guard: it fails the build the
// moment anyone registers a tool this server wasn't designed to have, or gives
// a registered tool a verb outside {get, logs}. Mirrors hxdrenv-operator's
// TestMCPReadTools_ClosedAllowlist — the source of truth is this test, not a
// count in a doc, so the catalog can't silently grow a write tool.
func TestReadTools_ClosedAllowlist(t *testing.T) {
	wantNames := map[string]bool{
		"list_workflows": true,
		"get_workflow":   true,
		"workflow_logs":  true,
		"diagnose":       true,
	}
	allowedVerbs := map[string]bool{"get": true, "logs": true}

	tools := ReadTools()
	if len(tools) != len(wantNames) {
		t.Fatalf("ReadTools() returned %d tools, want exactly %d", len(tools), len(wantNames))
	}
	seen := map[string]bool{}
	for _, tool := range tools {
		if !wantNames[tool.name] {
			t.Errorf("unexpected tool %q registered — not in the closed allowlist", tool.name)
		}
		if !allowedVerbs[tool.verb] {
			t.Errorf("tool %q declares verb %q, outside the read-only allowlist {get, logs}", tool.name, tool.verb)
		}
		if seen[tool.name] {
			t.Errorf("tool %q registered more than once", tool.name)
		}
		seen[tool.name] = true
	}
	for name := range wantNames {
		if !seen[name] {
			t.Errorf("expected tool %q was not registered", name)
		}
	}
}

// TestReadTools_NoMutatingToolEver guards the design invariant directly: no
// tool name in this package may ever be one of the well-known Argo mutating
// verbs, even if a future edit adds one to ReadTools by mistake.
func TestReadTools_NoMutatingToolEver(t *testing.T) {
	forbidden := map[string]bool{
		"submit_workflow":    true,
		"retry_workflow":     true,
		"terminate_workflow": true,
		"delete_workflow":    true,
		"resubmit_workflow":  true,
		"suspend_workflow":   true,
		"resume_workflow":    true,
	}
	for _, tool := range ReadTools() {
		if forbidden[tool.name] {
			t.Errorf("mutating tool %q must never be defined in this Phase-1 read-only server", tool.name)
		}
	}
}

// TestReadTools_AnnotateAsReadOnly guards against mcp-go's own default: it sets
// DestructiveHint=true unless a tool opts out. An MCP client uses these hints to
// decide whether to prompt for confirmation — a read tool advertised as
// destructive defeats the point of a read-only allowlist just as surely as
// actually registering a write tool would.
func TestReadTools_AnnotateAsReadOnly(t *testing.T) {
	for _, tool := range ReadTools() {
		ann := tool.tool.Annotations
		if ann.ReadOnlyHint == nil || !*ann.ReadOnlyHint {
			t.Errorf("tool %q: ReadOnlyHint must be true", tool.name)
		}
		if ann.DestructiveHint == nil || *ann.DestructiveHint {
			t.Errorf("tool %q: DestructiveHint must be false", tool.name)
		}
	}
}
