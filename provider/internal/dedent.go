package internal

import "strings"

// dedent strips the common leading whitespace from a raw-string description and
// turns double quotes into backticks, which raw strings cannot contain.
func dedent(s string) string {
	s = strings.ReplaceAll(s, `"`, "`")
	lines := strings.Split(s, "\n")
	indent := -1
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}
		if n := len(line) - len(trimmed); indent == -1 || n < indent {
			indent = n
		}
	}
	if indent > 0 {
		for i, line := range lines {
			if len(line) >= indent {
				lines[i] = line[indent:]
			}
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
