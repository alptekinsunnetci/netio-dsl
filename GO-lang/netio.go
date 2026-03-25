// Package netio provides a complete, single-file library for parsing, evaluating,
// linting, validating, diffing, and emitting Netio DSL configuration files.
//
// Quick start:
//
//	cfg, err := netio.ParseString(`
//	  app myservice:
//	    version = "1.0"
//	    replicas = 3
//	`)
//	jsonBytes, _ := netio.ToJSON(cfg)
//	fmt.Println(string(jsonBytes))
package netio

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// =========================================================================
// Config
// =========================================================================

// Config is the evaluated, in-memory representation of a Netio DSL file.
// Values are one of: string, int64, float64, bool, Config, []any.
type Config map[string]any

// =========================================================================
// Token
// =========================================================================

// TokenType represents the type of a lexed token.
type TokenType int

const (
	TokenEOF     TokenType = iota
	TokenIdent             // bare word / identifier
	TokenString            // "quoted string"
	TokenInt               // 42
	TokenFloat             // 3.14
	TokenBool              // true / false
	TokenIPCIDR            // 192.168.1.0/24 or 10.0.0.1
	TokenColon             // :
	TokenEquals            // =
	TokenArrow             // ->
	TokenDash              // - (list item prefix)
	TokenNewline           // \n
	TokenIndent            // virtual INDENT
	TokenDedent            // virtual DEDENT
	TokenInclude           // include
	TokenExtends           // extends
	TokenComment           // # ...
	tokenCount             // sentinel for array sizing
)

// Array lookup instead of map — O(1) with no hash overhead.
var tokenNames = [tokenCount]string{
	TokenEOF: "EOF", TokenIdent: "IDENT", TokenString: "STRING",
	TokenInt: "INT", TokenFloat: "FLOAT", TokenBool: "BOOL",
	TokenIPCIDR: "IP_CIDR", TokenColon: "COLON", TokenEquals: "EQUALS",
	TokenArrow: "ARROW", TokenDash: "DASH", TokenNewline: "NEWLINE",
	TokenIndent: "INDENT", TokenDedent: "DEDENT", TokenInclude: "INCLUDE",
	TokenExtends: "EXTENDS", TokenComment: "COMMENT",
}

func (t TokenType) String() string {
	if int(t) >= 0 && int(t) < len(tokenNames) && tokenNames[t] != "" {
		return tokenNames[t]
	}
	return "TOKEN(" + strconv.Itoa(int(t)) + ")"
}

// Token is a single lexical unit.
type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Col     int
}

func (t Token) String() string {
	return "Token{" + t.Type.String() + " " + strconv.Quote(t.Literal) +
		" L" + strconv.Itoa(t.Line) + ":C" + strconv.Itoa(t.Col) + "}"
}

// =========================================================================
// Lexer — byte-based scanning (avoids []rune allocation)
// =========================================================================

var keywords = map[string]TokenType{
	"include": TokenInclude, "extends": TokenExtends,
	"true": TokenBool, "false": TokenBool,
}

// Lexer holds the state for tokenising a single source file.
type Lexer struct {
	src     string  // source kept as string — no []rune copy
	pos     int     // byte offset
	line    int
	col     int
	tokens  []Token
	indents []int
	buf     []byte  // reusable buffer for escape-containing strings
}

// NewLexer creates a new Lexer for the given source text.
func NewLexer(src string) *Lexer {
	return &Lexer{
		src:     src,
		line:    1,
		col:     1,
		indents: []int{0},
		tokens:  make([]Token, 0, len(src)/5+16), // pre-allocate ~1 token per 5 chars
		buf:     make([]byte, 0, 64),
	}
}

// Tokenise runs the full scan and returns all tokens including INDENT/DEDENT.
func (l *Lexer) Tokenise() ([]Token, error) {
	for l.pos < len(l.src) {
		if err := l.scanLine(); err != nil {
			return nil, err
		}
	}
	for len(l.indents) > 1 {
		l.emit(TokenDedent, "", l.line, l.col)
		l.indents = l.indents[:len(l.indents)-1]
	}
	l.emit(TokenEOF, "", l.line, l.col)
	return l.tokens, nil
}

func (l *Lexer) scanLine() error {
	startLine := l.line
	startCol := l.col
	// Fast space counting — no function-call overhead per byte
	indent := 0
	for l.pos < len(l.src) && l.src[l.pos] == ' ' {
		indent++
		l.pos++
		l.col++
	}
	// Blank or comment line
	if l.pos >= len(l.src) || l.src[l.pos] == '\n' || l.src[l.pos] == '#' {
		l.skipToEOL()
		if l.pos < len(l.src) && l.src[l.pos] == '\n' {
			l.advanceByte()
		}
		return nil
	}
	current := l.indents[len(l.indents)-1]
	switch {
	case indent > current:
		l.indents = append(l.indents, indent)
		l.emit(TokenIndent, "", startLine, startCol)
	case indent < current:
		for len(l.indents) > 1 && l.indents[len(l.indents)-1] > indent {
			l.emit(TokenDedent, "", startLine, startCol)
			l.indents = l.indents[:len(l.indents)-1]
		}
		if l.indents[len(l.indents)-1] != indent {
			return fmt.Errorf("line %d: inconsistent indentation", l.line)
		}
	}
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		if err := l.scanToken(); err != nil {
			return err
		}
	}
	l.emit(TokenNewline, "\n", l.line, l.col)
	if l.pos < len(l.src) && l.src[l.pos] == '\n' {
		l.advanceByte()
	}
	return nil
}

func (l *Lexer) scanToken() error {
	// Skip spaces inline — hot path
	for l.pos < len(l.src) && l.src[l.pos] == ' ' {
		l.pos++
		l.col++
	}
	if l.pos >= len(l.src) || l.src[l.pos] == '\n' {
		return nil
	}
	line, col := l.line, l.col
	b := l.src[l.pos]
	switch {
	case b == '#':
		l.skipToEOL()
	case b == ':':
		l.advanceByte()
		l.emit(TokenColon, ":", line, col)
	case b == '=':
		l.advanceByte()
		l.emit(TokenEquals, "=", line, col)
	case b == '-' && l.peekAt(1) == '>':
		l.pos += 2
		l.col += 2
		l.emit(TokenArrow, "->", line, col)
	case b == '-':
		next := l.peekAt(1)
		if next == ' ' || next == '\n' || next == 0 {
			l.advanceByte()
			l.emit(TokenDash, "-", line, col)
		} else if next >= '0' && next <= '9' {
			return l.scanNumber(line, col)
		} else {
			l.advanceByte()
			l.emit(TokenDash, "-", line, col)
		}
	case b == '"':
		return l.scanString(line, col)
	case b >= '0' && b <= '9':
		return l.scanNumber(line, col)
	case isIdentStartByte(b):
		return l.scanIdent(line, col)
	default:
		r, _ := utf8.DecodeRuneInString(l.src[l.pos:])
		return fmt.Errorf("line %d col %d: unexpected character %q", line, col, r)
	}
	return nil
}

