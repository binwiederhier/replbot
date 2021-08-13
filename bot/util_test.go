package bot

import "testing"
import "github.com/stretchr/testify/assert"

func TestUnquote(t *testing.T) {
	assert.Equal(t, "line 1\nline\t2\nline 3", unquote("line 1\\nline\\t2\\nline \\x33"))
}
