package cluster

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Row is one line in a resource list.
//
// Lists carry projections, never whole Kubernetes objects. A raw pod is tens of
// kilobytes of fields no list renders; sending thousands of them would move megabytes
// to draw a table (ADR-006). The full object is fetched only when one is opened.
type Row struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Age       string            `json:"age"`
	CreatedAt string            `json:"createdAt"`
	Fields    map[string]string `json:"fields"`
	// Severity lets a list highlight what is wrong without the client re-deriving it.
	Severity string `json:"severity,omitempty"`
}

const (
	SeverityWarning = "warning"
	SeverityError   = "error"
)

// Link marks a column whose value navigates somewhere.
type Link string

const (
	LinkNone      Link = ""
	LinkNamespace Link = "namespace"
	LinkOwner     Link = "owner"
	LinkNode      Link = "node"
)

// Column describes one column of a list, so the client renders what the server decided
// rather than hard-coding a layout per kind.
type Column struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Mono  bool   `json:"mono,omitempty"`
	// Link turns the value into a navigation target: an owning workload, the node a pod
	// runs on, the namespace it lives in.
	Link Link `json:"link,omitempty"`
	// Status colours the value by what it says, so a failing row reads as failing
	// without hunting for the severity dot.
	Status bool `json:"status,omitempty"`
}

// projector turns an object into a row plus the columns its kind uses.
type projector struct {
	columns []Column
	project func(obj *unstructured.Unstructured) (map[string]string, string)
}

// ColumnsFor reports the columns a kind renders.
func ColumnsFor(kind string) []Column {
	if p, ok := projectors[kind]; ok {
		return p.columns
	}
	return genericProjector.columns
}

// Project turns an object into a list row.
func Project(kind string, obj *unstructured.Unstructured, now time.Time) Row {
	p, ok := projectors[kind]
	if !ok {
		p = genericProjector
	}
	fields, severity := p.project(obj)

	created := obj.GetCreationTimestamp().Time
	return Row{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		Age:       humanAge(now.Sub(created)),
		CreatedAt: created.UTC().Format(time.RFC3339),
		Fields:    fields,
		Severity:  severity,
	}
}

var genericProjector = projector{
	columns: []Column{{Key: "age", Label: "Age", Mono: true}},
	project: func(*unstructured.Unstructured) (map[string]string, string) {
		return map[string]string{}, ""
	},
}

