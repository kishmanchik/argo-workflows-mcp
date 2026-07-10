package internal

// Single redaction chokepoint. Every string returned to the model MUST pass
// through Redact first — workflow status messages and pod logs are untrusted,
// attacker-influenceable content that can carry credentials. Learned from
// hxdrenv-operator's pkg/health/redact.go: conservative, well-known token shapes
// only, so ordinary diagnostic text isn't mangled.

import "regexp"

var (
	reJWT       = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{6,}\.[A-Za-z0-9_-]{6,}\.[A-Za-z0-9_-]{6,}`)
	reAWSAccess = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)
	reBearer    = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]{12,}`)
	reURLCred   = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/\s:@]*:[^/\s@]+@`)
	// reLongOpaque catches generic long random-looking strings a more specific
	// pattern above didn't. Two things learned from live-testing against real
	// Argo Workflows data, where node/pod names commonly run 40-60 chars
	// (workflow name + template + retry/expansion suffixes):
	//   - `=` is base64 PADDING, which only ever appears 0-2 chars at the very
	//     end of a real token, never mid-string. The old class included `=`
	//     as an interior character too, so "workflow=some-long-name" collapsed
	//     into one match — the "=" glued our own "key=value" formatting into
	//     the blob. Restricting it to a trailing `={0,2}` closes that false
	//     positive with no loss of real-secret detection (real base64 never
	//     has interior `=` anyway).
	//   - the floor is 64, not 40: JWTs/AWS keys/GH tokens are already caught
	//     by their own patterns above, so this catch-all mainly exists for
	//     generic long opaque strings (raw API keys, session tokens), and 64
	//     gives real Argo-generated identifiers (observed up to ~58 chars)
	//     room to not false-trigger.
	reLongOpaque = regexp.MustCompile(`\b[A-Za-z0-9+/_-]{64,}={0,2}`)
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
