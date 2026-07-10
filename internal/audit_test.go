package internal

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func withAudit(t *testing.T) *bytes.Buffer {
	var buf bytes.Buffer
	origWriter, origNow := AuditWriter, AuditNow
	AuditWriter = &buf
	AuditNow = func() time.Time { return time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { AuditWriter, AuditNow = origWriter, origNow })
	return &buf
}

func TestAuditLog_RecordsToolAndArgs(t *testing.T) {
	buf := withAudit(t)
	AuditLog("list_workflows", map[string]any{"namespace": "dev1", "phase": "Failed"})
	got := buf.String()
	if !strings.Contains(got, "tool=list_workflows") {
		t.Errorf("expected tool name in audit line, got: %q", got)
	}
	if !strings.Contains(got, "namespace=dev1") || !strings.Contains(got, "phase=Failed") {
		t.Errorf("expected both arguments in audit line, got: %q", got)
	}
	if !strings.HasPrefix(got, "2026-07-10T00:00:00Z ") {
		t.Errorf("expected a leading RFC3339 UTC timestamp, got: %q", got)
	}
}

func TestAuditLog_RedactsArgValues(t *testing.T) {
	buf := withAudit(t)
	AuditLog("get_workflow", map[string]any{"name": "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE"})
	if ContainsCredentialShape(buf.String()) {
		t.Errorf("expected argument values to be redacted, got: %q", buf.String())
	}
}

func TestAuditLog_DeterministicArgOrder(t *testing.T) {
	buf := withAudit(t)
	AuditLog("list_workflows", map[string]any{"phase": "Failed", "namespace": "dev1"})
	first := buf.String()

	buf2 := withAudit(t)
	AuditLog("list_workflows", map[string]any{"namespace": "dev1", "phase": "Failed"})
	second := buf2.String()

	if first != second {
		t.Errorf("expected argument order to be deterministic regardless of map iteration, got %q vs %q", first, second)
	}
}
