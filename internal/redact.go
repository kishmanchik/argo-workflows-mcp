package internal

// Single redaction chokepoint. Every string returned to the model MUST pass
// through Redact first — workflow status messages and pod logs are untrusted,
// attacker-influenceable content that can carry credentials. Learned from
// hxdrenv-operator's pkg/health/redact.go: conservative, well-known token shapes
// only, so ordinary diagnostic text isn't mangled.

import "regexp"

var (
	reJWT        = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{6,}\.[A-Za-z0-9_-]{6,}\.[A-Za-z0-9_-]{6,}`)
	reAWSAccess  = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	reBearer     = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]{12,}`)
	reURLCred    = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/\s:@]*:[^/\s@]+@`)
	reLongOpaque = regexp.MustCompile(`\b[A-Za-z0-9+/=_-]{40,}\b`)
)

// Redact is the canonical scrubber for anything heading back to the model.
func Redact(s string) string {
	if s == "" {
		return s
	}
	s = reJWT.ReplaceAllString(s, "[REDACTED]")
	s = reAWSAccess.ReplaceAllString(s, "[REDACTED]")
	s = reBearer.ReplaceAllString(s, "bearer [REDACTED]")
	s = reURLCred.ReplaceAllString(s, "$1[REDACTED]@")
	s = reLongOpaque.ReplaceAllString(s, "[REDACTED]")
	return s
}

// ContainsCredentialShape is used in tests to assert nothing secret-shaped
// survived redaction.
func ContainsCredentialShape(s string) bool {
	return reJWT.MatchString(s) || reAWSAccess.MatchString(s) || reBearer.MatchString(s)
}
