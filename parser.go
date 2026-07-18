package sqlite

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// parser is a hand-written recursive-descent parser over a token stream.
type parser struct {
	toks   []token
	pos    int
	nParam int // count of ? placeholders seen, used to assign indices
}

// Parse parses a single SQL statement. A trailing semicolon is permitted. It is
// exported so callers can inspect the AST directly, though most use the
// database/sql driver instead.
func Parse(sql string) (Statement, error) {
	toks, err := tokenize(sql)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	stmt, err := p.parseStatement()
	if err != nil {
		return nil, err
	}
	if p.peek().typ == tokPunct && p.peek().text == ";" {
		p.advance()
	}
	if p.peek().typ != tokEOF {
		return nil, fmt.Errorf("sqlite: unexpected trailing token %q", p.peek().text)
	}
	return stmt, nil
}

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) advance() token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

// isKw reports whether the current token is the given keyword.
func (p *parser) isKw(kw string) bool {
	t := p.peek()
	return t.typ == tokKeyword && t.text == kw
}

// isPunct reports whether the current token is the given punctuation.
func (p *parser) isPunct(s string) bool {
	t := p.peek()
	return t.typ == tokPunct && t.text == s
}

func (p *parser) expectKw(kw string) error {
	if !p.isKw(kw) {
		return fmt.Errorf("sqlite: expected %q, got %q", kw, p.peek().text)
	}
	p.advance()
	return nil
}

func (p *parser) expectPunct(s string) error {
	if !p.isPunct(s) {
		return fmt.Errorf("sqlite: expected %q, got %q", s, p.peek().text)
	}
	p.advance()
	return nil
}

// parseName reads an identifier or a non-reserved keyword usable as a name.
func (p *parser) parseName() (string, error) {
	t := p.peek()
	if t.typ == tokIdent {
		p.advance()
		return t.text, nil
	}
	return "", fmt.Errorf("sqlite: expected identifier, got %q", t.text)
}

func (p *parser) parseStatement() (Statement, error) {
	t := p.peek()
	if t.typ != tokKeyword {
		return nil, fmt.Errorf("sqlite: expected statement keyword, got %q", t.text)
	}
	switch t.text {
	case "CREATE":
		return p.parseCreateTable()
	case "DROP":
		return p.parseDropTable()
	case "INSERT":
		return p.parseInsert()
	case "SELECT":
		return p.parseSelect()
	case "UPDATE":
		return p.parseUpdate()
	case "DELETE":
		return p.parseDelete()
	case "BEGIN":
		p.advance()
		if p.isKw("TRANSACTION") {
			p.advance()
		}
		return &BeginStmt{}, nil
	case "COMMIT":
		p.advance()
		if p.isKw("TRANSACTION") {
			p.advance()
		}
		return &CommitStmt{}, nil
	case "ROLLBACK":
		p.advance()
		if p.isKw("TRANSACTION") {
			p.advance()
		}
		return &RollbackStmt{}, nil
	default:
		return nil, fmt.Errorf("sqlite: unsupported statement %q", t.text)
	}
}

func (p *parser) parseCreateTable() (Statement, error) {
	if err := p.expectKw("CREATE"); err != nil {
		return nil, err
	}
	if err := p.expectKw("TABLE"); err != nil {
		return nil, err
	}
	st := &CreateTableStmt{}
	if p.isKw("IF") {
		p.advance()
		if err := p.expectKw("NOT"); err != nil {
			return nil, err
		}
		if err := p.expectKw("EXISTS"); err != nil {
			return nil, err
		}
		st.IfNotExists = true
	}
	name, err := p.parseName()
	if err != nil {
		return nil, err
	}
	st.Table = name
	if err := p.expectPunct("("); err != nil {
		return nil, err
	}
	for {
		col, err := p.parseColumnDef()
		if err != nil {
			return nil, err
		}
		st.Columns = append(st.Columns, col)
		if p.isPunct(",") {
			p.advance()
			continue
		}
		break
	}
	if err := p.expectPunct(")"); err != nil {
		return nil, err
	}
	if len(st.Columns) == 0 {
		return nil, fmt.Errorf("sqlite: table must have at least one column")
	}
	return st, nil
}

