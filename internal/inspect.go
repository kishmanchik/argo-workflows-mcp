package internal

// Read-only inspection of Argo Workflow resources. By construction these only
// ever `kubectl get` / `kubectl logs` the argoproj.io/v1alpha1 Workflow CRD and
// its pods — there is no mutate/delete path here, mirroring hxdrenv-operator's
// pkg/health/inspect.go. Every returned string is redacted before it reaches
// the caller.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// workflowNode is the subset of a Workflow's status.nodes entry we care about
// for diagnosis — one step in the DAG/steps template.
type workflowNode struct {
	DisplayName  string `json:"displayName"`
	TemplateName string `json:"templateName"`
	Phase        string `json:"phase"`
	Message      string `json:"message"`
	Type         string `json:"type"`
}

// condition is the subset of a Workflow's status.conditions entry we care
// about — e.g. a SpecWarning/SpecError can be true on a workflow whose phase
// otherwise looks healthy (Running/Succeeded).
type condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// problemConditionTypes are conditions worth surfacing even when phase alone
// looks healthy — every one of these means something is actually wrong.
var problemConditionTypes = map[string]bool{
	"SpecWarning":     true,
	"SpecError":       true,
	"MetricsError":    true,
	"ArtifactGCError": true,
}

// firstProblemCondition returns the first true problem condition, or ok=false
// if none. Name-sorted-equivalent: conditions is already a small ordered
// slice from the API, so first-true is deterministic without extra sorting.
func firstProblemCondition(conds []condition) (c condition, ok bool) {
	for _, cond := range conds {
		if problemConditionTypes[cond.Type] && cond.Status == "True" {
			return cond, true
		}
	}
	return condition{}, false
}