// scanString — fast path for no-escape strings (zero-copy substring),
// falls back to buffer only when escapes are encountered.
func (l *Lexer) scanString(line, col int) error {
	l.advanceByte() // skip opening "
	start := l.pos
	// Fast path: scan for closing " without any escape
	for l.pos < len(l.src) {
		b := l.src[l.pos]
		if b == '"' {
			lit := l.src[start:l.pos] // zero-copy substring
			l.advanceByte()
			l.emit(TokenString, lit, line, col)
			return nil
		}
		if b == '\n' {
			return fmt.Errorf("line %d col %d: unterminated string literal", line, col)
		}
		if b == '\\' {
			return l.scanStringEscape(line, col, start)
		}
		l.pos++
		if b&0xC0 != 0x80 { // not a UTF-8 continuation byte = rune start
			l.col++
		}
	}
	return fmt.Errorf("line %d col %d: unterminated string literal", line, col)
}

// scanStringEscape handles strings that contain escape sequences.
// Copies content seen so far into l.buf, then continues with escape processing.
func (l *Lexer) scanStringEscape(line, col int, start int) error {
	l.buf = append(l.buf[:0], l.src[start:l.pos]...)
	for l.pos < len(l.src) {
		b := l.src[l.pos]
		if b == '"' {
			l.advanceByte()
			l.emit(TokenString, string(l.buf), line, col)
			return nil
		}
		if b == '\n' {
			break
		}
		if b == '\\' {
			l.advanceByte()
			if l.pos >= len(l.src) {
				break
			}
			switch l.src[l.pos] {
			case 'n':
				l.buf = append(l.buf, '\n')
			case 't':
				l.buf = append(l.buf, '\t')
			case '"':
				l.buf = append(l.buf, '"')
			case '\\':
				l.buf = append(l.buf, '\\')
			default:
				l.buf = append(l.buf, '\\', l.src[l.pos])
			}
			l.advanceByte()
		} else if b < utf8.RuneSelf {
			l.buf = append(l.buf, b)
			l.pos++
			l.col++
		} else {
			_, size := utf8.DecodeRuneInString(l.src[l.pos:])
			l.buf = append(l.buf, l.src[l.pos:l.pos+size]...)
			l.pos += size
			l.col++
		}
	}
	return fmt.Errorf("line %d col %d: unterminated string literal", line, col)
}

// scanNumber — zero-copy: extracts substring from source directly.
func (l *Lexer) scanNumber(line, col int) error {
	start := l.pos
	if l.src[l.pos] == '-' {
		l.pos++
		l.col++
	}
	dotCount := 0
	slashOrColon := false
	for l.pos < len(l.src) {
		b := l.src[l.pos]
		if b >= '0' && b <= '9' {
			l.pos++
			l.col++
		} else if b == '.' {
			dotCount++
			l.pos++
			l.col++
		} else if b == '/' || b == ':' {
			slashOrColon = true
			l.pos++
			l.col++
		} else {
			break
		}
	}
	lit := l.src[start:l.pos] // zero-copy substring
	switch {
	case slashOrColon || dotCount > 1:
		l.emit(TokenIPCIDR, lit, line, col)
	case dotCount == 1:
		l.emit(TokenFloat, lit, line, col)
	default:
		l.emit(TokenInt, lit, line, col)
	}
	return nil
}

// scanIdent — zero-copy: fast ASCII path, rune decode only for non-ASCII.
func (l *Lexer) scanIdent(line, col int) error {
	start := l.pos
	for l.pos < len(l.src) {
		b := l.src[l.pos]
		if b < utf8.RuneSelf {
			if isIdentContASCII(b) {
				l.pos++
				l.col++
				continue
			}
			break
		}
		// Multi-byte: decode rune
		r, size := utf8.DecodeRuneInString(l.src[l.pos:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			l.pos += size
			l.col++
			continue
		}
		break
	}
	if l.pos == start {
		r, _ := utf8.DecodeRuneInString(l.src[l.pos:])
		return fmt.Errorf("line %d col %d: unexpected character %q", line, col, r)
	}
	lit := l.src[start:l.pos] // zero-copy substring
	if tt, ok := keywords[lit]; ok {
		l.emit(tt, lit, line, col)
	} else {
		l.emit(TokenIdent, lit, line, col)
	}
	return nil
}

// --- Lexer helpers ---

func (l *Lexer) peekAt(offset int) byte {
	idx := l.pos + offset
	if idx >= len(l.src) || idx < 0 {
		return 0
	}
	return l.src[idx]
}

func (l *Lexer) advanceByte() byte {
	b := l.src[l.pos]
	l.pos++
	if b == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return b
}

// skipToEOL advances past everything until newline. Col is stale but
// doesn't matter since the next newline resets it.
func (l *Lexer) skipToEOL() {
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
	}
}

func (l *Lexer) emit(tt TokenType, lit string, line, col int) {
	l.tokens = append(l.tokens, Token{Type: tt, Literal: lit, Line: line, Col: col})
}

// isIdentStartByte returns true if b could start an identifier.
// For non-ASCII (>= 0x80), returns true to let scanIdent decode the rune.
func isIdentStartByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_' || b >= utf8.RuneSelf
}

// isIdentContASCII is the fast ASCII check for ident continuation.
func isIdentContASCII(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.'
}

// =========================================================================
// AST
// =========================================================================

// Position holds source location information.
type Position struct {
	Line int
	Col  int
}

func (p Position) String() string {
	return "L" + strconv.Itoa(p.Line) + ":C" + strconv.Itoa(p.Col)
}

// Node is the base interface for all AST nodes.
type Node interface {
	nodeType() string
	Pos() Position
}

// Statement is implemented by all top-level and block-level statements.
type Statement interface {
	Node
	stmtNode()
}

// Value is the interface for all scalar / literal value nodes.
type Value interface {
	Node
	valueNode()
	GoValue() any
}

// File is the root AST node for a parsed Netio DSL file.
type File struct {
	Includes   []*IncludeDirective
	Statements []Statement
	pos        Position
}

func (f *File) nodeType() string { return "File" }
func (f *File) Pos() Position    { return f.pos }

// IncludeDirective represents `include "path"`.
type IncludeDirective struct {
	Path string
	pos  Position
}

func (i *IncludeDirective) nodeType() string { return "Include" }
func (i *IncludeDirective) Pos() Position    { return i.pos }
func (i *IncludeDirective) stmtNode()        {}

// Block represents a named block with optional extends.
type Block struct {
	Path          []string
	ExtendsTarget string
	Body          []Statement
	pos           Position
}

func (b *Block) nodeType() string { return "Block" }
func (b *Block) Pos() Position    { return b.pos }
func (b *Block) stmtNode()        {}

// Assignment represents a key-value pair: `key = value` or `a -> b = value`.
type Assignment struct {
	Path  []string
	Value Value
	pos   Position
}

func (a *Assignment) nodeType() string { return "Assignment" }
func (a *Assignment) Pos() Position    { return a.pos }
func (a *Assignment) stmtNode()        {}

// ListBlock represents a list literal block.
type ListBlock struct {
	Path  []string
	Items []*ListItem
	pos   Position
}

func (lb *ListBlock) nodeType() string { return "ListBlock" }
func (lb *ListBlock) Pos() Position    { return lb.pos }
func (lb *ListBlock) stmtNode()        {}

// ListItem is one element inside a ListBlock.
type ListItem struct {
	Inline Value
	Fields []Statement
	pos    Position
}

func (li *ListItem) nodeType() string { return "ListItem" }
func (li *ListItem) Pos() Position    { return li.pos }

