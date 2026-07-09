package internal

import "testing"

func TestRedact(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"jwt", "token: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PYE7"},
		{"aws access key", "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE"},
		{"bearer", "Authorization: Bearer abcdefghijklmnopqrstuvwxyz012345"},
		{"url credential", "postgres://myuser:sup3rSecret@db.internal:5432/hive"},
		{"long opaque blob", "secret=" + "aB3dE7fG9hJ1kL4mN6pQ8rS0tU2vW5xY7zA9bC1dE3fG5h"},
	}
	for _, c := range cases {
		got := Redact(c.in)
		if ContainsCredentialShape(got) {
			t.Errorf("%s: Redact(%q) = %q still contains a credential shape", c.name, c.in, got)
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
