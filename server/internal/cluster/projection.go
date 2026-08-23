package cluster

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/erolbeyaz/kubby/internal/k8s"
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
	// hideName drops the name column for kinds whose name says nothing.
	hideName bool
	project  func(obj *unstructured.Unstructured) (map[string]string, string)
}

// ColumnsFor reports the columns a kind renders.
func ColumnsFor(kind string) []Column {
	if p, ok := projectors[kind]; ok {
		return p.columns
	}
	return genericProjector.columns
}

// HidesName reports whether a kind's own name is worth a column. An Event's name is a
// generated suffix; what it is about is the object it names inside itself.
func HidesName(kind string) bool {
	p, ok := projectors[kind]
	return ok && p.hideName
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

			fields := map[string]string{
				"ready":     fmt.Sprintf("%d/%d", ready, desired),
				"uptodate":  fmt.Sprint(nestedInt(obj, "status", "updatedReplicas")),
				"available": fmt.Sprint(nestedInt(obj, "status", "availableReplicas")),
			}
			return withTrouble(fields, k8s.WorkloadTrouble(obj, desired, ready))
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

			fields := map[string]string{"ready": fmt.Sprintf("%d/%d", ready, desired)}
			return withTrouble(fields, k8s.WorkloadTrouble(obj, desired, ready))
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

			fields := map[string]string{
				"desired":   fmt.Sprint(desired),
				"ready":     fmt.Sprint(ready),
				"available": fmt.Sprint(nestedInt(obj, "status", "numberAvailable")),
			}
			return withTrouble(fields, k8s.WorkloadTrouble(obj, desired, ready))
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
			{Key: "taints", Label: "Taints", Mono: true},
			{Key: "roles", Label: "Roles"},
			{Key: "version", Label: "Version", Mono: true},
			{Key: "age", Label: "Age", Mono: true},
			{Key: "status", Label: "Conditions", Status: true},
		},
		project: projectNode,
	},
	"Event": {
		// An event's own name is a generated suffix nobody reads; what it is about is
		// the involved object, and what it says is the message.
		hideName: true,
		columns: []Column{
			{Key: "type", Label: "Type", Status: true},
			{Key: "message", Label: "Message"},
			{Key: "involvedObject", Label: "Involved Object", Link: LinkOwner},
			{Key: "source", Label: "Source", Mono: true},
			{Key: "count", Label: "Count", Mono: true},
			{Key: "age", Label: "Age", Mono: true},
			{Key: "lastSeen", Label: "Last Seen", Mono: true},
		},
		project: projectEvent,
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
	// Not the phase: a crash-looping pod's phase is "Running", which is true of the pod
	// and useless to the reader (see k8s.PodStatus).
	phase := k8s.PodStatus(obj)

	// The same reading the health panel does (internal/k8s), so a row and the panel can
	// never disagree about the same pod. "Pending" is the question; this is the answer.
	trouble := k8s.PodTrouble(obj)

	severity := ""
	if trouble != nil {
		severity = trouble.Severity
	} else if restarts > 0 {
		severity = SeverityWarning
	}

	owner, ownerKind := ownerOf(obj)

	fields := map[string]string{
		"containers":       fmt.Sprintf("%d/%d", ready, len(containers)),
		"ready":            fmt.Sprintf("%d/%d", ready, len(containers)),
		"status":           phase,
		"restarts":         fmt.Sprint(restarts),
		"node":             nestedString(obj, "spec", "nodeName"),
		"controlledBy":     owner,
		"controlledByKind": ownerKind,
	}
	if trouble != nil {
		fields["reason"] = trouble.Reason
		fields["trouble"] = trouble.Detail
		if trouble.Container != "" {
			fields["troubleContainer"] = trouble.Container
		}
	}
	return fields, severity
}

// withTrouble folds a reading of what is wrong into a row's fields.
//
// The mark in the list and the sentence behind it come from one place, so a workload's
// own page says what its pods' page says rather than leaving the reader to go and look.
func withTrouble(fields map[string]string, trouble *k8s.Trouble) (map[string]string, string) {
	if trouble == nil {
		return fields, ""
	}

	fields["reason"] = trouble.Reason
	fields["trouble"] = trouble.Detail
	if trouble.Container != "" {
		fields["troubleContainer"] = trouble.Container
	}
	if trouble.Severity == k8s.SeverityWarning {
		return fields, SeverityWarning
	}
	return fields, SeverityError
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

	taints, _, _ := unstructured.NestedSlice(obj.Object, "spec", "taints")

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
		// The count rather than the taints themselves: a node with six taints has a
		// reason nobody reads in a table cell, and the detail panel has room for it.
		"taints": strconv.Itoa(len(taints)),
		// Disk is capacity, not usage: metrics-server reports CPU and memory only, and
		// an empty column is worse than an honest one.
		"disk": humanBytes(nestedString(obj, "status", "capacity", "ephemeral-storage")),
	}, severity
}

// humanBytes renders a Kubernetes quantity as something readable, or leaves it alone
// when it is not a plain byte count.
func humanBytes(quantity string) string {
	if quantity == "" {
		return ""
	}

	parsed, err := resource.ParseQuantity(quantity)
	if err != nil {
		return quantity
	}

	bytes := parsed.Value()
	const unit = 1024
	if bytes < unit {
		return strconv.FormatInt(bytes, 10) + "B"
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(bytes)/float64(div), 'f', 1, 64) + string("KMGTP"[exp]) + "i"
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

// projectEvent reads what an event is about, not what it is called.
func projectEvent(obj *unstructured.Unstructured) (map[string]string, string) {
	eventType := nestedString(obj, "type")

	severity := ""
	if eventType == "Warning" {
		severity = SeverityWarning
	}

	kind := nestedString(obj, "involvedObject", "kind")
	name := nestedString(obj, "involvedObject", "name")
	involved := name
	if kind != "" && name != "" {
		involved = kind + ": " + name
	}

	count, _, _ := unstructured.NestedInt64(obj.Object, "count")
	if count == 0 {
		count = 1
	}

	last := eventTime(obj)
	lastSeen := ""
	if !last.IsZero() {
		lastSeen = last.UTC().Format(time.RFC3339)
	}

	return map[string]string{
		"type":               eventType,
		"message":            nestedString(obj, "message"),
		"involvedObject":     involved,
		"involvedObjectKind": kind,
		"involvedObjectName": name,
		"source":             eventSource(obj),
		"count":              strconv.FormatInt(count, 10),
		"lastSeen":           lastSeen,
		"reason":             nestedString(obj, "reason"),
	}, severity
}

// eventSource is the component that reported it, with the node when there is one, which
// is how "kubelet on which machine" gets answered without opening the event.
func eventSource(obj *unstructured.Unstructured) string {
	component := nestedString(obj, "source", "component")
	if component == "" {
		component = nestedString(obj, "reportingComponent")
	}
	host := nestedString(obj, "source", "host")
	if host == "" {
		host = nestedString(obj, "reportingInstance")
	}

	switch {
	case component != "" && host != "":
		return component + " " + host
	case component != "":
		return component
	case host != "":
		return host
	}
	return "<unknown>"
}

// eventTime reads whichever of the three time fields the API server filled in. Events
// written through events.k8s.io leave lastTimestamp empty.
func eventTime(obj *unstructured.Unstructured) time.Time {
	for _, field := range []string{"lastTimestamp", "eventTime", "firstTimestamp"} {
		value := nestedString(obj, field)
		if value == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
