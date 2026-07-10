package internal

// Per-call audit trail. Before this, there was no record of which tool got
// called with what arguments — fine for correctness, not for debugging a
// confusing session or noticing a client hammering one tool. stderr only:
// stdout is the JSON-RPC stream and must never carry anything else.

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

// AuditWriter is where every invocation is recorded. A package var, swappable
// in tests, mirroring the KubectlExec pattern in kubectl.go.
var AuditWriter io.Writer = os.Stderr

// AuditNow is swappable in tests so a log line's timestamp is assertable.
var AuditNow = time.Now

// AuditLog records one tool invocation: timestamp, tool name, and its
// arguments with every value passed through Redact — an argument is
// caller-supplied text and, however unlikely for our regex-validated inputs
// today, gets the same treatment as any other untrusted string.
func AuditLog(tool string, args map[string]any) {
	fmt.Fprintf(AuditWriter, "%s tool=%s %s\n", AuditNow().UTC().Format(time.RFC3339), tool, redactArgs(args))
}

func redactArgs(args map[string]any) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic order — readable logs, assertable tests
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, Redact(fmt.Sprint(args[k]))))
	}
	return strings.Join(parts, " ")
}
