package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	tokEOF    = 0
	tokString = 's'
	tokNumber = 'n'
	tokName   = 'i'
)

var (
	jsonNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)
	luaInteger = regexp.MustCompile(`^-?[0-9]+$`)
	escapes    = map[byte]byte{'a': 7, 'b': 8, 'f': 12, 'n': 10, 'r': 13, 't': 9, 'v': 11, '\\': '\\', '"': '"', '\'': '\'', '\n': '\n'}
)

type token struct {
	kind byte
	text string
	line int
}

type lexer struct {
	src  []byte
	pos  int
	line int
}

func (l *lexer) fail(format string, args ...any) error {
	return fmt.Errorf("line %d: %s", l.line, fmt.Sprintf(format, args...))
}

func (l *lexer) peekByte(offset int) byte {
	if l.pos+offset < len(l.src) {
		return l.src[l.pos+offset]
	}
	return 0
}

func (l *lexer) skipSpace() error {
	for l.pos < len(l.src) {
		switch c := l.src[l.pos]; {
		case c == '\n':
			l.line++
			l.pos++
		case c == ' ' || c == '\t' || c == '\r':
			l.pos++
		case c == '-' && l.peekByte(1) == '-':
			if l.peekByte(2) == '[' && (l.peekByte(3) == '[' || l.peekByte(3) == '=') {
				return l.fail("long comment")
			}
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		default:
			return nil
		}
	}
	return nil
}

func (l *lexer) next() (token, error) {
	if err := l.skipSpace(); err != nil {
		return token{}, err
	}
	if l.pos >= len(l.src) {
		return token{kind: tokEOF, line: l.line}, nil
	}
	switch c := l.src[l.pos]; {
	case strings.IndexByte("{}[]=,;", c) >= 0:
		l.pos++
		return token{kind: c, line: l.line}, nil
	case c == '"' || c == '\'':
		return l.str(c)
	case c == '-' || isDigit(c):
		return l.number()
	case isNameStart(c):
		return l.name(), nil
	default:
		return token{}, l.fail("unexpected character %q", c)
	}
}

func (l *lexer) str(quote byte) (token, error) {
	line := l.line
	var b strings.Builder
	for l.pos++; l.pos < len(l.src); {
		c := l.src[l.pos]
		l.pos++
		switch c {
		case quote:
			if !utf8.ValidString(b.String()) {
				return token{}, l.fail("string is not UTF-8")
			}
			return token{kind: tokString, text: b.String(), line: line}, nil
		case '\n':
			return token{}, l.fail("unescaped newline in string")
		case '\\':
			if err := l.escape(&b); err != nil {
				return token{}, err
			}
		default:
			b.WriteByte(c)
		}
	}
	return token{}, l.fail("unterminated string")
}

func (l *lexer) escape(b *strings.Builder) error {
	c := l.peekByte(0)
	if isDigit(c) {
		return l.decimalEscape(b)
	}
	value, ok := escapes[c]
	if !ok {
		return l.fail("unknown escape \\%c", c)
	}
	if c == '\n' {
		l.line++
	}
	l.pos++
	b.WriteByte(value)
	return nil
}

func (l *lexer) decimalEscape(b *strings.Builder) error {
	start := l.pos
	for l.pos-start < 3 && isDigit(l.peekByte(0)) {
		l.pos++
	}
	value, _ := strconv.Atoi(string(l.src[start:l.pos]))
	if value > 255 {
		return l.fail("escape \\%d is above 255", value)
	}
	b.WriteByte(byte(value))
	return nil
}

func (l *lexer) number() (token, error) {
	start := l.pos
	l.pos++
	for l.pos < len(l.src) && isNumberPart(l.src[l.pos], l.src[l.pos-1]) {
		l.pos++
	}
	text := string(l.src[start:l.pos])
	if !jsonNumber.MatchString(text) {
		return token{}, l.fail("number %q has no exact JSON form", text)
	}
	return token{kind: tokNumber, text: text, line: l.line}, nil
}

