// Package xmldoc renders a spec.XMLDoc. With no template it converts the JSON
// tree generically; with a template it dispatches to a named format
// (templates.go). Output is written incrementally, so a large document does not
// sit in memory.
//
// Generic mapping (a common JSON⇄XML convention):
//   - an object becomes one child element per key
//   - a key beginning "@" is an attribute on the enclosing element
//   - a key "#text" is text content of the enclosing element
//   - an array repeats its element once per item
//   - null is an empty element; scalars are text
package xmldoc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/levelcodingdev/godoc/internal/spec"
)

// Render writes the XML for s to w.
func Render(w io.Writer, s *spec.XMLDoc) error {
	bw := bufio.NewWriter(w)
	defer bw.Flush()

	if s.WantDeclaration() {
		if _, err := bw.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`); err != nil {
			return err
		}
		if s.Indent {
			bw.WriteByte('\n')
		}
	}

	if s.Template != "" {
		return renderTemplate(bw, s)
	}

	var tree any
	if err := json.Unmarshal(s.Data, &tree); err != nil {
		return fmt.Errorf("xmldoc: %w", err)
	}
	root := s.Root
	if root == "" {
		root = "root"
	}
	e := &encoder{w: bw, indent: s.Indent}
	e.element(root, tree, 0)
	return e.err
}

type encoder struct {
	w      *bufio.Writer
	indent bool
	err    error
}

func (e *encoder) write(s string) {
	if e.err == nil {
		_, e.err = e.w.WriteString(s)
	}
}

func (e *encoder) pad(depth int) {
	if e.indent {
		e.write("\n")
		for i := 0; i < depth; i++ {
			e.write("  ")
		}
	}
}

// element emits <name ...attrs>...children...</name> for v.
func (e *encoder) element(name string, v any, depth int) {
	attrs, text, children := split(v)

	e.write("<" + name)
	for _, a := range attrs {
		e.write(fmt.Sprintf(" %s=%q", a.k, escapeAttr(scalar(a.v))))
	}

	if text == "" && len(children) == 0 {
		e.write("/>")
		return
	}
	e.write(">")

	if text != "" {
		e.write(escapeText(text))
	}
	for _, c := range children {
		e.pad(depth + 1)
		e.element(c.k, c.v, depth+1)
	}
	if len(children) > 0 {
		e.pad(depth)
	}
	e.write("</" + name + ">")
}

type kv struct {
	k string
	v any
}

// split classifies an object's members into attributes, #text and child
// elements; a non-object v is all text. Arrays are handled by the caller
// repeating the element, so split flattens a top-level array into repeated
// children under the same key is done in element via children expansion below.
func split(v any) (attrs []kv, text string, children []kv) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			val := t[k]
			switch {
			case len(k) > 1 && k[0] == '@':
				attrs = append(attrs, kv{k[1:], val})
			case k == "#text":
				text = scalar(val)
			default:
				if arr, ok := val.([]any); ok {
					for _, item := range arr {
						children = append(children, kv{k, item})
					}
				} else {
					children = append(children, kv{k, val})
				}
			}
		}
	case []any:
		for _, item := range t {
			children = append(children, kv{"item", item})
		}
	case nil:
		// empty element
	default:
		text = scalar(v)
	}
	return
}

func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func escapeText(s string) string {
	return replacer.Replace(s)
}

func escapeAttr(s string) string {
	return attrReplacer.Replace(s)
}
