package internal

import (
	"context"
	"os/exec"
	"time"
)

// KubectlTimeout bounds every kubectl call so a stuck or unreachable API server
// can never hang the server. Learned from hxdrenv-operator's pkg/health/kubectl.go.
const KubectlTimeout = 15 * time.Second

// KubectlExec is the actual runner — a package var so tests can substitute canned
// output instead of hitting a live cluster.
var KubectlExec = func(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), KubectlTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "kubectl", args...).Output()
}

// KubectlOutput runs `kubectl <args...>` bounded by KubectlTimeout.
func KubectlOutput(args ...string) ([]byte, error) {
	return KubectlExec(args...)
}
