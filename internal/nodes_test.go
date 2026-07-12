package internal

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// gzipCompressNodes mirrors what Argo's own controller does when packing
// status.nodes into status.compressedNodes — the exact reverse of
// decodeCompressedNodes, used here only to build test fixtures.
func gzipCompressNodes(t *testing.T, nodes map[string]workflowNode) string {
	data, err := json.Marshal(nodes)
	if err != nil {
		t.Fatalf("marshal fixture nodes: %v", err)
	}
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(data); err != nil {
		t.Fatalf("gzip fixture nodes: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestResolveNodes_PrefersUncompressedWhenPresent(t *testing.T) {
	item := &workflowItem{}
	item.Status.Nodes = map[string]workflowNode{"n1": {DisplayName: "step-one", Phase: "Succeeded"}}
	item.Status.CompressedNodes = "should-be-ignored"

	nodes, degraded := resolveNodes(item)
	if degraded != "" {
		t.Fatalf("unexpected degraded reason: %q", degraded)
	}
	if len(nodes) != 1 || nodes["n1"].DisplayName != "step-one" {
		t.Errorf("expected the uncompressed node map, got: %+v", nodes)
	}
}

func TestResolveNodes_DecompressesCompressedNodes(t *testing.T) {
	want := map[string]workflowNode{
		"n1": {DisplayName: "generate-pipeline-config", TemplateName: "generate-pipeline-config", Phase: "Succeeded"},
		"n2": {DisplayName: "processing", TemplateName: "processing", Phase: "Failed", Message: "boom"},
	}
	item := &workflowItem{}
	item.Status.CompressedNodes = gzipCompressNodes(t, want)

	nodes, degraded := resolveNodes(item)
	if degraded != "" {
		t.Fatalf("unexpected degraded reason: %q", degraded)
	}
	if len(nodes) != 2 || nodes["n2"].Message != "boom" {
		t.Errorf("expected the decompressed node map to round-trip, got: %+v", nodes)
	}
}

func TestResolveNodes_OffloadedIsDegraded(t *testing.T) {
	item := &workflowItem{}
	item.Status.OffloadNodeStatusVersion = "1"

	nodes, degraded := resolveNodes(item)
	if nodes != nil {
		t.Errorf("expected no nodes for an offloaded workflow, got: %+v", nodes)
	}
	if !strings.Contains(degraded, "offloaded to an external database") {
		t.Errorf("expected an offload-specific degraded reason, got: %q", degraded)
	}
}

func TestResolveNodes_UnsupportedCompressionIsDegradedNotAPanic(t *testing.T) {
	item := &workflowItem{}
	// Valid base64, but not gzip magic bytes — simulates a zstd/brotli-compressed
	// workflow (WORKFLOW_COMPRESSION_ALGORITHM override), which this tool
	// deliberately doesn't support.
	item.Status.CompressedNodes = base64.StdEncoding.EncodeToString([]byte("not-gzip-data"))

	nodes, degraded := resolveNodes(item)
	if nodes != nil {
		t.Errorf("expected no nodes when decompression fails, got: %+v", nodes)
	}
	if !strings.Contains(degraded, "could not decompress") {
		t.Errorf("expected a decompress-failure degraded reason, got: %q", degraded)
	}
}

func TestResolveNodes_TrulyNoNodesYetIsNotDegraded(t *testing.T) {
	item := &workflowItem{} // Pending: no Nodes, no CompressedNodes, no offload

	nodes, degraded := resolveNodes(item)
	if nodes != nil {
		t.Errorf("expected nil nodes, got: %+v", nodes)
	}
	if degraded != "" {
		t.Errorf("expected NOT degraded for a workflow that simply hasn't started, got: %q", degraded)
	}
}
