package health

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var nodesGVR = schema.GroupVersionResource{Version: "v1", Resource: "nodes"}

// pressures are node conditions that are a problem when true, unlike Ready which is a
// problem when false.
var pressures = map[string]string{
	"MemoryPressure": "The node is low on memory and will start evicting pods.",
	"DiskPressure":   "The node is low on disk and will start evicting pods and garbage-collecting images.",
	"PIDPressure":    "The node is running out of process IDs.",
	"NetworkUnavailable": "The node's network is not correctly configured, so its pods " +
		"cannot reach the rest of the cluster.",
}

// NodeDetector finds nodes that cannot carry their share of the cluster.
type NodeDetector struct{}

func (d *NodeDetector) Name() string { return "node" }

func (d *NodeDetector) Detect(ctx context.Context, r Reader) ([]Finding, error) {
	nodes, err := r.List(ctx, nodesGVR, "")
	if err != nil {
		return nil, err
	}

	var findings []Finding
	for i := range nodes {
		findings = append(findings, d.inspect(&nodes[i])...)
	}
	return findings, nil
}

func (d *NodeDetector) inspect(node *unstructured.Unstructured) []Finding {
	base := Finding{Kind: "Node", Name: node.GetName(), Category: CategoryNode, TypeKey: "nodes"}
	var findings []Finding

	for _, condition := range conditions(node) {
		conditionType, _ := condition["type"].(string)
		status, _ := condition["status"].(string)
		message, _ := condition["message"].(string)
		since, _ := condition["lastTransitionTime"].(string)

		switch {
		case conditionType == "Ready" && status != "True":
			finding := base
			finding.Severity = SeverityCritical
			finding.Reason = "NotReady"
			// An unknown Ready status means the kubelet stopped reporting, which is a
			// different failure from a kubelet that reports it is unhealthy.
			if status == "Unknown" {
				finding.Detail = orDefault(message, "The kubelet has stopped reporting; the node may be unreachable.")
			} else {
				finding.Detail = orDefault(message, "The node reports it is not ready.")
			}
			finding.LastSeen = since
			findings = append(findings, finding)

		case status == "True":
			detail, isPressure := pressures[conditionType]
			if !isPressure {
				continue
			}
			finding := base
			finding.Severity = SeverityWarning
			finding.Reason = conditionType
			finding.Detail = orDefault(message, detail)
			finding.LastSeen = since
			findings = append(findings, finding)
		}
	}

	// Cordoning is deliberate, so it is information rather than a fault — but a node left
	// cordoned after a maintenance window is a common and quiet cause of Pending pods.
	if unschedulable, _, _ := unstructured.NestedBool(node.Object, "spec", "unschedulable"); unschedulable {
		finding := base
		finding.Severity = SeverityInfo
		finding.Reason = "Cordoned"
		finding.Detail = "The node is cordoned, so the scheduler will not place new pods on it."
		findings = append(findings, finding)
	}
	return findings
}

// KubeletSkew reports nodes whose kubelet trails the control plane by more than the two
// minor versions Kubernetes supports.
func KubeletSkew(nodes []unstructured.Unstructured, serverMinor int) []Finding {
	var findings []Finding

	for i := range nodes {
		version := nested(&nodes[i], "status", "nodeInfo", "kubeletVersion")
		minor, ok := minorOf(version)
		if !ok || serverMinor-minor <= 2 {
			continue
		}
		findings = append(findings, Finding{
			Category: CategoryNode,
			Severity: SeverityWarning,
			Kind:     "Node",
			Name:     nodes[i].GetName(),
			Reason:   "VersionSkew",
			TypeKey:  "nodes",
			Detail: fmt.Sprintf("kubelet %s trails the control plane by %d minor versions; "+
				"Kubernetes supports at most two.", version, serverMinor-minor),
		})
	}
	return findings
}

func minorOf(version string) (int, bool) {
	var major, minor int
	if _, err := fmt.Sscanf(version, "v%d.%d", &major, &minor); err != nil {
		return 0, false
	}
	return minor, true
}