func (p *parser) parseColumnDef() (ColumnDef, error) {
	var col ColumnDef
	name, err := p.parseName()
	if err != nil {
		return col, err
	}
	col.Name = name
	col.Type = TypeNull // no declared type => flexible
	// Optional type keyword.
	switch {
	case p.isKw("INTEGER") || p.isKw("INT"):
		col.Type = TypeInteger
		p.advance()
	case p.isKw("TEXT"):
		col.Type = TypeText
		p.advance()
	case p.isKw("REAL"):
		col.Type = TypeReal
		p.advance()
	case p.isKw("BLOB"):
		col.Type = TypeBlob
		p.advance()
	}
	// Column constraints.
	for {
		switch {
		case p.isKw("PRIMARY"):
			p.advance()
			if err := p.expectKw("KEY"); err != nil {
				return col, err
			}
			col.PrimaryKey = true
			col.NotNull = true
		case p.isKw("NOT"):
			p.advance()
			if err := p.expectKw("NULL"); err != nil {
				return col, err
			}
			col.NotNull = true
		default:
			return col, nil
		}
	}
}

func (p *parser) parseDropTable() (Statement, error) {
	if err := p.expectKw("DROP"); err != nil {
		return nil, err
	}
	if err := p.expectKw("TABLE"); err != nil {
		return nil, err
	}
	st := &DropTableStmt{}
	if p.isKw("IF") {
		p.advance()
		if err := p.expectKw("EXISTS"); err != nil {
			return nil, err
		}
		st.IfExists = true
	}
	name, err := p.parseName()
	if err != nil {
		return nil, err
	}
	st.Table = name
	return st, nil
}

func (p *parser) parseInsert() (Statement, error) {
	if err := p.expectKw("INSERT"); err != nil {
		return nil, err
	}
	if err := p.expectKw("INTO"); err != nil {
		return nil, err
	}
	st := &InsertStmt{}
	name, err := p.parseName()
	if err != nil {
		return nil, err
	}
	st.Table = name
	if p.isPunct("(") {
		p.advance()
		for {
			cn, err := p.parseName()
			if err != nil {
				return nil, err
			}
			st.Columns = append(st.Columns, cn)
			if p.isPunct(",") {
				p.advance()
				continue
			}
			break
		}
		if err := p.expectPunct(")"); err != nil {
			return nil, err
		}
	}
	if err := p.expectKw("VALUES"); err != nil {
		return nil, err
	}
	for {
		if err := p.expectPunct("("); err != nil {
			return nil, err
		}
		var row []Expr
		for {
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			row = append(row, e)
			if p.isPunct(",") {
				p.advance()
				continue
			}
			break
		}
		if err := p.expectPunct(")"); err != nil {
			return nil, err
		}
		st.Rows = append(st.Rows, row)
		if p.isPunct(",") {
			p.advance()
			continue
		}
		break
	}
	return st, nil
}

