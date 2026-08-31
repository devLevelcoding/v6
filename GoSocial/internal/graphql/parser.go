// Package graphql is a minimal, hand-rolled GraphQL-shaped query engine:
// a parser for `{ field(arg: "value") { subfield { nested } } }` syntax
// (this file) and an Executor that resolves the parsed selection against
// a backing HTTP service (executor.go).
package graphql

import (
	"fmt"
	"strings"
	"unicode"
)

// Selection is one requested field, its arguments, and its (possibly
// empty) nested selection set.
type Selection struct {
	Name string
	Args map[string]string
	Sub  []Selection
}

// Parse parses a query string into its top-level Selection set.
func Parse(query string) ([]Selection, error) {
	p := &parser{input: query}
	p.skipSpace()
	sels, err := p.parseSelectionSet()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos != len(p.input) {
		return nil, fmt.Errorf("unexpected trailing input at %d: %q", p.pos, p.input[p.pos:])
	}
	return sels, nil
}

type parser struct {
	input string
	pos   int
}

func (p *parser) skipSpace() {
	for p.pos < len(p.input) && unicode.IsSpace(rune(p.input[p.pos])) {
		p.pos++
	}
}

func (p *parser) peek() byte {
	if p.pos >= len(p.input) {
		return 0
	}
	return p.input[p.pos]
}

func (p *parser) expect(c byte) error {
	p.skipSpace()
	if p.peek() != c {
		return fmt.Errorf("expected %q at position %d, got %q", c, p.pos, p.rest())
	}
	p.pos++
	return nil
}

func (p *parser) rest() string {
	end := p.pos + 10
	if end > len(p.input) {
		end = len(p.input)
	}
	return p.input[p.pos:end]
}

func (p *parser) parseSelectionSet() ([]Selection, error) {
	if err := p.expect('{'); err != nil {
		return nil, err
	}
	var sels []Selection
	for {
		p.skipSpace()
		if p.peek() == '}' {
			p.pos++
			return sels, nil
		}
		sel, err := p.parseSelection()
		if err != nil {
			return nil, err
		}
		sels = append(sels, sel)
	}
}

func (p *parser) parseSelection() (Selection, error) {
	name := p.parseName()
	if name == "" {
		return Selection{}, fmt.Errorf("expected field name at position %d, got %q", p.pos, p.rest())
	}
	sel := Selection{Name: name}

	p.skipSpace()
	if p.peek() == '(' {
		p.pos++
		args, err := p.parseArgs()
		if err != nil {
			return Selection{}, err
		}
		sel.Args = args
		if err := p.expect(')'); err != nil {
			return Selection{}, err
		}
	}

	p.skipSpace()
	if p.peek() == '{' {
		sub, err := p.parseSelectionSet()
		if err != nil {
			return Selection{}, err
		}
		sel.Sub = sub
	}
	return sel, nil
}

func (p *parser) parseArgs() (map[string]string, error) {
	args := map[string]string{}
	for {
		p.skipSpace()
		if p.peek() == ')' {
			return args, nil
		}
		name := p.parseName()
		if name == "" {
			return nil, fmt.Errorf("expected argument name at position %d", p.pos)
		}
		if err := p.expect(':'); err != nil {
			return nil, err
		}
		p.skipSpace()
		val, err := p.parseStringLiteral()
		if err != nil {
			return nil, err
		}
		args[name] = val
		p.skipSpace()
		if p.peek() == ',' {
			p.pos++
			continue
		}
	}
}

func (p *parser) parseName() string {
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.input) {
		c := p.input[p.pos]
		if unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c)) || c == '_' {
			p.pos++
			continue
		}
		break
	}
	return p.input[start:p.pos]
}

func (p *parser) parseStringLiteral() (string, error) {
	if p.peek() != '"' {
		return "", fmt.Errorf("expected string literal at position %d, got %q", p.pos, p.rest())
	}
	p.pos++
	var b strings.Builder
	for p.pos < len(p.input) && p.input[p.pos] != '"' {
		b.WriteByte(p.input[p.pos])
		p.pos++
	}
	if p.pos >= len(p.input) {
		return "", fmt.Errorf("unterminated string literal")
	}
	p.pos++
	return b.String(), nil
}
