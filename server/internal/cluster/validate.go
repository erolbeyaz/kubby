package cluster

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// validateRef checks a namespace and name before they are used.
//
// client-go validates these too and refuses to send the request, which is the right
// behaviour — but its error arrives as plain text with no type to match on, so it landed
// in the "could not read from the cluster" bucket and was reported as a gateway failure.
// A name the caller made up is the caller's to fix, and sending them to investigate a
// cluster that is working perfectly wastes the one thing an error message is for.
//
// Checked here rather than at each HTTP handler: every read and write path funnels
// through this package, and a rule enforced in fifteen places is a rule that is missing
// from one of them.
func validateRef(namespace, name string) error {
	if namespace != "" {
		if problems := validation.IsDNS1123Label(namespace); len(problems) > 0 {
			return fmt.Errorf("%w: namespace %q is not a valid name — %s",
				ErrRequestRejected, namespace, strings.Join(problems, "; "))
		}
	}
	if name != "" {
		// Subdomain rather than label: object names may contain dots, namespaces may not.
		if problems := validation.IsDNS1123Subdomain(name); len(problems) > 0 {
			return fmt.Errorf("%w: %q is not a valid name — %s",
				ErrRequestRejected, name, strings.Join(problems, "; "))
		}
	}
	return nil
}

// validateNamespaces checks a filter that may name several.
func validateNamespaces(namespaces []string) error {
	for _, namespace := range namespaces {
		if err := validateRef(namespace, ""); err != nil {
			return err
		}
	}
	return nil
}
