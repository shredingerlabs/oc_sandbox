package jsonfmt

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Marshal renders v as jq-style pretty JSON with a trailing newline.
func Marshal(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
	s := b.String()
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}