var projectors = map[string]projector{
	"Pod": {
		columns: []Column{
			{Key: "containers", Label: "Containers", Mono: true},
			{Key: "restarts", Label: "Restarts", Mono: true},
			{Key: "controlledBy", Label: "Controlled By", Link: LinkOwner},
			{Key: "node", Label: "Node", Mono: true, Link: LinkNode},
			{Key: "qos", Label: "QoS"},
			{Key: "status", Label: "Status", Status: true},
			{Key: "age", Label: "Age", Mono: true},
		},
		project: projectPod,
	},
	"Deployment": {
		columns: []Column{
			{Key: "ready", Label: "Ready", Mono: true},
			{Key: "uptodate", Label: "Up-to-date", Mono: true},
			{Key: "available", Label: "Available", Mono: true},
			{Key: "age", Label: "Age", Mono: true},
		},
		project: func(obj *unstructured.Unstructured) (map[string]string, string) {
			desired := nestedInt(obj, "spec", "replicas")
			ready := nestedInt(obj, "status", "readyReplicas")

			severity := ""
			if ready < desired {
				severity = SeverityWarning
			}
			return map[string]string{
				"ready":     fmt.Sprintf("%d/%d", ready, desired),
				"uptodate":  fmt.Sprint(nestedInt(obj, "status", "updatedReplicas")),
				"available": fmt.Sprint(nestedInt(obj, "status", "availableReplicas")),
			}, severity
		},
	},
	"StatefulSet": {
		columns: []Column{
			{Key: "ready", Label: "Ready", Mono: true},
			{Key: "age", Label: "Age", Mono: true},
		},
		project: func(obj *unstructured.Unstructured) (map[string]string, string) {
			desired := nestedInt(obj, "spec", "replicas")
			ready := nestedInt(obj, "status", "readyReplicas")

			severity := ""
			if ready < desired {
				severity = SeverityWarning
			}
			return map[string]string{"ready": fmt.Sprintf("%d/%d", ready, desired)}, severity
		},
	},
	"DaemonSet": {
		columns: []Column{
			{Key: "desired", Label: "Desired", Mono: true},
			{Key: "ready", Label: "Ready", Mono: true},
			{Key: "available", Label: "Available", Mono: true},
			{Key: "age", Label: "Age", Mono: true},
		},
		project: func(obj *unstructured.Unstructured) (map[string]string, string) {
			desired := nestedInt(obj, "status", "desiredNumberScheduled")
			ready := nestedInt(obj, "status", "numberReady")

			severity := ""
			if ready < desired {
				severity = SeverityWarning
			}
			return map[string]string{
				"desired":   fmt.Sprint(desired),
				"ready":     fmt.Sprint(ready),
				"available": fmt.Sprint(nestedInt(obj, "status", "numberAvailable")),
			}, severity
		},
	},
	"ReplicaSet": {
		columns: []Column{
			{Key: "desired", Label: "Desired", Mono: true},
			{Key: "ready", Label: "Ready", Mono: true},
			{Key: "controlledBy", Label: "Controlled By", Link: LinkOwner},
			{Key: "age", Label: "Age", Mono: true},
		},
		project: func(obj *unstructured.Unstructured) (map[string]string, string) {
			owner, ownerKind := ownerOf(obj)
			return map[string]string{
				"desired":          fmt.Sprint(nestedInt(obj, "spec", "replicas")),
				"ready":            fmt.Sprint(nestedInt(obj, "status", "readyReplicas")),
				"controlledBy":     owner,
				"controlledByKind": ownerKind,
			}, ""
		},
	},
	"Job": {
		columns: []Column{
			{Key: "completions", Label: "Completions", Mono: true},
			{Key: "age", Label: "Age", Mono: true},
		},
		project: func(obj *unstructured.Unstructured) (map[string]string, string) {
			succeeded := nestedInt(obj, "status", "succeeded")
			wanted := nestedInt(obj, "spec", "completions")

			severity := ""
			if nestedInt(obj, "status", "failed") > 0 {
				severity = SeverityError
			}
			return map[string]string{"completions": fmt.Sprintf("%d/%d", succeeded, wanted)}, severity
		},
	},
	"CronJob": {
		columns: []Column{
			{Key: "schedule", Label: "Schedule", Mono: true},
			{Key: "suspend", Label: "Suspend"},
			{Key: "active", Label: "Active", Mono: true},
			{Key: "age", Label: "Age", Mono: true},
		},
		project: func(obj *unstructured.Unstructured) (map[string]string, string) {
			suspended, _, _ := unstructured.NestedBool(obj.Object, "spec", "suspend")
			active, _, _ := unstructured.NestedSlice(obj.Object, "status", "active")

			return map[string]string{
				"schedule": nestedString(obj, "spec", "schedule"),
				"suspend":  fmt.Sprint(suspended),
				"active":   fmt.Sprint(len(active)),
			}, ""
		},
	},
	"Service": {
		columns: []Column{
			{Key: "type", Label: "Type"},
			{Key: "clusterIP", Label: "Cluster IP", Mono: true},
			{Key: "ports", Label: "Ports", Mono: true},
			{Key: "age", Label: "Age", Mono: true},
		},
		project: func(obj *unstructured.Unstructured) (map[string]string, string) {
			ports, _, _ := unstructured.NestedSlice(obj.Object, "spec", "ports")
			labels := make([]string, 0, len(ports))
			for _, raw := range ports {
				port, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				number, _, _ := unstructured.NestedInt64(port, "port")
				protocol, _, _ := unstructured.NestedString(port, "protocol")
				labels = append(labels, fmt.Sprintf("%d/%s", number, orDefault(protocol, "TCP")))
			}
			return map[string]string{
				"type":      nestedString(obj, "spec", "type"),
				"clusterIP": nestedString(obj, "spec", "clusterIP"),
				"ports":     strings.Join(labels, ","),
			}, ""
		},
	},
	"Ingress": {
		columns: []Column{
			{Key: "class", Label: "Class"},
			{Key: "hosts", Label: "Hosts", Mono: true},
			{Key: "age", Label: "Age", Mono: true},
		},
		project: func(obj *unstructured.Unstructured) (map[string]string, string) {
			rules, _, _ := unstructured.NestedSlice(obj.Object, "spec", "rules")
			hosts := make([]string, 0, len(rules))
			for _, raw := range rules {
				rule, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if host, _, _ := unstructured.NestedString(rule, "host"); host != "" {
					hosts = append(hosts, host)
				}
			}
			return map[string]string{
				"class": nestedString(obj, "spec", "ingressClassName"),
				"hosts": strings.Join(hosts, ","),
			}, ""
		},
	},
	"PersistentVolumeClaim": {
		columns: []Column{
			{Key: "status", Label: "Status", Status: true},
			{Key: "capacity", Label: "Capacity", Mono: true},
			{Key: "storageClass", Label: "Class", Mono: true},
			{Key: "age", Label: "Age", Mono: true},
		},
		project: func(obj *unstructured.Unstructured) (map[string]string, string) {
			phase := nestedString(obj, "status", "phase")

			severity := ""
			if phase != "Bound" {
				severity = SeverityWarning
			}
			return map[string]string{
				"status":       phase,
				"capacity":     nestedString(obj, "status", "capacity", "storage"),
				"storageClass": nestedString(obj, "spec", "storageClassName"),
			}, severity
		},
	},
	"Node": {
		columns: []Column{
			{Key: "status", Label: "Status", Status: true},
			{Key: "roles", Label: "Roles"},
			{Key: "version", Label: "Version", Mono: true},
			{Key: "age", Label: "Age", Mono: true},
		},
		project: projectNode,
	},
	"Namespace": {
		columns: []Column{
			{Key: "status", Label: "Status", Status: true},
			{Key: "age", Label: "Age", Mono: true},
		},
		project: func(obj *unstructured.Unstructured) (map[string]string, string) {
			return map[string]string{"status": nestedString(obj, "status", "phase")}, ""
		},
	},
	"Secret": {
		columns: []Column{
			{Key: "type", Label: "Type", Mono: true},
			{Key: "keys", Label: "Keys", Mono: true},
			{Key: "age", Label: "Age", Mono: true},
		},
		// Only key names and their count. Secret values never appear in a list, and the
		// detail view has to ask for them explicitly.
		project: func(obj *unstructured.Unstructured) (map[string]string, string) {
			data, _, _ := unstructured.NestedMap(obj.Object, "data")
			names := make([]string, 0, len(data))
			for key := range data {
				names = append(names, key)
			}
			sort.Strings(names)

			return map[string]string{
				"type": nestedString(obj, "type"),
				"keys": strings.Join(names, ", "),
			}, ""
		},
	},
	"ConfigMap": {
		columns: []Column{
			{Key: "keys", Label: "Keys", Mono: true},
			{Key: "age", Label: "Age", Mono: true},
		},
		project: func(obj *unstructured.Unstructured) (map[string]string, string) {
			data, _, _ := unstructured.NestedMap(obj.Object, "data")
			names := make([]string, 0, len(data))
			for key := range data {
				names = append(names, key)
			}
			sort.Strings(names)
			return map[string]string{"keys": strings.Join(names, ", ")}, ""
		},
	},
}

