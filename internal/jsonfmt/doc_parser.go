// Package jsonfmt marshals JSON in the exact shape the bash TUI's jq
// pipeline produces: two-space indent, no HTML escaping, key order
// preserved, trailing newline — so Go-written files are interchangeable
// with jq-rewritten ones.
package jsonfmt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Entry is one key/value pair of an ordered JSON object.
type Entry struct {
	Key string
	Val any
}

// Doc is a JSON object preserving key order.
type Doc struct {
	Entries []Entry
}

// Get returns the value for key.
func (d *Doc) Get(key string) (any, bool) {
	for _, e := range d.Entries {
		if e.Key == key {
			return e.Val, true
		}
	}
	return nil, false
}

// GetString returns the string value for key, or def when absent/other type.
func (d *Doc) GetString(key, def string) string {
	if v, ok := d.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

// GetBool returns the boolean value for key, or def when absent.
func (d *Doc) GetBool(key string, def bool) bool {
	if v, ok := d.Get(key); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

// GetArray returns the array value for key, or nil when absent.
func (d *Doc) GetArray(key string) ([]any, bool) {
	if v, ok := d.Get(key); ok {
		if a, ok := v.([]any); ok {
			return a, true
		}
	}
	return nil, false
}

// Set replaces the value for key in place, or appends it at the end.
func (d *Doc) Set(key string, val any) {
	for i := range d.Entries {
		if d.Entries[i].Key == key {
			d.Entries[i].Val = val
			return
		}
	}
	d.Entries = append(d.Entries, Entry{Key: key, Val: val})
}

// MarshalJSON renders the doc compactly; jsonfmt.Marshal adds jq-style indent.
func (d *Doc) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, e := range d.Entries {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(encodeString(e.Key))
		b.WriteByte(':')
		if err := encodeValue(&b, e.Val); err != nil {
			return nil, err
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func encodeValue(b *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case *Doc:
		enc, err := t.MarshalJSON()
		if err != nil {
			return err
		}
		b.Write(enc)
		return nil
	case []any:
		b.WriteByte('[')
		for i, el := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := encodeValue(b, el); err != nil {
				return err
			}
		}
		b.WriteByte(']')
		return nil
	case string:
		b.WriteString(encodeString(t))
		return nil
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
		return nil
	case nil:
		b.WriteString("null")
		return nil
	default:
		enc, err := json.Marshal(v)
		if err != nil {
			return err
		}
		b.Write(enc)
		return nil
	}
}

func encodeString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimRight(b.String(), "\n")
}

// Parse decodes JSON into an ordered Doc. Object values become *Doc, arrays
// become []any, numbers become json.Number, strings/bools/null as-is.
func Parse(data []byte) (*Doc, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	doc, ok := v.(*Doc)
	if !ok {
		return nil, fmt.Errorf("jsonfmt: top-level value is not an object")
	}
	return doc, nil
}

func parseValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return decodeFrom(tok, dec)
}

func decodeFrom(tok json.Token, dec *json.Decoder) (any, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			doc := &Doc{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("jsonfmt: non-string object key")
				}
				val, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				doc.Entries = append(doc.Entries, Entry{Key: key, Val: val})
			}
			if _, err := dec.Token(); err != nil { // consume '}'
				return nil, err
			}
			return doc, nil
		case '[':
			arr := []any{}
			for dec.More() {
				val, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, val)
			}
			if _, err := dec.Token(); err != nil { // consume ']'
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("jsonfmt: unexpected delimiter %v", t)
	default:
		return tok, nil
	}
}
