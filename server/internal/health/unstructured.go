package health

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

func nested(obj *unstructured.Unstructured, fields ...string) string {
	value, _, _ := unstructured.NestedString(obj.Object, fields...)
	return value
}

func conditions(obj *unstructured.Unstructured) []map[string]any {
	return mapsAt(obj, "status", "conditions")
}

func containerStatuses(obj *unstructured.Unstructured, key string) []map[string]any {
	return mapsAt(obj, "status", key)
}

func mapsAt(obj *unstructured.Unstructured, fields ...string) []map[string]any {
	raw, _, _ := unstructured.NestedSlice(obj.Object, fields...)

	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(map[string]any); ok {
			out = append(out, value)
		}
	}
	return out
}

func intOf(value any) int {
	switch typed := value.(type) {
	case int64:
		return int(typed)
	case int:
		return typed
	case float64:
		return int(typed)
	}
	return 0
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