// --- Value types ---

type StringValue struct {
	V   string
	pos Position
}

func (s *StringValue) nodeType() string    { return "StringValue" }
func (s *StringValue) Pos() Position       { return s.pos }
func (s *StringValue) valueNode()          {}
func (s *StringValue) GoValue() any { return s.V }

type IntValue struct {
	V   int64
	pos Position
}

func (i *IntValue) nodeType() string    { return "IntValue" }
func (i *IntValue) Pos() Position       { return i.pos }
func (i *IntValue) valueNode()          {}
func (i *IntValue) GoValue() any { return i.V }

type FloatValue struct {
	V   float64
	pos Position
}

func (f *FloatValue) nodeType() string    { return "FloatValue" }
func (f *FloatValue) Pos() Position       { return f.pos }
func (f *FloatValue) valueNode()          {}
func (f *FloatValue) GoValue() any { return f.V }

type BoolValue struct {
	V   bool
	pos Position
}

func (b *BoolValue) nodeType() string    { return "BoolValue" }
func (b *BoolValue) Pos() Position       { return b.pos }
func (b *BoolValue) valueNode()          {}
func (b *BoolValue) GoValue() any { return b.V }

type IPCIDRValue struct {
	V   string
	pos Position
}

func (ip *IPCIDRValue) nodeType() string    { return "IPCIDRValue" }
func (ip *IPCIDRValue) Pos() Position       { return ip.pos }
func (ip *IPCIDRValue) valueNode()          {}
func (ip *IPCIDRValue) GoValue() any { return ip.V }

// =========================================================================
// Parser — direct token-array access in hot loops
// =========================================================================

// Parser holds state for a single parse pass.
type Parser struct {
	tokens []Token
	pos    int
	errors []error
}

// NewParser creates a Parser from a token slice.
func NewParser(tokens []Token) *Parser {
	return &Parser{tokens: tokens}
}

// Parse runs the parser and returns the File AST node and any non-fatal errors.
func (p *Parser) Parse() (*File, []error) {
	file := &File{pos: p.posOf(p.current())}
	for !p.isEOF() {
		p.skipNewlines()
		if p.isEOF() {
			break
		}
		stmt := p.parseTopLevel()
		if stmt == nil {
			continue
		}
		if inc, ok := stmt.(*IncludeDirective); ok {
			file.Includes = append(file.Includes, inc)
		} else {
			file.Statements = append(file.Statements, stmt)
		}
	}
	return file, p.errors
}

func (p *Parser) parseTopLevel() Statement {
	tok := p.current()
	switch tok.Type {
	case TokenInclude:
		return p.parseInclude()
	case TokenIdent:
		return p.parseStatement()
	case TokenNewline:
		p.advance()
		return nil
	default:
		p.errorf("unexpected token at top level: %s", tok)
		p.advance()
		return nil
	}
}

func (p *Parser) parseStatement() Statement {
	pos := p.posOf(p.current())
	path := p.parsePath()
	cur := p.current()

	switch cur.Type {
	case TokenColon:
		return p.parseBlockOrList(path, pos)

	case TokenEquals:
		p.advance()
		val := p.parseValue()
		p.consumeNewline()
		return &Assignment{Path: path, Value: val, pos: pos}

	case TokenIdent:
		name := cur.Literal
		p.advance()
		path = append(path, name)

		extendsTarget := ""
		if p.current().Type == TokenArrow {
			p.advance()
			if p.current().Literal != "extends" {
				p.errorf("expected 'extends' after ->, got %q", p.current().Literal)
			} else {
				p.advance()
				extendsTarget = p.current().Literal
				p.advance()
			}
		}

		if p.current().Type != TokenColon {
			p.errorf("expected ':' to open block, got %s", p.current())
			return nil
		}
		blk := p.parseBlockOrList(path, pos)
		if b, ok := blk.(*Block); ok {
			b.ExtendsTarget = extendsTarget
		}
		return blk

	default:
		p.errorf("unexpected token after path %v: %s", path, cur)
		p.advance()
		return nil
	}
}

func (p *Parser) parsePath() []string {
	var path []string
	if p.current().Type != TokenIdent {
		return path
	}
	path = append(path, p.current().Literal)
	p.advance()
	for p.current().Type == TokenArrow {
		p.advance()
		if p.current().Literal == "extends" {
			break
		}
		if p.current().Type != TokenIdent {
			p.errorf("expected identifier after ->, got %s", p.current())
			break
		}
		path = append(path, p.current().Literal)
		p.advance()
	}
	return path
}

func (p *Parser) parseBlockOrList(path []string, pos Position) Statement {
	p.advance() // consume ':'
	p.consumeNewline()
	p.skipNewlines()
	if p.current().Type != TokenIndent {
		return &Block{Path: path, pos: pos}
	}
	p.advance()
	p.skipNewlines()
	if p.current().Type == TokenDash {
		return p.parseListBody(path, pos)
	}
	return p.parseBlockBody(path, pos)
}

func (p *Parser) parseBlockBody(path []string, pos Position) *Block {
	blk := &Block{Path: path, pos: pos}
	for !p.isEOF() && p.current().Type != TokenDedent {
		p.skipNewlines()
		if p.current().Type == TokenDedent || p.isEOF() {
			break
		}
		stmt := p.parseStatement()
		if stmt != nil {
			blk.Body = append(blk.Body, stmt)
		}
	}
	p.consumeDedent()
	return blk
}

func (p *Parser) parseListBody(path []string, pos Position) *ListBlock {
	lb := &ListBlock{Path: path, pos: pos}
	for !p.isEOF() && p.current().Type != TokenDedent {
		p.skipNewlines()
		if p.current().Type != TokenDash {
			break
		}
		lb.Items = append(lb.Items, p.parseListItem())
	}
	p.consumeDedent()
	return lb
}

func (p *Parser) parseListItem() *ListItem {
	pos := p.posOf(p.current())
	p.advance() // consume '-'
	item := &ListItem{pos: pos}

	isStructured := p.current().Type == TokenIdent && p.peekNext().Type == TokenEquals

	if isStructured {
		for p.current().Type == TokenIdent && p.peekNext().Type == TokenEquals {
			stmtPos := p.posOf(p.current())
			key := p.current().Literal
			p.advance()
			p.advance()
			val := p.parseValue()
			item.Fields = append(item.Fields, &Assignment{Path: []string{key}, Value: val, pos: stmtPos})
		}
		p.consumeNewline()
		if p.current().Type == TokenIndent {
			p.advance()
			for p.current().Type != TokenDedent && !p.isEOF() {
				p.skipNewlines()
				if p.current().Type == TokenDedent || p.isEOF() {
					break
				}
				stmt := p.parseStatement()
				if stmt != nil {
					item.Fields = append(item.Fields, stmt)
				}
			}
			p.consumeDedent()
		}
	} else {
		item.Inline = p.parseValue()
		p.consumeNewline()
	}
	return item
}

