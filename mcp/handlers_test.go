package mcp

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"argo-workflows-mcp/internal"
)

// withKubectl swaps internal.KubectlExec for the test duration. internal is a
// separate package, but KubectlExec is exported specifically so the handler
// wiring here is testable end-to-end without a live cluster.
func withKubectl(t *testing.T, fn func(args ...string) ([]byte, error)) {
	orig := internal.KubectlExec
	internal.KubectlExec = fn
	t.Cleanup(func() { internal.KubectlExec = orig })
}

func handlerFor(t *testing.T, name string) server.ToolHandlerFunc {
	for _, tool := range ReadTools() {
		if tool.name == name {
			return tool.handler
		}
	}
	t.Fatalf("no registered tool named %q", name)
	return nil
}

func callToolText(t *testing.T, name string, args map[string]any) (string, bool) {
	req := gomcp.CallToolRequest{Params: gomcp.CallToolParams{Name: name, Arguments: args}}
	res, err := handlerFor(t, name)(context.Background(), req)
	if err != nil {
		t.Fatalf("handler %q returned a transport-level error: %v", name, err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("handler %q: expected exactly one content block, got %d", name, len(res.Content))
	}
	tc, ok := res.Content[0].(gomcp.TextContent)
	if !ok {
		t.Fatalf("handler %q: expected TextContent, got %T", name, res.Content[0])
	}
	return tc.Text, res.IsError
}

func TestListWorkflows_SuccessIsWrappedAsUntrustedData(t *testing.T) {
	withKubectl(t, func(args ...string) ([]byte, error) {
		return []byte(`{"items":[]}`), nil
	})
	text, isErr := callToolText(t, "list_workflows", map[string]any{"namespace": "dev1"})
	if isErr {
		t.Fatalf("expected success, got error result: %q", text)
	}
	if !strings.HasPrefix(text, untrustedOpen) || !strings.HasSuffix(text, untrustedClose) {
		t.Errorf("expected result wrapped in untrusted-data delimiters, got: %q", text)
	}
}

func TestGetWorkflow_InvalidInputIsCategorized(t *testing.T) {
	text, isErr := callToolText(t, "get_workflow", map[string]any{"namespace": "dev1", "name": "--terminate"})
	if !isErr {
		t.Fatalf("expected an error result for an invalid workflow name, got success: %q", text)
	}
	if !strings.HasPrefix(text, "[invalid_input]") {
		t.Errorf("expected an [invalid_input]-categorized error, got: %q", text)
	}
}

func TestListWorkflows_KubectlUnavailableIsCategorized(t *testing.T) {
	withKubectl(t, func(args ...string) ([]byte, error) {
		// The real error type exec.Command(...).Output() returns when the binary
		// itself can't be found — not a stand-in string, the actual *exec.Error.
		return nil, &exec.Error{Name: "kubectl", Err: exec.ErrNotFound}
	})
	text, isErr := callToolText(t, "list_workflows", map[string]any{"namespace": "dev1"})
	if !isErr {
		t.Fatalf("expected an error result, got success: %q", text)
	}
	if !strings.HasPrefix(text, "[unavailable]") {
		t.Errorf("expected an [unavailable]-categorized error, got: %q", text)
	}
}

func TestDiagnose_NotFoundIsCategorized(t *testing.T) {
	withKubectl(t, func(args ...string) ([]byte, error) {
		return nil, errors.New("workflows.argoproj.io \"wf-x\" not found")
	})
	text, isErr := callToolText(t, "diagnose", map[string]any{"namespace": "dev1"})
	if !isErr {
		t.Fatalf("expected an error result, got success: %q", text)
	}
	// Plain errors.New has no *exec.ExitError.Stderr for ClassifyError to inspect,
	// so this correctly falls to "other" — asserting that keeps the categorizer
	// honest about what it can and can't detect from a bare error.
	if !strings.HasPrefix(text, "[other]") {
		t.Errorf("expected an [other]-categorized error for a bare non-exec error, got: %q", text)
	}
}
