package sqlite

import (
	"fmt"
	"strings"
)

// tokenType classifies a lexical token.
type tokenType int

const (
	tokEOF tokenType = iota
	tokIdent
	tokKeyword
	tokNumber // integer or real literal
	tokString // 'text' literal
	tokBlob   // x'...' literal
	tokParam  // ? placeholder
	tokPunct  // operators and punctuation
)

// token is a single lexical unit.
type token struct {
	typ tokenType
	// text is the normalized lexeme. For keywords it is upper-cased; for string
	// literals it is the unquoted content.
	text string
	pos  int
}

// keywords recognized by the lexer. Anything not here is treated as an
// identifier.
var keywords = map[string]bool{
	"CREATE": true, "TABLE": true, "PRIMARY": true, "KEY": true, "NOT": true,
	"NULL": true, "INSERT": true, "INTO": true, "VALUES": true, "SELECT": true,
	"FROM": true, "WHERE": true, "AND": true, "OR": true, "IN": true, "IS": true,
	"LIKE": true, "ORDER": true, "BY": true, "ASC": true, "DESC": true,
	"LIMIT": true, "OFFSET": true, "GROUP": true, "HAVING": true, "AS": true,
	"COUNT": true, "SUM": true, "AVG": true, "MIN": true, "MAX": true,
	"UPDATE": true, "SET": true, "DELETE": true, "INNER": true, "JOIN": true,
	"ON": true, "INTEGER": true, "TEXT": true, "REAL": true, "BLOB": true,
	"INT": true, "BEGIN": true, "COMMIT": true, "ROLLBACK": true,
	"TRANSACTION": true, "DISTINCT": true, "TRUE": true, "FALSE": true,
	"IF": true, "EXISTS": true, "DROP": true, "COLLATE": true, "GLOB": true,
}

// lexer turns SQL text into a slice of tokens.
type lexer struct {
	src string
	pos int
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isIdent(c byte) bool  { return isLetter(c) || isDigit(c) }

// tokenize lexes the entire input, returning the token stream or an error on an
// illegal character or unterminated literal.
func tokenize(src string) ([]token, error) {
	l := &lexer{src: src}
	var toks []token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		if t.typ == tokEOF {
			return toks, nil
		}
	}
}

func (l *lexer) next() (token, error) {
	l.skipSpaceAndComments()
	if l.pos >= len(l.src) {
		return token{typ: tokEOF, pos: l.pos}, nil
	}
	start := l.pos
	c := l.src[l.pos]

	switch {
	case c == '?':
		l.pos++
		return token{typ: tokParam, text: "?", pos: start}, nil
	case c == '\'':
		return l.lexString()
	case (c == 'x' || c == 'X') && l.pos+1 < len(l.src) && l.src[l.pos+1] == '\'':
		return l.lexBlob()
	case c == '"':
		return l.lexQuotedIdent()
	case isLetter(c):
		return l.lexIdent()
	case isDigit(c) || (c == '.' && l.pos+1 < len(l.src) && isDigit(l.src[l.pos+1])):
		return l.lexNumber()
	default:
		return l.lexPunct()
	}
}

func (l *lexer) skipSpaceAndComments() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			l.pos++
		case c == '-' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '-':
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
		case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*':
			l.pos += 2
			for l.pos+1 < len(l.src) && (l.src[l.pos] != '*' || l.src[l.pos+1] != '/') {
				l.pos++
			}
			l.pos += 2
			if l.pos > len(l.src) {
				l.pos = len(l.src)
			}
		default:
			return
		}
	}
}

func (l *lexer) lexIdent() (token, error) {
	start := l.pos
	for l.pos < len(l.src) && isIdent(l.src[l.pos]) {
		l.pos++
	}
	word := l.src[start:l.pos]
	up := strings.ToUpper(word)
	if keywords[up] {
		return token{typ: tokKeyword, text: up, pos: start}, nil
	}
	return token{typ: tokIdent, text: word, pos: start}, nil
}

func (l *lexer) lexQuotedIdent() (token, error) {
	start := l.pos
	l.pos++ // opening quote
	var sb strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '"' {
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '"' {
				sb.WriteByte('"')
				l.pos += 2
				continue
			}
			l.pos++
			return token{typ: tokIdent, text: sb.String(), pos: start}, nil
		}
		sb.WriteByte(c)
		l.pos++
	}
	return token{}, fmt.Errorf("sqlite: unterminated quoted identifier at %d", start)
}

func (l *lexer) lexNumber() (token, error) {
	start := l.pos
	for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
		l.pos++
	}
	if l.pos < len(l.src) && l.src[l.pos] == '.' {
		l.pos++
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			l.pos++
		}
	}
	if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
		l.pos++
		if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
			l.pos++
		}
		for l.pos < len(l.src) && isDigit(l.src[l.pos]) {
			l.pos++
		}
	}
	return token{typ: tokNumber, text: l.src[start:l.pos], pos: start}, nil
}

func (l *lexer) lexString() (token, error) {
	start := l.pos
	l.pos++ // opening quote
	var sb strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\'' {
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '\'' {
				sb.WriteByte('\'')
				l.pos += 2
				continue
			}
			l.pos++
			return token{typ: tokString, text: sb.String(), pos: start}, nil
		}
		sb.WriteByte(c)
		l.pos++
	}
	return token{}, fmt.Errorf("sqlite: unterminated string literal at %d", start)
}

func (l *lexer) lexBlob() (token, error) {
	start := l.pos
	l.pos += 2 // x'
	valStart := l.pos
	for l.pos < len(l.src) && l.src[l.pos] != '\'' {
		l.pos++
	}
	if l.pos >= len(l.src) {
		return token{}, fmt.Errorf("sqlite: unterminated blob literal at %d", start)
	}
	hex := l.src[valStart:l.pos]
	l.pos++ // closing quote
	if len(hex)%2 != 0 {
		return token{}, fmt.Errorf("sqlite: odd-length blob literal at %d", start)
	}
	return token{typ: tokBlob, text: hex, pos: start}, nil
}

// multiCharOps lists the two-character operators.
var multiCharOps = []string{"<=", ">=", "<>", "!=", "||"}

func (l *lexer) lexPunct() (token, error) {
	start := l.pos
	if l.pos+1 < len(l.src) {
		two := l.src[l.pos : l.pos+2]
		for _, op := range multiCharOps {
			if two == op {
				l.pos += 2
				return token{typ: tokPunct, text: two, pos: start}, nil
			}
		}
	}
	c := l.src[l.pos]
	switch c {
	case '(', ')', ',', '*', '=', '<', '>', '+', '-', '/', '%', '.', ';':
		l.pos++
		return token{typ: tokPunct, text: string(c), pos: start}, nil
	default:
		return token{}, fmt.Errorf("sqlite: unexpected character %q at %d", string(c), start)
	}
}