func (p *Parser) parseValue() Value {
	tok := p.current()
	pos := p.posOf(tok)
	switch tok.Type {
	case TokenString:
		p.advance()
		return &StringValue{V: tok.Literal, pos: pos}
	case TokenInt:
		p.advance()
		v, err := strconv.ParseInt(tok.Literal, 10, 64)
		if err != nil {
			p.errorf("invalid integer %q: %v", tok.Literal, err)
		}
		return &IntValue{V: v, pos: pos}
	case TokenFloat:
		p.advance()
		v, err := strconv.ParseFloat(tok.Literal, 64)
		if err != nil {
			p.errorf("invalid float %q: %v", tok.Literal, err)
		}
		return &FloatValue{V: v, pos: pos}
	case TokenBool:
		p.advance()
		return &BoolValue{V: tok.Literal == "true", pos: pos}
	case TokenIPCIDR:
		p.advance()
		return &IPCIDRValue{V: tok.Literal, pos: pos}
	case TokenIdent:
		p.advance()
		return &StringValue{V: tok.Literal, pos: pos}
	default:
		p.errorf("expected a value, got %s", tok)
		p.advance()
		return &StringValue{V: "", pos: pos}
	}
}

func (p *Parser) parseInclude() *IncludeDirective {
	pos := p.posOf(p.current())
	p.advance()
	pathTok := p.current()
	if pathTok.Type != TokenString && pathTok.Type != TokenIdent {
		p.errorf("expected file path after include, got %s", pathTok)
		return nil
	}
	p.advance()
	p.consumeNewline()
	return &IncludeDirective{Path: pathTok.Literal, pos: pos}
}

func (p *Parser) current() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *Parser) peekNext() Token {
	if p.pos+1 >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos+1]
}

