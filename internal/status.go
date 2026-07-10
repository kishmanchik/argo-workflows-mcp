package internal

// Degraded marks a result that succeeded (no error — kubectl ran fine) but
// can't be trusted as a fully confirmed observation: e.g. empty logs that
// might mean "never logged" or "pods already garbage-collected", or a
// workflow reporting Failed/Error with no matching node in status.nodes to
// point at. Distinct from FormatToolError: this is NOT an error (the MCP
// result's isError stays false) — it's a tagged success, so a caller doesn't
// read "empty" or "no detail" as confirmed-healthy.
const DegradedPrefix = "[degraded] "

// Degraded prepends the marker to a human-readable reason.
func Degraded(reason string) string {
	return DegradedPrefix + reason
}
