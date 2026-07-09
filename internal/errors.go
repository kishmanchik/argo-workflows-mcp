package internal

// Error categorization. A bare Go error string gives the model an unstructured
// blob to guess at; a stable category prefix lets it (and any programmatic
// caller) branch on *kind* of failure without string-sniffing prose.

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrInvalidInput is the sentinel every validate.go rejection wraps, so
// ClassifyError can recognize it through any number of fmt.Errorf("...: %w", ...)
// wrapping layers.
var ErrInvalidInput = errors.New("invalid input")

type ErrorCategory string

const (
	CategoryInvalidInput ErrorCategory = "invalid_input"
	CategoryNotFound     ErrorCategory = "not_found"
	CategoryTimeout      ErrorCategory = "timeout"
	CategoryUnavailable  ErrorCategory = "unavailable"
	CategoryOther        ErrorCategory = "other"
)

// ClassifyError maps a (possibly wrapped) error onto a small stable category
// set: invalid_input (a validate.go rejection), not_found (kubectl reported the
// resource doesn't exist), timeout (the 15s kubectl bound tripped), unavailable
// (kubectl isn't on PATH / couldn't run at all), or other (anything else,
// typically a live cluster/API-server error).
func ClassifyError(err error) ErrorCategory {
	switch {
	case errors.Is(err, ErrInvalidInput):
		return CategoryInvalidInput
	case errors.Is(err, context.DeadlineExceeded):
		return CategoryTimeout
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return CategoryUnavailable // e.g. "kubectl": executable file not found in $PATH
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && looksLikeNotFound(string(exitErr.Stderr)) {
		return CategoryNotFound
	}
	return CategoryOther
}

func looksLikeNotFound(stderr string) bool {
	low := strings.ToLower(stderr)
	return strings.Contains(low, "notfound") || strings.Contains(low, "not found")
}

// FormatToolError renders err as a "[category] message" string — the shape
// every MCP tool handler in this server returns on failure, so a category is
// always present even though MCP's wire format has no separate status field.
func FormatToolError(err error) string {
	return fmt.Sprintf("[%s] %s", ClassifyError(err), err.Error())
}