func (p *parser) parseSelect() (Statement, error) {
	if err := p.expectKw("SELECT"); err != nil {
		return nil, err
	}
	st := &SelectStmt{}
	if p.isKw("DISTINCT") {
		p.advance()
		st.Distinct = true
	}
	// Result columns.
	for {
		rc, err := p.parseResultColumn()
		if err != nil {
			return nil, err
		}
		st.Columns = append(st.Columns, rc)
		if p.isPunct(",") {
			p.advance()
			continue
		}
		break
	}
	// FROM (optional, to allow SELECT of pure expressions).
	if p.isKw("FROM") {
		p.advance()
		name, err := p.parseName()
		if err != nil {
			return nil, err
		}
		st.From = name
		st.FromAlias = p.parseOptAlias()
		if p.isKw("INNER") || p.isKw("JOIN") {
			jc, err := p.parseJoin()
			if err != nil {
				return nil, err
			}
			st.Join = jc
		}
	}
	if p.isKw("WHERE") {
		p.advance()
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		st.Where = e
	}
	if p.isKw("GROUP") {
		p.advance()
		if err := p.expectKw("BY"); err != nil {
			return nil, err
		}
		for {
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			st.GroupBy = append(st.GroupBy, e)
			if p.isPunct(",") {
				p.advance()
				continue
			}
			break
		}
		if p.isKw("HAVING") {
			p.advance()
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			st.Having = e
		}
	}
	if p.isKw("ORDER") {
		p.advance()
		if err := p.expectKw("BY"); err != nil {
			return nil, err
		}
		for {
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			ot := OrderTerm{Expr: e}
			if p.isKw("ASC") {
				p.advance()
			} else if p.isKw("DESC") {
				p.advance()
				ot.Desc = true
			}
			st.OrderBy = append(st.OrderBy, ot)
			if p.isPunct(",") {
				p.advance()
				continue
			}
			break
		}
	}
	if p.isKw("LIMIT") {
		p.advance()
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		st.Limit = e
		if p.isKw("OFFSET") {
			p.advance()
			o, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			st.Offset = o
		} else if p.isPunct(",") {
			// LIMIT offset, count  (SQLite compatibility)
			p.advance()
			cnt, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			st.Offset = st.Limit
			st.Limit = cnt
		}
	}
	return st, nil
}

func (p *parser) parseOptAlias() string {
	if p.isKw("AS") {
		p.advance()
		if n, err := p.parseName(); err == nil {
			return n
		}
		return ""
	}
	if p.peek().typ == tokIdent {
		return p.advance().text
	}
	return ""
}

func (p *parser) parseJoin() (*JoinClause, error) {
	if p.isKw("INNER") {
		p.advance()
	}
	if err := p.expectKw("JOIN"); err != nil {
		return nil, err
	}
	jc := &JoinClause{}
	name, err := p.parseName()
	if err != nil {
		return nil, err
	}
	jc.Table = name
	jc.Alias = p.parseOptAlias()
	if err := p.expectKw("ON"); err != nil {
		return nil, err
	}
	on, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	jc.On = on
	return jc, nil
}

func (p *parser) parseResultColumn() (ResultColumn, error) {
	var rc ResultColumn
	if p.isPunct("*") {
		p.advance()
		rc.Star = true
		return rc, nil
	}
	// Look ahead for "table.*"
	if p.peek().typ == tokIdent {
		save := p.pos
		tbl := p.peek().text
		p.advance()
		if p.isPunct(".") {
			p.advance()
			if p.isPunct("*") {
				p.advance()
				rc.Star = true
				rc.Table = tbl
				return rc, nil
			}
		}
		p.pos = save
	}
	e, err := p.parseExpr()
	if err != nil {
		return rc, err
	}
	rc.Expr = e
	if p.isKw("AS") {
		p.advance()
		n, err := p.parseName()
		if err != nil {
			return rc, err
		}
		rc.Alias = n
	} else if p.peek().typ == tokIdent {
		rc.Alias = p.advance().text
	}
	return rc, nil
}

func (p *parser) parseUpdate() (Statement, error) {
	if err := p.expectKw("UPDATE"); err != nil {
		return nil, err
	}
	st := &UpdateStmt{}
	name, err := p.parseName()
	if err != nil {
		return nil, err
	}
	st.Table = name
	if err := p.expectKw("SET"); err != nil {
		return nil, err
	}
	for {
		cn, err := p.parseName()
		if err != nil {
			return nil, err
		}
		if err := p.expectPunct("="); err != nil {
			return nil, err
		}
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		st.Cols = append(st.Cols, cn)
		st.Vals = append(st.Vals, val)
		if p.isPunct(",") {
			p.advance()
			continue
		}
		break
	}
	if p.isKw("WHERE") {
		p.advance()
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		st.Where = e
	}
	return st, nil
}

