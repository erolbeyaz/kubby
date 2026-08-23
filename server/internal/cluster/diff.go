package cluster

import (
	"errors"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"sigs.k8s.io/yaml"
)

// DiffLine is one line of a unified diff.
type DiffLine struct {
	Kind string `json:"kind"` // context | added | removed
	Text string `json:"text"`
}

// Fields the server owns and the reader did not write. Showing them turns every diff
// into noise about resourceVersion and managedFields, which is how people stop reading
// diffs at all.
var serverOwnedFields = []string{
	"metadata.resourceVersion",
	"metadata.generation",
	"metadata.managedFields",
	"metadata.creationTimestamp",
	"metadata.uid",
	"metadata.selfLink",
	"status",
}

// Diff renders what changed between two objects as a unified diff of their YAML.
//
// Comparing rendered YAML rather than walking the objects keeps the output in the shape
// the person is editing: they wrote YAML, and the answer to "what will this do" should
// be in the same language as the question.
func Diff(before, after *unstructured.Unstructured) []DiffLine {
	return diffText(render(before), render(after))
}

func render(obj *unstructured.Unstructured) string {
	if obj == nil {
		return ""
	}

	cleaned := obj.DeepCopy()
	for _, path := range serverOwnedFields {
		unstructured.RemoveNestedField(cleaned.Object, strings.Split(path, ".")...)
	}
	unstructured.RemoveNestedField(cleaned.Object,
		"metadata", "annotations", "kubectl.kubernetes.io/last-applied-configuration")
	// Removing the last entry leaves an empty map behind, which renders as a changed
	// line for something nobody wrote.
	pruneEmptyMaps(cleaned.Object, []string{"metadata", "annotations"}, []string{"metadata", "labels"})

	out, err := yaml.Marshal(cleaned.Object)
	if err != nil {
		return ""
	}
	return string(out)
}

// diffText is a longest-common-subsequence diff over lines.
func diffText(before, after string) []DiffLine {
	if before == after {
		return nil
	}

	a := splitLines(before)
	b := splitLines(after)

	// The table is |a| x |b| ints; manifests are small enough that this is cheap and
	// exact, which a heuristic diff would not be.
	lengths := make([][]int, len(a)+1)
	for i := range lengths {
		lengths[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lengths[i][j] = lengths[i+1][j+1] + 1
				continue
			}
			lengths[i][j] = max(lengths[i+1][j], lengths[i][j+1])
		}
	}

	var out []DiffLine
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, DiffLine{Kind: "context", Text: a[i]})
			i++
			j++
		case lengths[i+1][j] >= lengths[i][j+1]:
			out = append(out, DiffLine{Kind: "removed", Text: a[i]})
			i++
		default:
			out = append(out, DiffLine{Kind: "added", Text: b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		out = append(out, DiffLine{Kind: "removed", Text: a[i]})
	}
	for ; j < len(b); j++ {
		out = append(out, DiffLine{Kind: "added", Text: b[j]})
	}
	return out
}

func pruneEmptyMaps(object map[string]any, paths ...[]string) {
	for _, path := range paths {
		value, found, err := unstructured.NestedMap(object, path...)
		if err == nil && found && len(value) == 0 {
			unstructured.RemoveNestedField(object, path...)
		}
	}
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(text, "\n"), "\n")
}

// translateWriteError names the refusal.
//
// "the server rejected it" is true of every failure here and useful for none of them. A
// quota, an admission webhook and an immutable field are three different things to do
// next, and the message has to say which.
func translateWriteError(err error, resourceType ResourceType, name string) error {
	switch {
	case apierrors.IsForbidden(err):
		return fmt.Errorf("%w: %s", ErrClusterDenied, apierrors.ReasonForError(err))
	case apierrors.IsInvalid(err):
		return fmt.Errorf("the manifest is not valid for %s %q: %s",
			resourceType.Kind, name, causesOf(err))
	case apierrors.IsConflict(err):
		return fmt.Errorf("%w: reload it and reapply your change", ErrConflict)
	case apierrors.IsAlreadyExists(err):
		return fmt.Errorf("%s %q already exists", resourceType.Kind, name)
	case apierrors.IsNotFound(err):
		return fmt.Errorf("%w: %s %q", ErrResourceNotFound, resourceType.Kind, name)
	case isQuotaError(err):
		return fmt.Errorf("a resource quota refused this: %s", err.Error())
	case isWebhookError(err):
		return fmt.Errorf("an admission webhook refused this: %s", err.Error())
	}
	return translateAPIError(err, resourceType)
}

// causesOf pulls the field-level reasons out of a validation error, which is where the
// actually useful part of an Invalid response lives.
func causesOf(err error) string {
	var status apierrors.APIStatus
	if !errors.As(err, &status) || status.Status().Details == nil {
		return err.Error()
	}

	reasons := make([]string, 0, len(status.Status().Details.Causes))
	for _, cause := range status.Status().Details.Causes {
		if cause.Field != "" {
			reasons = append(reasons, fmt.Sprintf("%s: %s", cause.Field, cause.Message))
			continue
		}
		reasons = append(reasons, cause.Message)
	}
	if len(reasons) == 0 {
		return err.Error()
	}
	return strings.Join(reasons, "; ")
}

func isQuotaError(err error) bool {
	return apierrors.IsForbidden(err) && strings.Contains(err.Error(), "exceeded quota")
}

func isWebhookError(err error) bool {
	text := err.Error()
	return strings.Contains(text, "admission webhook") || strings.Contains(text, "denied the request")
}