func projectPod(obj *unstructured.Unstructured) (map[string]string, string) {
	statuses, _, _ := unstructured.NestedSlice(obj.Object, "status", "containerStatuses")

	ready, restarts := 0, int64(0)
	for _, raw := range statuses {
		status, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if isReady, _, _ := unstructured.NestedBool(status, "ready"); isReady {
			ready++
		}
		count, _, _ := unstructured.NestedInt64(status, "restartCount")
		restarts += count
	}

	containers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "containers")
	phase := nestedString(obj, "status", "phase")

	severity := ""
	switch {
	case phase == "Failed":
		severity = SeverityError
	case phase == "Pending", restarts > 0, ready < len(containers):
		severity = SeverityWarning
	}

	owner, ownerKind := ownerOf(obj)

	return map[string]string{
		"containers":       fmt.Sprintf("%d/%d", ready, len(containers)),
		"ready":            fmt.Sprintf("%d/%d", ready, len(containers)),
		"status":           phase,
		"restarts":         fmt.Sprint(restarts),
		"node":             nestedString(obj, "spec", "nodeName"),
		"qos":              nestedString(obj, "status", "qosClass"),
		"controlledBy":     owner,
		"controlledByKind": ownerKind,
	}, severity
}

// ownerOf reports the workload a resource is controlled by, which is the first question
// asked of a misbehaving pod: what created it, and is that the thing to look at.
func ownerOf(obj *unstructured.Unstructured) (name, kind string) {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Controller != nil && *ref.Controller {
			return ref.Name, ref.Kind
		}
	}
	if refs := obj.GetOwnerReferences(); len(refs) > 0 {
		return refs[0].Name, refs[0].Kind
	}
	return "", ""
}

func projectNode(obj *unstructured.Unstructured) (map[string]string, string) {
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")

	status, severity := "Unknown", SeverityWarning
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		conditionType, _, _ := unstructured.NestedString(condition, "type")
		if conditionType != "Ready" {
			continue
		}
		if value, _, _ := unstructured.NestedString(condition, "status"); value == "True" {
			status, severity = "Ready", ""
		} else {
			status, severity = "NotReady", SeverityError
		}
	}

	if unschedulable, _, _ := unstructured.NestedBool(obj.Object, "spec", "unschedulable"); unschedulable {
		status += ",SchedulingDisabled"
		if severity == "" {
			severity = SeverityWarning
		}
	}

	var roles []string
	for label := range obj.GetLabels() {
		if role, found := strings.CutPrefix(label, "node-role.kubernetes.io/"); found && role != "" {
			roles = append(roles, role)
		}
	}
	sort.Strings(roles)
	if len(roles) == 0 {
		roles = []string{"<none>"}
	}

	return map[string]string{
		"status":  status,
		"roles":   strings.Join(roles, ","),
		"version": nestedString(obj, "status", "nodeInfo", "kubeletVersion"),
	}, severity
}

func nestedString(obj *unstructured.Unstructured, path ...string) string {
	value, _, _ := unstructured.NestedString(obj.Object, path...)
	return value
}

func nestedInt(obj *unstructured.Unstructured, path ...string) int64 {
	value, _, _ := unstructured.NestedInt64(obj.Object, path...)
	return value
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// humanAge renders a duration the way kubectl renders its AGE column.
func humanAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	default:
		return fmt.Sprintf("%dy", int(d.Hours())/(24*365))
	}
}
