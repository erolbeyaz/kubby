package kubectlsh

import (
	"fmt"
	"strings"
)

// split turns a typed line into an argument slice.
//
// It is not a shell and does not try to be: quotes group, a backslash escapes the next
// character, and nothing else means anything. There is no expansion, no substitution and
// no operator, so a `;` or a `$(…)` in a command reaches kubectl as the literal text it
// looks like rather than as something that runs.
func split(line string) ([]string, error) {
	var (
		args    []string
		current strings.Builder
		quote   rune
		escaped bool
		open    bool
	)

	for _, r := range line {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
			open = true

		case r == '\\' && quote != '\'':
			escaped = true

		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}

		case r == '\'' || r == '"':
			quote = r
			open = true

		case r == ' ' || r == '\t':
			if open {
				args = append(args, current.String())
				current.Reset()
				open = false
			}

		default:
			current.WriteRune(r)
			open = true
		}
	}

	if quote != 0 {
		return nil, fmt.Errorf("unclosed %c quote", quote)
	}
	if escaped {
		return nil, fmt.Errorf("the line ends in a backslash")
	}
	if open {
		args = append(args, current.String())
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("nothing to run")
	}
	return args, nil
}
