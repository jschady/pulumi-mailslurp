package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDedent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"strips the common tab indent", "\n\t\tone\n\t\ttwo\n\t", "one\ntwo"},
		{"keeps the extra indent of a nested line", "\n\tone\n\t\ttwo\n", "one\n\ttwo"},
		{"keeps a blank line between paragraphs", "\n\t\tone\n\n\t\ttwo\n\t", "one\n\ntwo"},
		{"turns double quotes into backticks", "\n\t\tSet \"apiKey\" now.\n\t", "Set `apiKey` now."},
		{"leaves an unindented string alone", "one two", "one two"},
		{"returns an empty string unchanged", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, dedent(tt.in))
		})
	}
}

// A leftover space indent makes markdown render the description as a code block.
func TestDedentStripsSpaceIndentationToo(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "one\n  two", dedent("\n  one\n    two\n"))
}