func (p *parser) parseDelete() (Statement, error) {
	if err := p.expectKw("DELETE"); err != nil {
		return nil, err
	}
	if err := p.expectKw("FROM"); err != nil {
		return nil, err
	}
	st := &DeleteStmt{}
	name, err := p.parseName()
	if err != nil {
		return nil, err
	}
	st.Table = name
	if p.isKw("WHERE") {
		p.advance()
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		st.Where = e
	}
	return st, nil
}

// --- Expression parsing (precedence climbing) ---

func (p *parser) parseExpr() (Expr, error) { return p.parseOr() }

func (p *parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.isKw("OR") {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "OR", Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Expr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.isKw("AND") {
		p.advance()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpr{Op: "AND", Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseNot() (Expr, error) {
	if p.isKw("NOT") {
		p.advance()
		e, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: "NOT", Expr: e}, nil
	}
	return p.parsePredicate()
}

// parsePredicate handles comparison, IS NULL, IN, LIKE which bind tighter than
// NOT/AND/OR but looser than arithmetic.
func (p *parser) parsePredicate() (Expr, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.isKw("IS"):
			p.advance()
			not := false
			if p.isKw("NOT") {
				p.advance()
				not = true
			}
			if err := p.expectKw("NULL"); err != nil {
				return nil, err
			}
			left = &IsNullExpr{Expr: left, Not: not}
		case p.isKw("IN"):
			p.advance()
			ie, err := p.parseInTail(left, false)
			if err != nil {
				return nil, err
			}
			left = ie
		case p.isKw("LIKE") || p.isKw("GLOB"):
			p.advance()
			pat, err := p.parseComparison()
			if err != nil {
				return nil, err
			}
			left = &LikeExpr{Expr: left, Pattern: pat}
		case p.isKw("NOT"):
			// NOT IN / NOT LIKE
			save := p.pos
			p.advance()
			switch {
			case p.isKw("IN"):
				p.advance()
				ie, err := p.parseInTail(left, true)
				if err != nil {
					return nil, err
				}
				left = ie
			case p.isKw("LIKE") || p.isKw("GLOB"):
				p.advance()
				pat, err := p.parseComparison()
				if err != nil {
					return nil, err
				}
				left = &LikeExpr{Expr: left, Pattern: pat, Not: true}
			default:
				p.pos = save
				return left, nil
			}
		default:
			return left, nil
		}
	}
}

func (p *parser) parseInTail(left Expr, not bool) (Expr, error) {
	if err := p.expectPunct("("); err != nil {
		return nil, err
	}
	ie := &InExpr{Expr: left, Not: not}
	if !p.isPunct(")") {
		for {
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			ie.List = append(ie.List, e)
			if p.isPunct(",") {
				p.advance()
				continue
			}
			break
		}
	}
	if err := p.expectPunct(")"); err != nil {
		return nil, err
	}
	return ie, nil
}

func (p *parser) parseComparison() (Expr, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.typ == tokPunct {
			switch t.text {
			case "=", "<", ">", "<=", ">=", "<>", "!=":
				p.advance()
				right, err := p.parseAdditive()
				if err != nil {
					return nil, err
				}
				op := t.text
				if op == "!=" {
					op = "<>"
				}
				left = &BinaryExpr{Op: op, Left: left, Right: right}
				continue
			}
		}
		return left, nil
	}
}

func (p *parser) parseAdditive() (Expr, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.typ == tokPunct && (t.text == "+" || t.text == "-" || t.text == "||") {
			p.advance()
			right, err := p.parseMultiplicative()
			if err != nil {
				return nil, err
			}
			left = &BinaryExpr{Op: t.text, Left: left, Right: right}
			continue
		}
		return left, nil
	}
}

func (p *parser) parseMultiplicative() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.typ == tokPunct && (t.text == "*" || t.text == "/" || t.text == "%") {
			p.advance()
			right, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			left = &BinaryExpr{Op: t.text, Left: left, Right: right}
			continue
		}
		return left, nil
	}
}