func (p *Parser) advance() Token {
	tok := p.current()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

// isEOF avoids Token copy by checking the array directly.
func (p *Parser) isEOF() bool {
	return p.pos >= len(p.tokens) || p.tokens[p.pos].Type == TokenEOF
}

// skipNewlines avoids Token copy in tight loop.
func (p *Parser) skipNewlines() {
	for p.pos < len(p.tokens) && p.tokens[p.pos].Type == TokenNewline {
		p.pos++
	}
}

// consumeNewline avoids Token copy.
func (p *Parser) consumeNewline() {
	if p.pos < len(p.tokens) && p.tokens[p.pos].Type == TokenNewline {
		p.pos++
	}
}

// consumeDedent avoids Token copy.
func (p *Parser) consumeDedent() {
	if p.pos < len(p.tokens) && p.tokens[p.pos].Type == TokenDedent {
		p.pos++
	}
}

func (p *Parser) posOf(tok Token) Position { return Position{Line: tok.Line, Col: tok.Col} }

func (p *Parser) errorf(format string, args ...any) {
	p.errors = append(p.errors, fmt.Errorf(format, args...))
}

// =========================================================================
// Evaluator
// =========================================================================

// Evaluator resolves AST → Config, managing includes and extends.
type Evaluator struct {
	baseDir      string
	visitedFiles map[string]bool
	namedBlocks  map[string]*Block
}

// NewEvaluator creates an Evaluator anchored to baseDir for include resolution.
func NewEvaluator(baseDir string) *Evaluator {
	return &Evaluator{
		baseDir:      baseDir,
		visitedFiles: make(map[string]bool),
		namedBlocks:  make(map[string]*Block),
	}
}

// Evaluate evaluates an *File into a Config.
func (e *Evaluator) Evaluate(file *File) (Config, error) {
	result := make(Config)
	for _, inc := range file.Includes {
		cfg, err := e.evaluateInclude(inc.Path)
		if err != nil {
			return nil, fmt.Errorf("include %q: %w", inc.Path, err)
		}
		mergeConfig(result, cfg)
	}
	for _, stmt := range file.Statements {
		if blk, ok := stmt.(*Block); ok && len(blk.Path) >= 2 {
			key := blk.Path[0] + ":" + blk.Path[1]
			e.namedBlocks[key] = blk
		}
	}
	for _, stmt := range file.Statements {
		if err := e.evalStatement(result, stmt); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// EvaluateSource is a convenience wrapper: lex → parse → evaluate.
func (e *Evaluator) EvaluateSource(src string) (Config, error) {
	tokens, err := NewLexer(src).Tokenise()
	if err != nil {
		return nil, fmt.Errorf("lex error: %w", err)
	}
	file, errs := NewParser(tokens).Parse()
	if len(errs) > 0 {
		return nil, fmt.Errorf("parse errors: %v", errs)
	}
	return e.Evaluate(file)
}

func (e *Evaluator) evaluateInclude(path string) (Config, error) {
	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(e.baseDir, path)
	}
	absPath = filepath.Clean(absPath)
	if e.visitedFiles[absPath] {
		return nil, fmt.Errorf("include cycle detected: %q", absPath)
	}
	e.visitedFiles[absPath] = true
	defer delete(e.visitedFiles, absPath)
	src, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	subEval := &Evaluator{
		baseDir:      filepath.Dir(absPath),
		visitedFiles: e.visitedFiles,
		namedBlocks:  e.namedBlocks,
	}
	return subEval.EvaluateSource(string(src))
}

func (e *Evaluator) evalStatement(cfg Config, stmt Statement) error {
	switch s := stmt.(type) {
	case *Block:
		return e.evalBlock(cfg, s)
	case *Assignment:
		return e.evalAssignment(cfg, s)
	case *ListBlock:
		return e.evalListBlock(cfg, s)
	case *IncludeDirective:
		return nil
	default:
		return fmt.Errorf("unknown statement type %T", stmt)
	}
}

func (e *Evaluator) evalBlock(cfg Config, blk *Block) error {
	target := cloneConfig(getNestedConfig(cfg, blk.Path))
	if blk.ExtendsTarget != "" {
		base, err := e.resolveExtends(blk)
		if err != nil {
			return err
		}
		mergeConfig(target, base)
	}
	for _, stmt := range blk.Body {
		if err := e.evalStatement(target, stmt); err != nil {
			return err
		}
	}
	setNested(cfg, blk.Path, target)
	return nil
}

func (e *Evaluator) evalAssignment(cfg Config, a *Assignment) error {
	setNested(cfg, a.Path, a.Value.GoValue())
	return nil
}

func (e *Evaluator) evalListBlock(cfg Config, lb *ListBlock) error {
	items := make([]any, 0, len(lb.Items))
	for _, item := range lb.Items {
		if item.Inline != nil {
			items = append(items, item.Inline.GoValue())
		} else {
			sub := make(Config)
			for _, f := range item.Fields {
				if err := e.evalStatement(sub, f); err != nil {
					return err
				}
			}
			items = append(items, sub)
		}
	}
	setNested(cfg, lb.Path, items)
	return nil
}

func (e *Evaluator) resolveExtends(blk *Block) (Config, error) {
	if len(blk.Path) < 1 {
		return nil, fmt.Errorf("extends: block has empty path")
	}
	kind := blk.Path[0]
	target := blk.ExtendsTarget
	key := kind + ":" + target
	baseBlk, ok := e.namedBlocks[key]
	if !ok {
		return nil, fmt.Errorf("extends: cannot find %q (key %q not registered)", target, key)
	}
	extendingName := blk.Path[len(blk.Path)-1]
	if baseBlk.ExtendsTarget == extendingName {
		return nil, fmt.Errorf("extends cycle detected between %v and %v", blk.Path, baseBlk.Path)
	}
	tempRoot := make(Config)
	if err := e.evalBlock(tempRoot, baseBlk); err != nil {
		return nil, fmt.Errorf("extends: evaluating base %q: %w", target, err)
	}
	return getNestedConfig(tempRoot, baseBlk.Path), nil
}

// --- Config path helpers ---

func getNestedConfig(cfg Config, path []string) Config {
	var cur any = cfg
	for _, seg := range path {
		m, ok := cur.(Config)
		if !ok {
			return make(Config)
		}
		cur = m[seg]
	}
	if c, ok := cur.(Config); ok {
		return c
	}
	return make(Config)
}

// setNested — iterative instead of recursive to avoid stack frames.
func setNested(cfg Config, path []string, value any) {
	if len(path) == 0 {
		return
	}
	for i := 0; i < len(path)-1; i++ {
		seg := path[i]
		if v, ok := cfg[seg]; ok {
			if c, ok := v.(Config); ok {
				cfg = c
				continue
			}
		}
		sub := make(Config)
		cfg[seg] = sub
		cfg = sub
	}
	cfg[path[len(path)-1]] = value
}

func mergeConfig(dst, src Config) {
	for k, sv := range src {
		dv, exists := dst[k]
		if !exists {
			dst[k] = sv
			continue
		}
		dstMap, dstIsMap := dv.(Config)
		srcMap, srcIsMap := sv.(Config)
		if dstIsMap && srcIsMap {
			mergeConfig(dstMap, srcMap)
		} else {
			dst[k] = sv
		}
	}
}

func cloneConfig(src Config) Config {
	dst := make(Config, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// =========================================================================
// Emitter — JSON
// =========================================================================

// ToJSON serialises a Config to pretty-printed JSON bytes.
func ToJSON(cfg Config) ([]byte, error) {
	return json.MarshalIndent(toMap(cfg), "", "  ")
}

func toMap(cfg Config) map[string]any {
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		out[k] = convertValue(v)
	}
	return out
}

func convertValue(v any) any {
	switch tv := v.(type) {
	case Config:
		return toMap(tv)
	case map[string]any:
		return toMap(Config(tv))
	case []any:
		out := make([]any, len(tv))
		for i, item := range tv {
			out[i] = convertValue(item)
		}
		return out
	default:
		return v
	}
}

// =========================================================================
// Emitter — ENV
// =========================================================================

// ToENV serialises a Config to flat KEY=VALUE lines.
func ToENV(cfg Config, prefix string) string {
	var lines []string
	collectENV(cfg, strings.ToUpper(prefix), &lines)
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func collectENV(cfg Config, prefix string, lines *[]string) {
	for k, v := range cfg {
		key := envKey(prefix, k)
		switch tv := v.(type) {
		case Config:
			collectENV(tv, key, lines)
		case map[string]any:
			collectENV(Config(tv), key, lines)
		case []any:
			*lines = append(*lines, key+"="+sliceToEnv(tv))
		default:
			*lines = append(*lines, key+"="+formatENVScalar(tv))
		}
	}
}

func envKey(prefix, key string) string {
	k := strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
	if prefix == "" {
		return k
	}
	return prefix + "_" + k
}

// formatENVScalar avoids fmt.Sprintf for common types.
func formatENVScalar(v any) string {
	switch tv := v.(type) {
	case string:
		return tv
	case int64:
		return strconv.FormatInt(tv, 10)
	case float64:
		return strconv.FormatFloat(tv, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(tv)
	default:
		return fmt.Sprintf("%v", tv)
	}
}

func sliceToEnv(items []any) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		switch tv := item.(type) {
		case Config:
			b, _ := json.Marshal(toMap(tv))
			parts = append(parts, string(b))
		default:
			parts = append(parts, formatENVScalar(tv))
		}
	}
	return strings.Join(parts, ",")
}

// =========================================================================
// Emitter — Canonical Netio DSL
// =========================================================================

// Pre-computed indent strings for common nesting depths.
var indentCache = [...]string{
	"", "  ", "    ", "      ", "        ",
	"          ", "            ", "              ", "                ",
}

func getIndent(depth int) string {
	if depth < len(indentCache) {
		return indentCache[depth]
	}
	return strings.Repeat("  ", depth)
}

// ToNetio re-serialises a Config back into canonical Netio DSL text.
func ToNetio(cfg Config) string {
	var sb strings.Builder
	sb.Grow(256)
	writeConfig(&sb, cfg, 0)
	return sb.String()
}

func writeConfig(sb *strings.Builder, cfg Config, depth int) {
	indent := getIndent(depth)
	keys := sortedKeys(cfg)
	for _, k := range keys {
		v := cfg[k]
		switch tv := v.(type) {
		case Config:
			sb.WriteString(indent)
			sb.WriteString(k)
			sb.WriteString(":\n")
			writeConfig(sb, tv, depth+1)
		case map[string]any:
			sb.WriteString(indent)
			sb.WriteString(k)
			sb.WriteString(":\n")
			writeConfig(sb, Config(tv), depth+1)
		case []any:
			sb.WriteString(indent)
			sb.WriteString(k)
			sb.WriteString(":\n")
			for _, item := range tv {
				writeListItem(sb, item, depth+1)
			}
		default:
			sb.WriteString(indent)
			sb.WriteString(k)
			sb.WriteString(" = ")
			sb.WriteString(formatScalar(v))
			sb.WriteByte('\n')
		}
	}
}

func writeListItem(sb *strings.Builder, item any, depth int) {
	indent := getIndent(depth)
	switch tv := item.(type) {
	case Config:
		sb.WriteString(indent)
		sb.WriteByte('-')
		keys := sortedKeys(tv)
		first := true
		for _, k := range keys {
			v := tv[k]
			switch sv := v.(type) {
			case Config, map[string]any, []any:
				if first {
					sb.WriteByte('\n')
					first = false
				}
				writeConfig(sb, asConfig(sv), depth+1)
			default:
				if first {
					sb.WriteByte(' ')
					sb.WriteString(k)
					sb.WriteString(" = ")
					sb.WriteString(formatScalar(v))
					first = false
				} else {
					sb.WriteByte('\n')
					sb.WriteString(getIndent(depth + 1))
					sb.WriteString(k)
					sb.WriteString(" = ")
					sb.WriteString(formatScalar(v))
				}
			}
		}
		sb.WriteByte('\n')
	default:
		sb.WriteString(indent)
		sb.WriteString("- ")
		sb.WriteString(formatScalar(item))
		sb.WriteByte('\n')
	}
}

// formatScalar uses strconv for numbers — ~5x faster than fmt.Sprintf.
func formatScalar(v any) string {
	switch tv := v.(type) {
	case string:
		if strings.ContainsAny(tv, " \t\"") {
			return strconv.Quote(tv)
		}
		return tv
	case bool:
		return strconv.FormatBool(tv)
	case int64:
		return strconv.FormatInt(tv, 10)
	case float64:
		return strconv.FormatFloat(tv, 'g', -1, 64)
	default:
		return fmt.Sprintf("%v", tv)
	}
}

func asConfig(v any) Config {
	switch tv := v.(type) {
	case Config:
		return tv
	case map[string]any:
		return Config(tv)
	}
	return Config{}
}

// sortedKeys extracts and sorts map keys — shared helper to reduce duplication.
func sortedKeys(m Config) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// =========================================================================
// Schema Validation
// =========================================================================

// ValidationError describes a single schema violation.
type ValidationError struct {
	Path    string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return e.Path + ": " + e.Message
}

// ValidationErrors is a slice of ValidationError that implements error.
type ValidationErrors []*ValidationError

func (ve ValidationErrors) Error() string {
	var sb strings.Builder
	sb.WriteString("validation failed:")
	for _, e := range ve {
		sb.WriteString("\n  - ")
		sb.WriteString(e.Error())
	}
	return sb.String()
}

// HasErrors returns true when there is at least one violation.
func (ve ValidationErrors) HasErrors() bool { return len(ve) > 0 }

func newValidationErr(path, msg string, args ...any) *ValidationError {
	return &ValidationError{Path: path, Message: fmt.Sprintf(msg, args...)}
}

func schemaJoinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

// Validator is the common interface for all schema nodes.
type Validator interface {
	validate(path string, value any) ValidationErrors
}

// --- Schema (root) ---

// Schema is the top-level validator for a whole Config map.
type Schema struct {
	objectValidator
}

// NewSchema returns a new empty Schema.
func NewSchema() *Schema {
	return &Schema{objectValidator{fields: make(map[string]Validator)}}
}

// Validate runs the schema against cfg and returns all violations.
func (s *Schema) Validate(cfg Config) ValidationErrors {
	return s.validate("", cfg)
}

// Required marks top-level keys as mandatory.
func (s *Schema) Required(keys ...string) *Schema {
	s.objectValidator.required = append(s.objectValidator.required, keys...)
	return s
}

// Field attaches a top-level field validator.
func (s *Schema) Field(key string, v Validator) *Schema {
	s.objectValidator.fields[key] = v
	return s
}

// --- objectValidator ---

type objectValidator struct {
	fields   map[string]Validator
	required []string
}

func (o *objectValidator) validate(path string, value any) ValidationErrors {
	var errs ValidationErrors
	var m Config
	switch tv := value.(type) {
	case Config:
		m = tv
	case map[string]any:
		m = Config(tv)
	case nil:
		m = make(Config)
	default:
		return append(errs, newValidationErr(path, "expected an object (Config), got %T", value))
	}
	for _, key := range o.required {
		if _, ok := m[key]; !ok {
			errs = append(errs, newValidationErr(schemaJoinPath(path, key), "required field is missing"))
		}
	}
	for key, v := range o.fields {
		errs = append(errs, v.validate(schemaJoinPath(path, key), m[key])...)
	}
	return errs
}

// --- ObjectValidator ---

// ObjectValidator validates a nested Config block.
type ObjectValidator struct {
	objectValidator
	optional bool
}

// Object returns a new ObjectValidator.
func Object() *ObjectValidator {
	return &ObjectValidator{objectValidator: objectValidator{fields: make(map[string]Validator)}}
}

func (o *ObjectValidator) Required(keys ...string) *ObjectValidator {
	o.objectValidator.required = append(o.objectValidator.required, keys...)
	return o
}
func (o *ObjectValidator) Field(key string, v Validator) *ObjectValidator {
	o.objectValidator.fields[key] = v
	return o
}
func (o *ObjectValidator) Optional() *ObjectValidator { o.optional = true; return o }

func (o *ObjectValidator) validate(path string, value any) ValidationErrors {
	if value == nil && o.optional {
		return nil
	}
	return o.objectValidator.validate(path, value)
}

// --- StringValidator ---

type StringValidator struct {
	minLen, maxLen *int
	oneOf          []string
	nonEmpty       bool
	optional       bool
}

func Str() *StringValidator                              { return &StringValidator{} }
func (sv *StringValidator) NonEmpty() *StringValidator    { sv.nonEmpty = true; return sv }
func (sv *StringValidator) MinLen(n int) *StringValidator { sv.minLen = &n; return sv }
func (sv *StringValidator) MaxLen(n int) *StringValidator { sv.maxLen = &n; return sv }
func (sv *StringValidator) OneOf(values ...string) *StringValidator {
	sv.oneOf = values
	return sv
}
func (sv *StringValidator) Optional() *StringValidator { sv.optional = true; return sv }

func (sv *StringValidator) validate(path string, value any) ValidationErrors {
	var errs ValidationErrors
	if value == nil {
		return nil
	}
	s, ok := value.(string)
	if !ok {
		return append(errs, newValidationErr(path, "expected string, got %T", value))
	}
	if sv.nonEmpty && strings.TrimSpace(s) == "" {
		errs = append(errs, newValidationErr(path, "value must not be empty"))
	}
	if sv.minLen != nil && len(s) < *sv.minLen {
		errs = append(errs, newValidationErr(path, "string length %d is below minimum %d", len(s), *sv.minLen))
	}
	if sv.maxLen != nil && len(s) > *sv.maxLen {
		errs = append(errs, newValidationErr(path, "string length %d exceeds maximum %d", len(s), *sv.maxLen))
	}
	if len(sv.oneOf) > 0 {
		found := false
		for _, allowed := range sv.oneOf {
			if s == allowed {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, newValidationErr(path, "value %q is not one of [%s]", s, strings.Join(sv.oneOf, ", ")))
		}
	}
	return errs
}

// --- IntValidator ---

type IntValidator struct {
	min, max *int64
	optional bool
}

func Int() *IntValidator                             { return &IntValidator{} }
func (iv *IntValidator) Min(n int64) *IntValidator   { iv.min = &n; return iv }
func (iv *IntValidator) Max(n int64) *IntValidator   { iv.max = &n; return iv }
func (iv *IntValidator) Optional() *IntValidator     { iv.optional = true; return iv }

func (iv *IntValidator) validate(path string, value any) ValidationErrors {
	var errs ValidationErrors
	if value == nil {
		return nil
	}
	var n int64
	switch tv := value.(type) {
	case int64:
		n = tv
	case int:
		n = int64(tv)
	case float64:
		n = int64(tv)
	default:
		return append(errs, newValidationErr(path, "expected integer, got %T", value))
	}
	if iv.min != nil && n < *iv.min {
		errs = append(errs, newValidationErr(path, "value %d is below minimum %d", n, *iv.min))
	}
	if iv.max != nil && n > *iv.max {
		errs = append(errs, newValidationErr(path, "value %d exceeds maximum %d", n, *iv.max))
	}
	return errs
}

// --- FloatValidator ---

type FloatValidator struct {
	min, max *float64
	optional bool
}

func Float() *FloatValidator                                 { return &FloatValidator{} }
func (fv *FloatValidator) Min(n float64) *FloatValidator     { fv.min = &n; return fv }
func (fv *FloatValidator) Max(n float64) *FloatValidator     { fv.max = &n; return fv }
func (fv *FloatValidator) Optional() *FloatValidator         { fv.optional = true; return fv }

func (fv *FloatValidator) validate(path string, value any) ValidationErrors {
	var errs ValidationErrors
	if value == nil {
		return nil
	}
	var f float64
	switch tv := value.(type) {
	case float64:
		f = tv
	case int64:
		f = float64(tv)
	default:
		return append(errs, newValidationErr(path, "expected float, got %T", value))
	}
	if fv.min != nil && f < *fv.min {
		errs = append(errs, newValidationErr(path, "value %g is below minimum %g", f, *fv.min))
	}
	if fv.max != nil && f > *fv.max {
		errs = append(errs, newValidationErr(path, "value %g exceeds maximum %g", f, *fv.max))
	}
	return errs
}

// --- BoolValidator ---

type BoolValidator struct{}

func Bool() *BoolValidator { return &BoolValidator{} }

func (bv *BoolValidator) validate(path string, value any) ValidationErrors {
	if value == nil {
		return nil
	}
	if _, ok := value.(bool); !ok {
		return ValidationErrors{newValidationErr(path, "expected bool, got %T", value)}
	}
	return nil
}

// --- ListValidator ---

type ListValidator struct {
	minItems, maxItems *int
	itemV              Validator
	optional           bool
}

func List() *ListValidator                                   { return &ListValidator{} }
func (lv *ListValidator) MinItems(n int) *ListValidator      { lv.minItems = &n; return lv }
func (lv *ListValidator) MaxItems(n int) *ListValidator      { lv.maxItems = &n; return lv }
func (lv *ListValidator) Items(v Validator) *ListValidator   { lv.itemV = v; return lv }
func (lv *ListValidator) Optional() *ListValidator           { lv.optional = true; return lv }

func (lv *ListValidator) validate(path string, value any) ValidationErrors {
	var errs ValidationErrors
	if value == nil {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return append(errs, newValidationErr(path, "expected list, got %T", value))
	}
	if lv.minItems != nil && len(items) < *lv.minItems {
		errs = append(errs, newValidationErr(path, "list has %d items, minimum is %d", len(items), *lv.minItems))
	}
	if lv.maxItems != nil && len(items) > *lv.maxItems {
		errs = append(errs, newValidationErr(path, "list has %d items, maximum is %d", len(items), *lv.maxItems))
	}
	if lv.itemV != nil {
		for i, item := range items {
			errs = append(errs, lv.itemV.validate(fmt.Sprintf("%s[%d]", path, i), item)...)
		}
	}
	return errs
}

// --- AnyValidator ---

type AnyValidator struct{}

func Any() *AnyValidator { return &AnyValidator{} }

func (a *AnyValidator) validate(_ string, _ any) ValidationErrors { return nil }

// =========================================================================
// Linter
// =========================================================================

// Severity classifies how serious a linter finding is.
type Severity int

const (
	SeverityWarning Severity = iota
	SeverityError
)

func (s Severity) String() string {
	if s == SeverityError {
		return "ERROR"
	}
	return "WARN"
}

// Issue is a single linter finding.
type Issue struct {
	Rule     string
	Severity Severity
	Pos      Position
	Message  string
}

func (i Issue) String() string {
	return "[" + i.Severity.String() + "] " + i.Rule + " at " + i.Pos.String() + ": " + i.Message
}

// Issues is a slice of Issue with helpers.
type Issues []Issue

func (is Issues) HasErrors() bool {
	for _, i := range is {
		if i.Severity == SeverityError {
			return true
		}
	}
	return false
}

func (is Issues) String() string {
	var sb strings.Builder
	for i, issue := range is {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(issue.String())
	}
	return sb.String()
}

// LinterConfig controls which rules are active.
type LinterConfig struct {
	CheckDuplicateKeys bool
	CheckEmptyBlocks   bool
	CheckUnsortedKeys  bool
	CheckDeepNesting   bool
	MaxNestingDepth    int
}

// DefaultLinterConfig returns an opinionated default config.
func DefaultLinterConfig() LinterConfig {
	return LinterConfig{
		CheckDuplicateKeys: true,
		CheckEmptyBlocks:   true,
		CheckUnsortedKeys:  true,
		CheckDeepNesting:   true,
		MaxNestingDepth:    6,
	}
}

// LinterObj runs configured rules against an *File.
type LinterObj struct {
	cfg LinterConfig
}

// NewLinter creates a Linter with the given config.
func NewLinter(cfg LinterConfig) *LinterObj { return &LinterObj{cfg: cfg} }

// NewDefaultLinter creates a Linter with DefaultLinterConfig.
func NewDefaultLinter() *LinterObj { return NewLinter(DefaultLinterConfig()) }

// Lint runs all enabled rules and returns every Issue found.
func (l *LinterObj) Lint(file *File) Issues {
	var issues Issues
	for _, stmt := range file.Statements {
		issues = append(issues, l.lintStatement(stmt, 0)...)
	}
	return issues
}

func (l *LinterObj) lintStatement(stmt Statement, depth int) Issues {
	switch s := stmt.(type) {
	case *Block:
		return l.lintBlock(s, depth)
	case *ListBlock:
		return l.lintListBlock(s)
	default:
		return nil
	}
}

func (l *LinterObj) lintBlock(blk *Block, depth int) Issues {
	var issues Issues
	if l.cfg.CheckDeepNesting && depth >= l.cfg.MaxNestingDepth {
		issues = append(issues, Issue{
			Rule: "DeepNesting", Severity: SeverityWarning, Pos: blk.Pos(),
			Message: fmt.Sprintf("block %v is nested %d levels deep (max %d)", blk.Path, depth+1, l.cfg.MaxNestingDepth),
		})
	}
	if l.cfg.CheckEmptyBlocks && len(blk.Body) == 0 {
		issues = append(issues, Issue{
			Rule: "EmptyBlock", Severity: SeverityWarning, Pos: blk.Pos(),
			Message: "block \"" + strings.Join(blk.Path, " -> ") + "\" has no body",
		})
	}
	if len(blk.Body) > 0 {
		if l.cfg.CheckDuplicateKeys {
			issues = append(issues, l.checkDuplicateKeys(blk)...)
		}
		if l.cfg.CheckUnsortedKeys {
			issues = append(issues, l.checkUnsortedKeys(blk)...)
		}
		for _, stmt := range blk.Body {
			issues = append(issues, l.lintStatement(stmt, depth+1)...)
		}
	}
	return issues
}

// stmtKey extracts the key string from a statement for duplicate/sort checks.
func stmtKey(stmt Statement) string {
	switch s := stmt.(type) {
	case *Assignment:
		if len(s.Path) == 1 {
			return s.Path[0]
		}
		return strings.Join(s.Path, "->")
	case *Block:
		if len(s.Path) == 1 {
			return s.Path[0]
		}
		return strings.Join(s.Path, "->")
	case *ListBlock:
		if len(s.Path) == 1 {
			return s.Path[0]
		}
		return strings.Join(s.Path, "->")
	}
	return ""
}

func (l *LinterObj) checkDuplicateKeys(blk *Block) Issues {
	var issues Issues
	seen := make(map[string]bool, len(blk.Body))
	for _, stmt := range blk.Body {
		key := stmtKey(stmt)
		if key == "" {
			continue
		}
		if seen[key] {
			issues = append(issues, Issue{
				Rule: "DuplicateKey", Severity: SeverityError, Pos: stmt.Pos(),
				Message: fmt.Sprintf("key %q is defined more than once in block %v", key, blk.Path),
			})
		}
		seen[key] = true
	}
	return issues
}

func (l *LinterObj) checkUnsortedKeys(blk *Block) Issues {
	var issues Issues
	var keys []string
	keyPos := make(map[string]Position)
	for _, stmt := range blk.Body {
		if a, ok := stmt.(*Assignment); ok {
			var k string
			if len(a.Path) == 1 {
				k = a.Path[0]
			} else {
				k = strings.Join(a.Path, "->")
			}
			keys = append(keys, k)
			keyPos[k] = a.Pos()
		}
	}
	if len(keys) <= 1 {
		return nil
	}
	sorted := make([]string, len(keys))
	copy(sorted, keys)
	sort.Strings(sorted)
	for i, k := range keys {
		if k != sorted[i] {
			issues = append(issues, Issue{
				Rule: "UnsortedKeys", Severity: SeverityWarning, Pos: keyPos[k],
				Message: fmt.Sprintf("key %q is out of canonical order (expected %q at position %d)", k, sorted[i], i+1),
			})
			break
		}
	}
	return issues
}

func (l *LinterObj) lintListBlock(lb *ListBlock) Issues {
	var issues Issues
	if l.cfg.CheckEmptyBlocks && len(lb.Items) == 0 {
		issues = append(issues, Issue{
			Rule: "EmptyBlock", Severity: SeverityWarning, Pos: lb.Pos(),
			Message: "list \"" + strings.Join(lb.Path, " -> ") + "\" has no items",
		})
	}
	return issues
}

// =========================================================================
// Diff — direct value comparison (no fmt.Sprintf allocation)
// =========================================================================

// ChangeKind describes what happened at a path.
type ChangeKind int

const (
	Added    ChangeKind = iota
	Removed
	Modified
)

func (k ChangeKind) String() string {
	switch k {
	case Added:
		return "+"
	case Removed:
		return "-"
	default:
		return "~"
	}
}

// Change represents a single difference at a dot-path.
type Change struct {
	Kind     ChangeKind
	Path     string
	OldValue any
	NewValue any
}

func (c Change) String() string {
	switch c.Kind {
	case Added:
		return "+ " + c.Path + " = " + diffFormatVal(c.NewValue)
	case Removed:
		return "- " + c.Path + " = " + diffFormatVal(c.OldValue)
	default:
		return "~ " + c.Path + ": " + diffFormatVal(c.OldValue) + " → " + diffFormatVal(c.NewValue)
	}
}

// Diff compares two Configs and returns sorted changes.
func Diff(old, new Config) []Change {
	var changes []Change
	diffCompare("", old, new, &changes)
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes
}

func diffCompare(prefix string, old, new Config, out *[]Change) {
	for k, ov := range old {
		path := diffJoinPath(prefix, k)
		nv, exists := new[k]
		if !exists {
			*out = append(*out, Change{Kind: Removed, Path: path, OldValue: ov})
			continue
		}
		diffCompareValues(path, ov, nv, out)
	}
	for k, nv := range new {
		if _, exists := old[k]; !exists {
			*out = append(*out, Change{Kind: Added, Path: diffJoinPath(prefix, k), NewValue: nv})
		}
	}
}

func diffCompareValues(path string, ov, nv any, out *[]Change) {
	oldMap, oldIsMap := asConfigDiff(ov)
	newMap, newIsMap := asConfigDiff(nv)
	switch {
	case oldIsMap && newIsMap:
		diffCompare(path, oldMap, newMap, out)
	case isSlice(ov) && isSlice(nv):
		if !slicesEqual(ov.([]any), nv.([]any)) {
			*out = append(*out, Change{Kind: Modified, Path: path, OldValue: ov, NewValue: nv})
		}
	default:
		if !valuesEqual(ov, nv) {
			*out = append(*out, Change{Kind: Modified, Path: path, OldValue: ov, NewValue: nv})
		}
	}
}

// valuesEqual compares scalars directly — avoids fmt.Sprintf allocation.
func valuesEqual(a, b any) bool {
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case int64:
		bv, ok := b.(int64)
		return ok && av == bv
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	default:
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
}

func asConfigDiff(v any) (Config, bool) {
	switch tv := v.(type) {
	case Config:
		return tv, true
	case map[string]any:
		return Config(tv), true
	}
	return nil, false
}

func isSlice(v any) bool {
	_, ok := v.([]any)
	return ok
}

func slicesEqual(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		aMap, aOk := asConfigDiff(a[i])
		bMap, bOk := asConfigDiff(b[i])
		if aOk && bOk {
			if len(aMap) != len(bMap) {
				return false
			}
			for k, av := range aMap {
				bv, ok := bMap[k]
				if !ok || !valuesEqual(av, bv) {
					return false
				}
			}
			continue
		}
		if !valuesEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func diffJoinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

// diffFormatVal uses strconv for common types — avoids fmt.Sprintf.
func diffFormatVal(v any) string {
	if v == nil {
		return "<nil>"
	}
	switch tv := v.(type) {
	case string:
		return strconv.Quote(tv)
	case int64:
		return strconv.FormatInt(tv, 10)
	case float64:
		return strconv.FormatFloat(tv, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(tv)
	case []any:
		parts := make([]string, len(tv))
		for i, item := range tv {
			parts[i] = diffFormatVal(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case Config, map[string]any:
		return "{...}"
	default:
		return fmt.Sprintf("%v", tv)
	}
}

// DiffRender formats a change list as a human-readable string.
func DiffRender(changes []Change) string {
	if len(changes) == 0 {
		return "(no differences)"
	}
	var sb strings.Builder
	for i, c := range changes {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(c.String())
	}
	return sb.String()
}

// DiffSummary returns a short summary like "2 added, 1 modified".
func DiffSummary(changes []Change) string {
	var added, removed, modified int
	for i := range changes {
		switch changes[i].Kind {
		case Added:
			added++
		case Removed:
			removed++
		case Modified:
			modified++
		}
	}
	var parts []string
	if added > 0 {
		parts = append(parts, strconv.Itoa(added)+" added")
	}
	if removed > 0 {
		parts = append(parts, strconv.Itoa(removed)+" removed")
	}
	if modified > 0 {
		parts = append(parts, strconv.Itoa(modified)+" modified")
	}
	if len(parts) == 0 {
		return "no differences"
	}
	return strings.Join(parts, ", ")
}

// =========================================================================
// Top-level convenience functions
// =========================================================================

// Tokenise returns the raw token stream for the given source.
func Tokenise(src string) ([]Token, error) {
	return NewLexer(src).Tokenise()
}

// ParseAST returns the *File for the given source without evaluating it.
func ParseAST(src string) (*File, []error) {
	tokens, err := NewLexer(src).Tokenise()
	if err != nil {
		return nil, []error{err}
	}
	return NewParser(tokens).Parse()
}

// ParseString lexes, parses, and evaluates Netio DSL source text.
func ParseString(src string, baseDir ...string) (Config, error) {
	bd := "."
	if len(baseDir) > 0 && baseDir[0] != "" {
		bd = baseDir[0]
	}
	return NewEvaluator(bd).EvaluateSource(src)
}

// ParseFile reads, lexes, parses, and evaluates a Netio DSL file.
func ParseFile(path string) (Config, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	return NewEvaluator(filepath.Dir(absPath)).EvaluateSource(string(src))
}

// LintFile parses a file and runs the default linter rules.
func LintFile(path string) (Issues, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LintString(string(src))
}

// LintString parses source text and runs the default linter.
func LintString(src string) (Issues, error) {
	file, errs := ParseAST(src)
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return NewDefaultLinter().Lint(file), nil
}

// LintAST runs the linter against a pre-parsed *File.
func LintAST(file *File, cfg LinterConfig) Issues {
	return NewLinter(cfg).Lint(file)
}
