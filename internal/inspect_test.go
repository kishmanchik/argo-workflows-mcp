package internal

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// withKubectl swaps KubectlExec for the test duration, restoring it after.
// Mirrors hxdrenv-operator's pkg/health/repair_safety_test.go withKubectl helper —
// the parse/render logic here is otherwise untestable without a live cluster.
func withKubectl(t *testing.T, fn func(args ...string) ([]byte, error)) {
	orig := KubectlExec
	KubectlExec = fn
	t.Cleanup(func() { KubectlExec = orig })
}

func canned(items []workflowItem) []byte {
	b, _ := json.Marshal(workflowList{Items: items})
	return b
}

func TestListWorkflows_RejectsInvalidNamespace(t *testing.T) {
	if _, err := ListWorkflows("--all-namespaces", ""); err == nil {
		t.Fatal("expected error for invalid namespace, got nil")
	}
}

func TestListWorkflows_RejectsInvalidPhase(t *testing.T) {
	if _, err := ListWorkflows("dev1", "Bogus"); err == nil {
		t.Fatal("expected error for invalid phase, got nil")
	}
}

func TestListWorkflows_FormatsAndFilters(t *testing.T) {
	withKubectl(t, func(args ...string) ([]byte, error) {
		items := []workflowItem{
			mkItem("wf-a", "Succeeded", "3/3", "2026-07-01T00:00:00Z"),
			mkItem("wf-b", "Failed", "1/3", "2026-07-02T00:00:00Z"),
		}
		return canned(items), nil
	})
	out, err := ListWorkflows("dev1", "Failed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "wf-b") || strings.Contains(out, "wf-a") {
		t.Errorf("phase filter not applied, got: %q", out)
	}
}

func TestListWorkflows_EmptyNamespace(t *testing.T) {
	withKubectl(t, func(args ...string) ([]byte, error) {
		return canned(nil), nil
	})
	out, err := ListWorkflows("dev1", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "no workflows found") {
		t.Errorf("expected empty-namespace message, got: %q", out)
	}
}

func TestListWorkflows_KubectlErrorPropagates(t *testing.T) {
	withKubectl(t, func(args ...string) ([]byte, error) {
		return nil, errors.New("connection refused")
	})
	if _, err := ListWorkflows("dev1", ""); err == nil {
		t.Fatal("expected kubectl error to propagate, got nil")
	}
}

func TestGetWorkflow_ReportsFailedNode(t *testing.T) {
	withKubectl(t, func(args ...string) ([]byte, error) {
		item := mkItem("wf-b", "Failed", "1/3", "2026-07-02T00:00:00Z")
		item.Status.Nodes = map[string]workflowNode{
			"wf-b-1": {DisplayName: "step-one", TemplateName: "build", Phase: "Succeeded"},
			"wf-b-2": {DisplayName: "step-two", TemplateName: "deploy", Phase: "Failed", Message: "connection refused to db.internal"},
		}
		b, _ := json.Marshal(item)
		return b, nil
	})
	out, err := GetWorkflow("dev1", "wf-b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "step-two") || !strings.Contains(out, "Failed") {
		t.Errorf("expected failed node detail, got: %q", out)
	}
}

func TestGetWorkflow_RejectsInvalidName(t *testing.T) {
	if _, err := GetWorkflow("dev1", "--terminate"); err == nil {
		t.Fatal("expected error for invalid workflow name, got nil")
	}
}

func TestWorkflowLogs_UsesWorkflowLabelSelector(t *testing.T) {
	var gotArgs []string
	withKubectl(t, func(args ...string) ([]byte, error) {
		gotArgs = args
		return []byte("pod/wf-b-2[main]: doing the thing\n"), nil
	})
	out, err := WorkflowLogs("dev1", "wf-b", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "doing the thing") {
		t.Errorf("expected log content, got: %q", out)
	}
	found := false
	for _, a := range gotArgs {
		if a == "workflows.argoproj.io/workflow=wf-b" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the workflow label selector in kubectl args, got: %v", gotArgs)
	}
}

func TestWorkflowLogs_RedactsSecrets(t *testing.T) {
	withKubectl(t, func(args ...string) ([]byte, error) {
		return []byte("connecting with AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n"), nil
	})
	out, err := WorkflowLogs("dev1", "wf-b", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ContainsCredentialShape(out) {
		t.Errorf("expected logs to be redacted, got: %q", out)
	}
}

func TestWorkflowLogs_RejectsExcessiveTail(t *testing.T) {
	if _, err := WorkflowLogs("dev1", "wf-b", 5000); err == nil {
		t.Fatal("expected error for tail exceeding max, got nil")
	}
}

func TestDiagnose_SkipsSucceededOnlySurfacesUnhealthy(t *testing.T) {
	withKubectl(t, func(args ...string) ([]byte, error) {
		items := []workflowItem{
			mkItem("wf-ok", "Succeeded", "3/3", "2026-07-01T00:00:00Z"),
			mkItem("wf-broken", "Failed", "1/3", "2026-07-02T00:00:00Z"),
		}
		items[1].Status.Nodes = map[string]workflowNode{
			"n1": {DisplayName: "step-one", TemplateName: "build", Phase: "Failed", Message: "boom"},
		}
		return canned(items), nil
	})
	out, err := Diagnose("dev1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "wf-ok") {
		t.Errorf("expected Succeeded workflow to be excluded, got: %q", out)
	}
	if !strings.Contains(out, "wf-broken") || !strings.Contains(out, "step-one") {
		t.Errorf("expected broken workflow + failed step detail, got: %q", out)
	}
}

func TestFirstFailedNode_DeterministicAcrossCalls(t *testing.T) {
	nodes := map[string]workflowNode{
		"z-node": {DisplayName: "z", Phase: "Failed"},
		"a-node": {DisplayName: "a", Phase: "Failed"},
	}
	got1 := firstFailedNode(nodes)
	got2 := firstFailedNode(nodes)
	if got1 == nil || got2 == nil || got1.DisplayName != got2.DisplayName {
		t.Fatalf("expected deterministic result across calls, got %v then %v", got1, got2)
	}
	if got1.DisplayName != "a" {
		t.Errorf("expected name-sorted first failed node 'a', got %q", got1.DisplayName)
	}
}

func mkItem(name, phase, progress, started string) workflowItem {
	var it workflowItem
	it.Metadata.Name = name
	it.Metadata.CreationTimestamp = started
	it.Status.Phase = phase
	it.Status.Progress = progress
	it.Status.StartedAt = started
	return it
}