func (l *lexer) name() token {
	start := l.pos
	for l.pos < len(l.src) && (isNameStart(l.src[l.pos]) || isDigit(l.src[l.pos])) {
		l.pos++
	}
	return token{kind: tokName, text: string(l.src[start:l.pos]), line: l.line}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isNameStart(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func isNumberPart(c, previous byte) bool {
	return isDigit(c) || isNameStart(c) || c == '.' || (c == '+' || c == '-') && (previous == 'e' || previous == 'E')
}

type parser struct {
	lex lexer
	tok token
}

func (p *parser) advance() error {
	t, err := p.lex.next()
	p.tok = t
	return err
}

func (p *parser) expect(kind byte) error {
	if p.tok.kind != kind {
		return p.unexpected()
	}
	return p.advance()
}

func (p *parser) unexpected() error {
	return fmt.Errorf("line %d: unexpected %s", p.tok.line, describe(p.tok))
}

func describe(t token) string {
	switch t.kind {
	case tokEOF:
		return "end of file"
	case tokString:
		return "string"
	case tokNumber, tokName:
		return strconv.Quote(t.text)
	default:
		return strconv.Quote(string(t.kind))
	}
}

func parseSavedVariables(src []byte) (map[string]any, error) {
	p := &parser{lex: lexer{src: src, line: 1}}
	if err := p.advance(); err != nil {
		return nil, err
	}
	globals := map[string]any{}
	seen := map[string]bool{}
	for p.tok.kind != tokEOF {
		name := p.tok.text
		if p.tok.kind != tokName || seen[name] {
			return nil, p.unexpected()
		}
		seen[name] = true
		value, err := p.assignment()
		if err != nil {
			return nil, err
		}
		if value != nil {
			globals[name] = value
		}
	}
	return globals, nil
}

func (p *parser) assignment() (any, error) {
	if err := p.nameEquals(); err != nil {
		return nil, err
	}
	return p.value()
}

func (p *parser) nameEquals() error {
	if err := p.advance(); err != nil {
		return err
	}
	return p.expect('=')
}

func (p *parser) value() (any, error) {
	t := p.tok
	if err := p.advance(); err != nil {
		return nil, err
	}
	switch {
	case t.kind == '{':
		return p.table()
	case t.kind == tokString:
		return t.text, nil
	case t.kind == tokNumber:
		return json.Number(t.text), nil
	case t.kind == tokName && (t.text == "true" || t.text == "false"):
		return t.text == "true", nil
	case t.kind == tokName && t.text == "nil":
		return nil, nil
	}
	p.tok = t
	return nil, p.unexpected()
}

type tableBuilder struct {
	fields   map[string]any
	seen     map[string]bool
	onlyInts bool
	next     int64
}

func (p *parser) table() (any, error) {
	t := &tableBuilder{fields: map[string]any{}, seen: map[string]bool{}, onlyInts: true, next: 1}
	for p.tok.kind != '}' {
		if err := p.field(t); err != nil {
			return nil, err
		}
		if p.tok.kind != ',' && p.tok.kind != ';' {
			break
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
	}
	if err := p.expect('}'); err != nil {
		return nil, err
	}
	return t.result(), nil
}

func (p *parser) field(t *tableBuilder) error {
	line := p.tok.line
	key, isInt, err := p.fieldKey(t)
	if err != nil {
		return err
	}
	if t.seen[key] {
		return fmt.Errorf("line %d: duplicate key %q", line, key)
	}
	t.seen[key] = true
	value, err := p.value()
	if value != nil {
		t.fields[key] = value
		t.onlyInts = t.onlyInts && isInt
	}
	return err
}

func (p *parser) fieldKey(t *tableBuilder) (string, bool, error) {
	switch {
	case p.tok.kind == '[':
		return p.bracketKey()
	case p.tok.kind == tokName && p.tok.text != "true" && p.tok.text != "false" && p.tok.text != "nil":
		name := p.tok.text
		return name, false, p.nameEquals()
	}
	key := strconv.FormatInt(t.next, 10)
	t.next++
	return key, true, nil
}

func (p *parser) bracketKey() (string, bool, error) {
	if err := p.advance(); err != nil {
		return "", false, err
	}
	key, isInt, err := p.keyText()
	if err != nil {
		return "", false, err
	}
	if err := p.advance(); err != nil {
		return "", false, err
	}
	if err := p.expect(']'); err != nil {
		return "", false, err
	}
	return key, isInt, p.expect('=')
}

func (p *parser) keyText() (string, bool, error) {
	switch {
	case p.tok.kind == tokString:
		return p.tok.text, false, nil
	case p.tok.kind == tokNumber && luaInteger.MatchString(p.tok.text):
		n, err := strconv.ParseInt(p.tok.text, 10, 64)
		return strconv.FormatInt(n, 10), true, err
	case p.tok.kind == tokNumber:
		return p.tok.text, false, nil
	}
	return "", false, p.unexpected()
}

func (t *tableBuilder) result() any {
	if !t.onlyInts || len(t.fields) == 0 {
		return t.fields
	}
	array := make([]any, len(t.fields))
	for i := range array {
		value, ok := t.fields[strconv.Itoa(i+1)]
		if !ok {
			return t.fields
		}
		array[i] = value
	}
	return array
}