// workflowItem is the subset of a Workflow resource we parse out of
// `kubectl get workflows.argoproj.io -o json`. The real CRD carries far more
// (spec.templates, full node metadata, artifacts, ...) — we only pull what a
// diagnosis needs, deliberately narrow.
type workflowItem struct {
	Metadata struct {
		Name              string `json:"name"`
		Namespace         string `json:"namespace"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	Status struct {
		Phase                    string                  `json:"phase"`
		Message                  string                  `json:"message"`
		Progress                 string                  `json:"progress"`
		StartedAt                string                  `json:"startedAt"`
		FinishedAt               string                  `json:"finishedAt"`
		Nodes                    map[string]workflowNode `json:"nodes"`
		CompressedNodes          string                  `json:"compressedNodes"`
		OffloadNodeStatusVersion string                  `json:"offloadNodeStatusVersion"`
		Conditions               []condition             `json:"conditions"`
	} `json:"status"`
}

type workflowList struct {
	Items []workflowItem `json:"items"`
}

func getWorkflows(namespace string, extraArgs ...string) ([]workflowItem, error) {
	args := append([]string{"get", "workflows.argoproj.io", "-n", namespace, "-o", "json"}, extraArgs...)
	out, err := KubectlOutput(args...)
	if err != nil {
		return nil, fmt.Errorf("kubectl get workflows: %w", err)
	}
	var list workflowList
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("parsing workflow list: %w", err)
	}
	return list.Items, nil
}

func getWorkflow(namespace, name string) (*workflowItem, error) {
	out, err := KubectlOutput("get", "workflows.argoproj.io", name, "-n", namespace, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("kubectl get workflow %s: %w", name, err)
	}
	var item workflowItem
	if err := json.Unmarshal(out, &item); err != nil {
		return nil, fmt.Errorf("parsing workflow: %w", err)
	}
	return &item, nil
}

// ListWorkflows renders a one-line-per-workflow summary for a namespace,
// optionally filtered to one phase.
func ListWorkflows(namespace, phase string) (string, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return "", err
	}
	if err := ValidatePhase(phase); err != nil {
		return "", err
	}
	items, err := getWorkflows(namespace)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "no workflows found in namespace " + namespace, nil
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Metadata.CreationTimestamp > items[j].Metadata.CreationTimestamp
	})
	var sb strings.Builder
	count := 0
	for _, it := range items {
		if phase != "" && it.Status.Phase != phase {
			continue
		}
		count++
		msg := it.Status.Message
		if msg != "" {
			msg = " — " + msg
		}
		fmt.Fprintf(&sb, "%s\tphase=%s\tprogress=%s\tstarted=%s%s\n",
			it.Metadata.Name, orUnknown(it.Status.Phase), orUnknown(it.Status.Progress),
			orUnknown(it.Status.StartedAt), msg)
	}
	if count == 0 {
		return fmt.Sprintf("no workflows with phase=%s in namespace %s", phase, namespace), nil
	}
	return Redact(sb.String()), nil
}

// GetWorkflow renders full detail for one workflow: overall phase/progress plus
// a per-node breakdown, so a failed step is visible without a separate call.
func GetWorkflow(namespace, name string) (string, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return "", err
	}
	if err := ValidateWorkflowName(name); err != nil {
		return "", err
	}
	item, err := getWorkflow(namespace, name)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "workflow=%s namespace=%s phase=%s progress=%s started=%s finished=%s\n",
		item.Metadata.Name, namespace, orUnknown(item.Status.Phase), orUnknown(item.Status.Progress),
		orUnknown(item.Status.StartedAt), orUnknown(item.Status.FinishedAt))
	if item.Status.Message != "" {
		fmt.Fprintf(&sb, "message: %s\n", item.Status.Message)
	}
	if cond, ok := firstProblemCondition(item.Status.Conditions); ok {
		fmt.Fprintf(&sb, "condition %s: %s\n", cond.Type, cond.Message)
	}
	nodes, degraded := resolveNodes(item)
	if degraded != "" {
		sb.WriteString(Degraded(degraded) + "\n")
		return Redact(sb.String()), nil
	}
	if len(nodes) == 0 {
		sb.WriteString("(no node status yet)\n")
		return Redact(sb.String()), nil
	}
	sb.WriteString("nodes:\n")
	nodeNames := make([]string, 0, len(nodes))
	for id := range nodes {
		nodeNames = append(nodeNames, id)
	}
	sort.Strings(nodeNames)
	for _, id := range nodeNames {
		n := nodes[id]
		line := fmt.Sprintf("  [%s] %s (template=%s) phase=%s", n.Type, orUnknown(n.DisplayName), orUnknown(n.TemplateName), orUnknown(n.Phase))
		if n.Message != "" {
			line += " — " + n.Message
		}
		sb.WriteString(line + "\n")
	}
	return Redact(sb.String()), nil
}

// WorkflowLogs tails the redacted logs of every pod belonging to one workflow,
// selected by the `workflows.argoproj.io/workflow` label Argo sets on every
// pod it creates — never a model-supplied pod name.
func WorkflowLogs(namespace, name string, tail int) (string, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return "", err
	}
	if err := ValidateWorkflowName(name); err != nil {
		return "", err
	}
	tail, err := ValidateTail(tail)
	if err != nil {
		return "", err
	}
	out, err := KubectlOutput("logs",
		"-n", namespace,
		"-l", "workflows.argoproj.io/workflow="+name,
		"--all-containers=true", "--prefix",
		"--tail="+fmt.Sprint(tail))
	if err != nil {
		return "", fmt.Errorf("kubectl logs for workflow %s: %w", name, err)
	}
	if len(out) == 0 {
		// Not a confirmed "no logs ever" — kubectl succeeded, but empty could
		// equally mean the pods (and their logs) are already garbage-collected,
		// or the label selector matched nothing due to a naming mismatch.
		// Degraded, not a clean success, so a caller doesn't read this as
		// confirmed-quiet.
		return Degraded(fmt.Sprintf("no logs found for workflow %s in namespace %s (pods may already be garbage-collected)", name, namespace)), nil
	}
	return Redact(string(out)), nil
}

// Diagnose is a one-shot, read-only overview: every non-Succeeded workflow,
// PLUS any Succeeded/Running one flagged by a problem condition
// (SpecWarning/SpecError/MetricsError/ArtifactGCError — phase alone can look
// healthy while one of these is true), plus for each Failed/Error workflow
// the first failed/errored node — the fastest "what broke and where" starting
// point. Deterministic (no LLM); the caller reasons over it.
func Diagnose(namespace string) (string, error) {
	if err := ValidateNamespace(namespace); err != nil {
		return "", err
	}
	items, err := getWorkflows(namespace)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	unhealthy := 0
	for _, it := range items {
		cond, hasProblem := firstProblemCondition(it.Status.Conditions)
		phaseHealthy := it.Status.Phase == "Succeeded" || it.Status.Phase == ""
		if phaseHealthy && !hasProblem {
			continue
		}
		unhealthy++
		fmt.Fprintf(&sb, "== %s == phase=%s", it.Metadata.Name, orUnknown(it.Status.Phase))
		if it.Status.Message != "" {
			fmt.Fprintf(&sb, " — %s", it.Status.Message)
		}
		sb.WriteString("\n")
		if hasProblem {
			fmt.Fprintf(&sb, "  condition %s: %s\n", cond.Type, cond.Message)
		}
		if it.Status.Phase == "Failed" || it.Status.Phase == "Error" {
			nodes, degraded := resolveNodes(&it)
			switch {
			case degraded != "":
				fmt.Fprintf(&sb, "  %s\n", Degraded(degraded))
			case len(nodes) == 0:
				fmt.Fprintf(&sb, "  %s\n", Degraded("workflow reports "+it.Status.Phase+" but has no node status at all"))
			default:
				if node := firstFailedNode(nodes); node != nil {
					fmt.Fprintf(&sb, "  first failed step: %s (template=%s) — %s\n",
						orUnknown(node.DisplayName), orUnknown(node.TemplateName), node.Message)
				} else {
					// The workflow itself reports Failed/Error, but nothing in
					// the resolved nodes corroborates it. Don't imply we
					// identified a culprit step when we didn't.
					fmt.Fprintf(&sb, "  %s\n", Degraded("workflow reports "+it.Status.Phase+" but no matching Failed/Error node was found in status.nodes"))
				}
			}
		}
	}
	if unhealthy == 0 {
		return fmt.Sprintf("no Pending/Running/Failed/Error workflows or problem conditions in namespace %s", namespace), nil
	}
	return Redact(sb.String()), nil
}

// firstFailedNode returns a deterministic (name-sorted) first Failed/Error node,
// so the same workflow always reports the same "first" culprit across calls.
func firstFailedNode(nodes map[string]workflowNode) *workflowNode {
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := nodes[id]
		if n.Phase == "Failed" || n.Phase == "Error" {
			return &n
		}
	}
	return nil
}

func orUnknown(s string) string {
	if s == "" {
		return "<unknown>"
	}
	return s
}
