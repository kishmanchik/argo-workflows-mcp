package internal

import (
	"fmt"
	"regexp"
)

// k8sNameRe matches an RFC1123 DNS label — the shape of every Kubernetes
// namespace and (Argo Workflow) resource name. Anchored, so a value like
// "--all-namespaces" or "-n kube-system" can never pass through to kubectl argv:
// it must not start with '-', which is what closes the flag-injection path.
var k8sNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// ValidateNamespace rejects anything that isn't a bare RFC1123 label before it
// ever reaches a kubectl argv slot.
func ValidateNamespace(ns string) error {
	if !k8sNameRe.MatchString(ns) {
		return fmt.Errorf("invalid namespace %q: must be a bare lowercase RFC1123 label: %w", ns, ErrInvalidInput)
	}
	return nil
}

// ValidateWorkflowName rejects anything that isn't a bare RFC1123 label.
func ValidateWorkflowName(name string) error {
	if !k8sNameRe.MatchString(name) {
		return fmt.Errorf("invalid workflow name %q: must be a bare lowercase RFC1123 label: %w", name, ErrInvalidInput)
	}
	return nil
}

// allowedPhases is the closed set of Argo Workflow phases. An empty string
// means "no filter".
var allowedPhases = map[string]bool{
	"":          true,
	"Pending":   true,
	"Running":   true,
	"Succeeded": true,
	"Failed":    true,
	"Error":     true,
}

// ValidatePhase rejects any phase filter outside the known Argo Workflow phase
// enum, so it can never carry an injected flag either.
func ValidatePhase(phase string) error {
	if !allowedPhases[phase] {
		return fmt.Errorf("invalid phase %q: must be one of Pending/Running/Succeeded/Failed/Error: %w", phase, ErrInvalidInput)
	}
	return nil
}

// ValidateTail bounds a log tail request so a model can't ask for an
// unbounded/huge tail that blows up the response size or runtime.
func ValidateTail(n int) (int, error) {
	if n <= 0 {
		return 50, nil // default
	}
	if n > 500 {
		return 0, fmt.Errorf("tail %d exceeds the maximum of 500 lines: %w", n, ErrInvalidInput)
	}
	return n, nil
}
