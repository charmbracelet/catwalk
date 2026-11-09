// Package jsonfmt provides an opinionated JSON formatter that keeps small
// objects and arrays on a single line. It produces the same layout as
// encoding/json's MarshalIndent, except that any object or array whose
// compact rendering fits within the line width is collapsed onto one line.
package jsonfmt

import (
	"encoding/json"
	"fmt"
)

// Options controls the output layout of Marshal and Format.
type Options struct {
	// Indent is the string prefixed once per nesting level.
	// Defaults to two spaces when zero.
	Indent string
	// Width is the maximum line width, in bytes. Objects and arrays whose
	// compact rendering fits on their line, within this width, are collapsed.
	// Defaults to 80 when zero.
	Width int
}

// Marshal marshals v as JSON formatted like json.MarshalIndent with "", "  ",
// except that objects and arrays that fit within 80 columns are collapsed
// onto a single line.
func Marshal(v any) ([]byte, error) {
	return MarshalOptions(v, Options{})
}

// MarshalOptions marshals v as JSON with the given layout options.
func MarshalOptions(v any, opts Options) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("jsonfmt: unable to marshal value: %w", err)
	}
	return Format(data, opts), nil
}

// Format reformats json, which must be valid JSON, with the given layout
// options. The input may already contain whitespace; it is reparsed and
// reprinted. Key order and scalar values are preserved exactly.
func Format(data []byte, opts Options) []byte {
	root, _, ok := parse(data, 0)
	if !ok {
		// Not valid JSON as far as we can tell; return the input unchanged
		// rather than corrupting it.
		return data
	}
	indent := opts.Indent
	if indent == "" {
		indent = "  "
	}
	width := opts.Width
	if width == 0 {
		width = 80
	}
	f := formatter{indent: indent, indentLen: len(indent), width: width}
	buf := make([]byte, 0, len(data)*2)
	buf = f.appendValue(buf, root, 0, 0)
	buf = append(buf, '\n')
	return buf
}

type node struct {
	kind byte // 'o' for object, 'a' for array, 'v' for scalar
	raw  []byte
	keys [][]byte
	vals []*node
}

type formatter struct {
	indent    string
	indentLen int
	width     int
}

func (f *formatter) appendValue(buf []byte, n *node, depth, col int) []byte {
	switch n.kind {
	case 'v':
		return append(buf, n.raw...)
	}
	compact := f.compact(n)
	if col+len(compact) <= f.width {
		return append(buf, compact...)
	}
	buf = append(buf, n.kind) // '{' or '['
	for i, v := range n.vals {
		buf = append(buf, '\n')
		buf = f.appendTabs(buf, depth+1)
		childCol := (depth + 1) * f.indentLen
		if n.kind == '{' {
			buf = append(buf, n.keys[i]...)
			buf = append(buf, ": "...)
			childCol += len(n.keys[i]) + 2
		}
		buf = f.appendValue(buf, v, depth+1, childCol)
		if i < len(n.vals)-1 {
			buf = append(buf, ',')
		}
	}
	if len(n.vals) > 0 {
		buf = append(buf, '\n')
		buf = f.appendTabs(buf, depth)
	}
	return append(buf, closing(n.kind))
}

// compact returns the fully inline rendering of n, with no newlines and a
// space after each colon and comma.
func (f *formatter) compact(n *node) []byte {
	buf := make([]byte, 0, 64)
	buf = appendCompact(buf, n)
	return buf
}

func appendCompact(buf []byte, n *node) []byte {
	switch n.kind {
	case 'v':
		return append(buf, n.raw...)
	}
	buf = append(buf, n.kind)
	if n.kind == '{' && len(n.vals) > 0 {
		buf = append(buf, ' ')
	}
	for i, v := range n.vals {
		if i > 0 {
			buf = append(buf, ',', ' ')
		}
		if n.kind == '{' {
			buf = append(buf, n.keys[i]...)
			buf = append(buf, ": "...)
		}
		buf = appendCompact(buf, v)
	}
	if n.kind == '{' && len(n.vals) > 0 {
		buf = append(buf, ' ')
	}
	return append(buf, closing(n.kind))
}

func closing(open byte) byte {
	if open == '{' {
		return '}'
	}
	return ']'
}

func (f *formatter) appendTabs(buf []byte, depth int) []byte {
	for range depth {
		buf = append(buf, f.indent...)
	}
	return buf
}

// parse parses the value at data[i] and returns it along with the index just
// past it. ok is false if the input is malformed at any nesting depth.
func parse(data []byte, i int) (*node, int, bool) {
	i = skipSpace(data, i)
	if i >= len(data) {
		return nil, i, false
	}
	switch c := data[i]; c {
	case '{':
		return parseComposite(data, i, '{', '}')
	case '[':
		return parseComposite(data, i, '[', ']')
	case '"':
		end, ok := scanString(data, i)
		if !ok {
			return nil, end, false
		}
		return &node{kind: 'v', raw: data[i:end]}, end, true
	default:
		end := scanScalar(data, i)
		if end == i {
			return nil, end, false
		}
		return &node{kind: 'v', raw: data[i:end]}, end, true
	}
}

func parseComposite(data []byte, i int, open, closeChar byte) (*node, int, bool) {
	n := &node{kind: open}
	i = skipSpace(data, i+1)
	if i < len(data) && data[i] == closeChar {
		return n, i + 1, true
	}
	for {
		if open == '{' {
			i = skipSpace(data, i)
			if i >= len(data) || data[i] != '"' {
				return nil, i, false
			}
			keyEnd, ok := scanString(data, i)
			if !ok {
				return nil, keyEnd, false
			}
			n.keys = append(n.keys, data[i:keyEnd])
			i = skipSpace(data, keyEnd)
			if i >= len(data) || data[i] != ':' {
				return nil, i, false
			}
			i++
		}
		val, next, ok := parse(data, i)
		if !ok {
			return nil, next, false
		}
		n.vals = append(n.vals, val)
		i = skipSpace(data, next)
		if i >= len(data) {
			return nil, i, false
		}
		if data[i] == ',' {
			i++
			continue
		}
		if data[i] == closeChar {
			return n, i + 1, true
		}
		return nil, i, false
	}
}

func skipSpace(data []byte, i int) int {
	for i < len(data) {
		switch data[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

// scanString returns the index just past the closing quote of the string
// starting at data[i], handling escape sequences.
func scanString(data []byte, i int) (int, bool) {
	i++ // opening quote
	for i < len(data) {
		switch data[i] {
		case '\\':
			i += 2
		case '"':
			return i + 1, true
		default:
			i++
		}
	}
	return i, false
}

// scanScalar returns the index just past a number, true, false, or null
// starting at data[i]. It returns i unchanged if no scalar starts there.
func scanScalar(data []byte, i int) int {
	start := i
	for i < len(data) {
		switch c := data[i]; {
		case c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' ||
			(c >= '0' && c <= '9'):
			i++
		case (c == 't' || c == 'f' || c == 'n') && i == start:
			for i < len(data) && data[i] >= 'a' && data[i] <= 'z' {
				i++
			}
			return i
		default:
			if i == start {
				return start
			}
			return i
		}
	}
	return i
}
