package internal

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"jwt", "token: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PYE7"},
		{"aws access key", "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE"},
		{"bearer", "Authorization: Bearer abcdefghijklmnopqrstuvwxyz012345"},
		{"url credential", "postgres://myuser:sup3rSecret@db.internal:5432/hive"},
	}
	for _, c := range cases {
		got := Redact(c.in)
		if ContainsCredentialShape(got) {
			t.Errorf("%s: Redact(%q) = %q still contains a credential shape", c.name, c.in, got)
		}
	}
}

// TestRedact_LongOpaqueBlob checks reLongOpaque directly — ContainsCredentialShape
// doesn't cover it, so a case relying on that helper alone would pass even if this
// regex were deleted entirely. The blob is >=64 chars: the floor was deliberately
// raised from 40 after live-testing against real Argo Workflows data (see
// internal/redact.go) — a shorter blob wouldn't exercise the current threshold.
func TestRedact_LongOpaqueBlob(t *testing.T) {
	blob := "aB3dE7fG9hJ1kL4mN6pQ8rS0tU2vW5xY7zA9bC1dE3fG5hK8mN2pQ4rS6tU8vW0x" // 65 chars
	in := "secret=" + blob
	got := Redact(in)
	if strings.Contains(got, blob) {
		t.Errorf("expected the long opaque blob to be redacted, got: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected a [REDACTED] marker, got: %q", got)
	}
}

// TestRedact_TrailingPaddingOnlyStillCaught guards the first live-testing fix:
// `=` was narrowed to trailing base64 padding (0-2 chars), never mid-string. A
// real base64-encoded secret only ever has padding at the end, so this must
// still be redacted after that change.
func TestRedact_TrailingPaddingOnlyStillCaught(t *testing.T) {
	// base64 of a 60-byte string, real trailing "==" padding, no interior "=".
	secret := "c3VwZXItc2VjcmV0LWFwaS1rZXktbWF0ZXJpYWwtdGhhdC1pcy1yZWFzb25hYmx5LWxvbmctMTIzNA=="
	got := Redact("token=" + secret)
	if strings.Contains(got, secret) {
		t.Errorf("expected a real base64 secret (trailing padding) to still be redacted, got: %q", got)
	}
}

// TestRedact_KeyValueNotGlobbedIntoBlob is the second live-testing fix: `=`
// used to be part of the opaque-blob character class, so "workflow=<name>"
// collapsed into ONE match spanning our own "workflow=" label plus the name —
// real output seen against a live cluster. Interior "=" (not base64 padding)
// must no longer glue a key onto its value.
func TestRedact_KeyValueNotGlobbedIntoBlob(t *testing.T) {
	in := "workflow=technodigit-hspc-to-ogc3dtiles-9k8jf namespace=hxdr-processing"
	got := Redact(in)
	if got != in {
		t.Errorf("expected key=value formatting to survive redaction untouched, got: %q want: %q", got, in)
	}
}

// TestRedact_RealisticArgoNodeNamesSurvive: real Argo-generated node/pod names
// (workflow + template + retry/expansion suffixes) observed up to ~58 chars
// against a live cluster. The 64-char floor exists specifically so these don't
// false-trigger.
func TestRedact_RealisticArgoNodeNamesSurvive(t *testing.T) {
	names := []string{
		"technodigit-hspc-to-ogc3dtiles-9k8jf",
		"generate-volume-2-dr-luciad-meshup-3195637692",
		"step-1-dr-technodigit-microservices-retry-node-expansion-7",
	}
	for _, n := range names {
		if got := Redact(n); got != n {
			t.Errorf("expected realistic Argo node name to survive redaction, got: %q want: %q", got, n)
		}
	}
}

func TestRedactLeavesOrdinaryTextAlone(t *testing.T) {
	in := "pod hive-secret-fetch-0 is CrashLoopBackOff: exit code 1, restart count 5"
	if got := Redact(in); got != in {
		t.Errorf("Redact mangled ordinary text: got %q, want %q", got, in)
	}
}

func TestRedactEmptyString(t *testing.T) {
	if got := Redact(""); got != "" {
		t.Errorf("Redact(\"\") = %q, want \"\"", got)
	}
}