func (p *parser) parseUnary() (Expr, error) {
	t := p.peek()
	if t.typ == tokPunct && (t.text == "-" || t.text == "+") {
		p.advance()
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryExpr{Op: t.text, Expr: e}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Expr, error) {
	t := p.peek()
	switch t.typ {
	case tokNumber:
		p.advance()
		return numberLiteral(t.text)
	case tokString:
		p.advance()
		return &Literal{Val: Text(t.text)}, nil
	case tokBlob:
		p.advance()
		raw, err := hex.DecodeString(t.text)
		if err != nil {
			return nil, fmt.Errorf("sqlite: invalid blob literal: %w", err)
		}
		return &Literal{Val: Blob(raw)}, nil
	case tokParam:
		p.advance()
		idx := p.nParam
		p.nParam++
		return &Param{Index: idx}, nil
	case tokKeyword:
		switch t.text {
		case "NULL":
			p.advance()
			return &Literal{Val: Null()}, nil
		case "TRUE":
			p.advance()
			return &Literal{Val: Int(1)}, nil
		case "FALSE":
			p.advance()
			return &Literal{Val: Int(0)}, nil
		case "COUNT", "SUM", "AVG", "MIN", "MAX":
			return p.parseFuncCall(t.text)
		}
		return nil, fmt.Errorf("sqlite: unexpected keyword %q in expression", t.text)
	case tokIdent:
		// Could be a column reference (optionally qualified) or a function call.
		name := t.text
		p.advance()
		if p.isPunct("(") {
			return p.parseFuncCallBody(strings.ToUpper(name))
		}
		if p.isPunct(".") {
			p.advance()
			col, err := p.parseName()
			if err != nil {
				return nil, err
			}
			return &ColumnRef{Table: name, Name: col}, nil
		}
		return &ColumnRef{Name: name}, nil
	case tokPunct:
		if t.text == "(" {
			p.advance()
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expectPunct(")"); err != nil {
				return nil, err
			}
			return e, nil
		}
	}
	return nil, fmt.Errorf("sqlite: unexpected token %q in expression", t.text)
}

func (p *parser) parseFuncCall(name string) (Expr, error) {
	p.advance() // consume the function keyword
	return p.parseFuncCallBody(name)
}

func (p *parser) parseFuncCallBody(name string) (Expr, error) {
	if err := p.expectPunct("("); err != nil {
		return nil, err
	}
	fe := &FuncExpr{Name: name}
	if p.isPunct("*") {
		p.advance()
		fe.Star = true
		if err := p.expectPunct(")"); err != nil {
			return nil, err
		}
		return fe, nil
	}
	if p.isKw("DISTINCT") {
		p.advance()
		fe.Distinct = true
	}
	if !p.isPunct(")") {
		for {
			e, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			fe.Args = append(fe.Args, e)
			if p.isPunct(",") {
				p.advance()
				continue
			}
			break
		}
	}
	if err := p.expectPunct(")"); err != nil {
		return nil, err
	}
	return fe, nil
}

func numberLiteral(text string) (Expr, error) {
	if strings.ContainsAny(text, ".eE") {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			// A magnitude too large to represent overflows to ±Inf, which
			// SQLite accepts as a valid floating-point literal (rendered by
			// quote() as ±9.0e+999). Any other error is a genuine syntax error.
			if errors.Is(err, strconv.ErrRange) {
				return &Literal{Val: Real(f)}, nil
			}
			return nil, fmt.Errorf("sqlite: invalid number %q: %w", text, err)
		}
		return &Literal{Val: Real(f)}, nil
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		// Fall back to float for out-of-range integers (including overflow to
		// ±Inf, matching SQLite's handling of oversized numeric literals).
		f, ferr := strconv.ParseFloat(text, 64)
		if ferr != nil && !errors.Is(ferr, strconv.ErrRange) {
			return nil, fmt.Errorf("sqlite: invalid number %q: %w", text, err)
		}
		return &Literal{Val: Real(f)}, nil
	}
	return &Literal{Val: Int(n)}, nil
}
