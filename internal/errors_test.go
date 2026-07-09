package internal

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

func TestClassifyError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ErrorCategory
	}{
		{"validation sentinel", ValidateNamespace("--bad"), CategoryInvalidInput},
		{"wrapped validation sentinel", fmt.Errorf("boundary check failed: %w", ValidateWorkflowName("--bad")), CategoryInvalidInput},
		{"deadline exceeded", context.DeadlineExceeded, CategoryTimeout},
		{"wrapped deadline exceeded", fmt.Errorf("kubectl get workflows: %w", context.DeadlineExceeded), CategoryTimeout},
		{"missing binary", &exec.Error{Name: "kubectl", Err: exec.ErrNotFound}, CategoryUnavailable},
		{"exit error with NotFound stderr", &exec.ExitError{Stderr: []byte(`workflows.argoproj.io "wf-x" not found`)}, CategoryNotFound},
		{"exit error with unrelated stderr", &exec.ExitError{Stderr: []byte(`Unauthorized`)}, CategoryOther},
		{"generic error", errors.New("boom"), CategoryOther},
		{"unwrap-less re-description loses the original category", errors.New("wrapped: " + context.DeadlineExceeded.Error()), CategoryOther},
	}
	for _, c := range cases {
		if got := ClassifyError(c.err); got != c.want {
			t.Errorf("%s: ClassifyError() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFormatToolError(t *testing.T) {
	got := FormatToolError(ValidateNamespace("--bad"))
	if got == "" {
		t.Fatal("expected non-empty formatted error")
	}
	if got[0] != '[' {
		t.Errorf("expected a leading category tag, got: %q", got)
	}
}
