package internal

// Node-status resolution. Argo Workflows doesn't always keep status.nodes
// populated: for a large workflow it gets gzip+base64-packed into
// status.compressedNodes (mirrors the real controller's own
// workflow/packer.DecompressWorkflow — base64.StdEncoding then gzip), and if
// node-status offload to an external DB is enabled, status.offloadNodeStatusVersion
// is set and NEITHER field carries the data at all — that's not decodable by a
// kubectl-only tool, so it must be reported explicitly rather than read as
// "hasn't started yet".

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

// resolveNodes returns the workflow's node map. The second return value is a
// non-empty degraded reason when nodes genuinely can't be determined — an
// empty map AND an empty reason means "no nodes yet" (e.g. still Pending),
// which is not degraded.
func resolveNodes(item *workflowItem) (map[string]workflowNode, string) {
	if len(item.Status.Nodes) > 0 {
		return item.Status.Nodes, ""
	}
	if item.Status.OffloadNodeStatusVersion != "" {
		return nil, "node status is offloaded to an external database (status.offloadNodeStatusVersion is set) — not visible via kubectl"
	}
	if item.Status.CompressedNodes == "" {
		return nil, ""
	}
	nodes, err := decodeCompressedNodes(item.Status.CompressedNodes)
	if err != nil {
		return nil, "could not decompress status.compressedNodes: " + err.Error()
	}
	return nodes, ""
}

// decodeCompressedNodes reverses the encoding Argo's controller applies when
// status.nodes grows too large. Only gzip (the default/legacy algorithm) is
// supported — a workflow explicitly configured for zstd/brotli compression
// (WORKFLOW_COMPRESSION_ALGORITHM) surfaces as a clear decode error rather
// than pulling in extra non-stdlib dependencies for a rare case.
func decodeCompressedNodes(compressed string) (map[string]workflowNode, error) {
	raw, err := base64.StdEncoding.DecodeString(compressed)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		return nil, fmt.Errorf("not gzip-compressed (unsupported compression algorithm)")
	}
	gr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()
	data, err := io.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("reading gzip stream: %w", err)
	}
	var nodes map[string]workflowNode
	if err := json.Unmarshal(data, &nodes); err != nil {
		return nil, fmt.Errorf("parsing decompressed nodes: %w", err)
	}
	return nodes, nil
}
