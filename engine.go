package sqlite

import (
	"fmt"
	"sort"
	"strings"
)

// ExecResult reports the outcome of a statement that does not return rows.
type ExecResult struct {
	RowsAffected int64
	LastInsertID int64
}

// ResultSet is the materialized output of a query.
type ResultSet struct {
	Columns []string
	Rows    [][]Value
}

// evalTable is one table's contribution to a row environment during evaluation.
type evalTable struct {
	name  string
	alias string
	cols  []column
	vals  []Value
}

// evalRow is the environment against which an expression is evaluated: the
// current row from each table in scope, plus placeholder arguments.
type evalRow struct {
	tables []evalTable
	args   []Value
}

func (e *evalRow) resolve(ref *ColumnRef) (Value, error) {
	var found *Value
	for ti := range e.tables {
		t := &e.tables[ti]
		if ref.Table != "" {
			if !strings.EqualFold(ref.Table, t.name) && !strings.EqualFold(ref.Table, t.alias) {
				continue
			}
		}
		for ci, c := range t.cols {
			if strings.EqualFold(c.Name, ref.Name) {
				v := t.vals[ci]
				if found != nil {
					return Null(), fmt.Errorf("sqlite: ambiguous column name %q", ref.Name)
				}
				vv := v
				found = &vv
			}
		}
	}
	if found == nil {
		q := ref.Name
		if ref.Table != "" {
			q = ref.Table + "." + ref.Name
		}
		return Null(), fmt.Errorf("sqlite: no such column: %s", q)
	}
	return *found, nil
}

// eval evaluates an expression to a Value in the given environment. Aggregates
// are not valid here and produce an error.
func eval(expr Expr, env *evalRow) (Value, error) {
	switch e := expr.(type) {
	case *Literal:
		return e.Val, nil
	case *Param:
		if e.Index < 0 || e.Index >= len(env.args) {
			return Null(), fmt.Errorf("sqlite: missing argument for placeholder %d", e.Index+1)
		}
		return env.args[e.Index], nil
	case *ColumnRef:
		return env.resolve(e)
	case *UnaryExpr:
		return evalUnary(e, env)
	case *BinaryExpr:
		return evalBinary(e, env)
	case *IsNullExpr:
		v, err := eval(e.Expr, env)
		if err != nil {
			return Null(), err
		}
		isNull := v.IsNull()
		if e.Not {
			isNull = !isNull
		}
		return boolVal(isNull), nil
	case *InExpr:
		return evalIn(e, env)
	case *LikeExpr:
		return evalLike(e, env)
	case *FuncExpr:
		if isAggregateName(e.Name) {
			return Null(), fmt.Errorf("sqlite: aggregate %s() not allowed here", e.Name)
		}
		fn, ok := LookupScalar(e.Name)
		if !ok {
			return Null(), fmt.Errorf("sqlite: unknown function %s()", e.Name)
		}
		argv := make([]Value, len(e.Args))
		for i, a := range e.Args {
			av, err := eval(a, env)
			if err != nil {
				return Null(), err
			}
			argv[i] = av
		}
		return fn(argv)
	default:
		return Null(), fmt.Errorf("sqlite: cannot evaluate expression %T", expr)
	}
}

func boolVal(b bool) Value {
	if b {
		return Int(1)
	}
	return Int(0)
}

func evalUnary(e *UnaryExpr, env *evalRow) (Value, error) {
	v, err := eval(e.Expr, env)
	if err != nil {
		return Null(), err
	}
	switch e.Op {
	case "NOT":
		if v.IsNull() {
			return Null(), nil
		}
		return boolVal(!v.truthy()), nil
	case "-":
		if v.IsNull() {
			return Null(), nil
		}
		if v.Type == TypeInteger {
			return Int(-v.i), nil
		}
		return Real(-v.asFloat()), nil
	case "+":
		return v, nil
	default:
		return Null(), fmt.Errorf("sqlite: unknown unary operator %q", e.Op)
	}
}

func evalBinary(e *BinaryExpr, env *evalRow) (Value, error) {
	switch e.Op {
	case "AND", "OR":
		return evalLogical(e, env)
	}
	l, err := eval(e.Left, env)
	if err != nil {
		return Null(), err
	}
	r, err := eval(e.Right, env)
	if err != nil {
		return Null(), err
	}
	switch e.Op {
	case "=", "<>", "<", ">", "<=", ">=":
		if l.IsNull() || r.IsNull() {
			return Null(), nil
		}
		c := compare(l, r)
		switch e.Op {
		case "=":
			return boolVal(c == 0), nil
		case "<>":
			return boolVal(c != 0), nil
		case "<":
			return boolVal(c < 0), nil
		case ">":
			return boolVal(c > 0), nil
		case "<=":
			return boolVal(c <= 0), nil
		case ">=":
			return boolVal(c >= 0), nil
		}
	case "+", "-", "*", "/", "%":
		return evalArith(e.Op, l, r)
	case "||":
		if l.IsNull() || r.IsNull() {
			return Null(), nil
		}
		return Text(l.String() + r.String()), nil
	}
	return Null(), fmt.Errorf("sqlite: unknown operator %q", e.Op)
}

func evalLogical(e *BinaryExpr, env *evalRow) (Value, error) {
	l, err := eval(e.Left, env)
	if err != nil {
		return Null(), err
	}
	r, err := eval(e.Right, env)
	if err != nil {
		return Null(), err
	}
	lNull, rNull := l.IsNull(), r.IsNull()
	if e.Op == "AND" {
		if (!lNull && !l.truthy()) || (!rNull && !r.truthy()) {
			return boolVal(false), nil
		}
		if lNull || rNull {
			return Null(), nil
		}
		return boolVal(true), nil
	}
	// OR
	if (!lNull && l.truthy()) || (!rNull && r.truthy()) {
		return boolVal(true), nil
	}
	if lNull || rNull {
		return Null(), nil
	}
	return boolVal(false), nil
}

func evalArith(op string, l, r Value) (Value, error) {
	if l.IsNull() || r.IsNull() {
		return Null(), nil
	}
	if !l.isNumeric() || !r.isNumeric() {
		// Coerce text that looks numeric; otherwise treat as 0 like SQLite does
		// for arithmetic on text is complex — we require numeric operands.
		l = coerceToType(l, TypeReal)
		r = coerceToType(r, TypeReal)
		if !l.isNumeric() || !r.isNumeric() {
			return Null(), nil
		}
	}
	bothInt := l.Type == TypeInteger && r.Type == TypeInteger
	switch op {
	case "+":
		return numericAdd(l, r), nil
	case "-":
		if bothInt {
			return Int(l.i - r.i), nil
		}
		return Real(l.asFloat() - r.asFloat()), nil
	case "*":
		if bothInt {
			return Int(l.i * r.i), nil
		}
		return Real(l.asFloat() * r.asFloat()), nil
	case "/":
		if r.asFloat() == 0 {
			return Null(), nil
		}
		if bothInt {
			return Int(l.i / r.i), nil
		}
		return Real(l.asFloat() / r.asFloat()), nil
	case "%":
		if bothInt {
			if r.i == 0 {
				return Null(), nil
			}
			return Int(l.i % r.i), nil
		}
		return Null(), fmt.Errorf("sqlite: modulo requires integer operands")
	}
	return Null(), fmt.Errorf("sqlite: unknown arithmetic operator %q", op)
}

func evalIn(e *InExpr, env *evalRow) (Value, error) {
	v, err := eval(e.Expr, env)
	if err != nil {
		return Null(), err
	}
	if v.IsNull() {
		return Null(), nil
	}
	sawNull := false
	for _, item := range e.List {
		iv, err := eval(item, env)
		if err != nil {
			return Null(), err
		}
		if iv.IsNull() {
			sawNull = true
			continue
		}
		if compare(v, iv) == 0 {
			return boolVal(!e.Not), nil
		}
	}
	if sawNull {
		return Null(), nil
	}
	return boolVal(e.Not), nil
}

func evalLike(e *LikeExpr, env *evalRow) (Value, error) {
	v, err := eval(e.Expr, env)
	if err != nil {
		return Null(), err
	}
	pat, err := eval(e.Pattern, env)
	if err != nil {
		return Null(), err
	}
	if v.IsNull() || pat.IsNull() {
		return Null(), nil
	}
	m := likeMatch(pat.String(), v.String())
	if e.Not {
		m = !m
	}
	return boolVal(m), nil
}

// likeMatch implements SQL LIKE with '%' (any sequence) and '_' (any single
// character). Matching is case-insensitive for ASCII, matching SQLite's default.
func likeMatch(pattern, s string) bool {
	p := []rune(strings.ToLower(pattern))
	str := []rune(strings.ToLower(s))
	return likeHelper(p, str)
}

func likeHelper(p, s []rune) bool {
	// Iterative wildcard matching with backtracking.
	var pi, si int
	star := -1
	var sBacktrack int
	for si < len(s) {
		if pi < len(p) && (p[pi] == '_' || p[pi] == s[si]) {
			pi++
			si++
		} else if pi < len(p) && p[pi] == '%' {
			star = pi
			sBacktrack = si
			pi++
		} else if star != -1 {
			pi = star + 1
			sBacktrack++
			si = sBacktrack
		} else {
			return false
		}
	}
	for pi < len(p) && p[pi] == '%' {
		pi++
	}
	return pi == len(p)
}

// passesWhere reports whether a WHERE/HAVING predicate value admits the row.
// NULL (unknown) is treated as false.
func passesWhere(v Value) bool { return v.truthy() }

// --- statement execution ---

// execStatement runs a parsed statement against the database (lock held by the
// caller). Exactly one of the return values is meaningful depending on the
// statement kind; queries return a ResultSet, everything else an ExecResult.
func (db *Database) execStatement(stmt Statement, args []Value) (*ExecResult, *ResultSet, error) {
	switch s := stmt.(type) {
	case *CreateTableStmt:
		r, err := db.execCreate(s)
		return r, nil, err
	case *DropTableStmt:
		r, err := db.execDrop(s)
		return r, nil, err
	case *InsertStmt:
		r, err := db.execInsert(s, args)
		return r, nil, err
	case *SelectStmt:
		rs, err := db.execSelect(s, args)
		return nil, rs, err
	case *UpdateStmt:
		r, err := db.execUpdate(s, args)
		return r, nil, err
	case *DeleteStmt:
		r, err := db.execDelete(s, args)
		return r, nil, err
	default:
		return nil, nil, fmt.Errorf("sqlite: statement %T cannot be executed here", stmt)
	}
}

func (db *Database) execCreate(s *CreateTableStmt) (*ExecResult, error) {
	if _, ok := db.getTable(s.Table); ok {
		if s.IfNotExists {
			return &ExecResult{}, nil
		}
		return nil, fmt.Errorf("sqlite: table %s already exists", s.Table)
	}
	// Validate no duplicate column names.
	seen := map[string]bool{}
	pkCount := 0
	for _, c := range s.Columns {
		if seen[strings.ToLower(c.Name)] {
			return nil, fmt.Errorf("sqlite: duplicate column name: %s", c.Name)
		}
		seen[strings.ToLower(c.Name)] = true
		if c.PrimaryKey {
			pkCount++
		}
	}
	if pkCount > 1 {
		return nil, fmt.Errorf("sqlite: table %s has more than one primary key", s.Table)
	}
	db.tables[strings.ToLower(s.Table)] = newTable(s.Table, s.Columns)
	return &ExecResult{}, nil
}

func (db *Database) execDrop(s *DropTableStmt) (*ExecResult, error) {
	if _, ok := db.getTable(s.Table); !ok {
		if s.IfExists {
			return &ExecResult{}, nil
		}
		return nil, fmt.Errorf("sqlite: no such table: %s", s.Table)
	}
	delete(db.tables, strings.ToLower(s.Table))
	return &ExecResult{}, nil
}

func (db *Database) execInsert(s *InsertStmt, args []Value) (*ExecResult, error) {
	t, ok := db.getTable(s.Table)
	if !ok {
		return nil, fmt.Errorf("sqlite: no such table: %s", s.Table)
	}
	// Determine target column positions.
	var targets []int
	if len(s.Columns) == 0 {
		targets = make([]int, len(t.cols))
		for i := range t.cols {
			targets[i] = i
		}
	} else {
		for _, cn := range s.Columns {
			idx, ok := t.columnIndex(cn)
			if !ok {
				return nil, fmt.Errorf("sqlite: table %s has no column named %s", s.Table, cn)
			}
			targets = append(targets, idx)
		}
	}
	res := &ExecResult{}
	for _, exprRow := range s.Rows {
		if len(exprRow) != len(targets) {
			return nil, fmt.Errorf("sqlite: %d values for %d columns", len(exprRow), len(targets))
		}
		row := make([]Value, len(t.cols))
		for i := range row {
			row[i] = Null()
		}
		env := &evalRow{args: args}
		for i, expr := range exprRow {
			v, err := eval(expr, env)
			if err != nil {
				return nil, err
			}
			ci := targets[i]
			row[ci] = coerceToType(v, t.cols[ci].Type)
		}
		if err := checkConstraints(t, row); err != nil {
			return nil, err
		}
		id, err := t.insertRow(row)
		if err != nil {
			return nil, err
		}
		res.LastInsertID = id
		res.RowsAffected++
	}
	return res, nil
}

func checkConstraints(t *table, row []Value) error {
	for i, c := range t.cols {
		if c.NotNull && row[i].IsNull() {
			return fmt.Errorf("sqlite: NOT NULL constraint failed: %s.%s", t.name, c.Name)
		}
	}
	return nil
}

func (db *Database) execUpdate(s *UpdateStmt, args []Value) (*ExecResult, error) {
	t, ok := db.getTable(s.Table)
	if !ok {
		return nil, fmt.Errorf("sqlite: no such table: %s", s.Table)
	}
	setIdx := make([]int, len(s.Cols))
	for i, cn := range s.Cols {
		idx, ok := t.columnIndex(cn)
		if !ok {
			return nil, fmt.Errorf("sqlite: table %s has no column named %s", s.Table, cn)
		}
		setIdx[i] = idx
	}
	res := &ExecResult{}
	for _, id := range append([]int64(nil), t.order...) {
		row := t.rows[id]
		env := &evalRow{
			tables: []evalTable{{name: t.name, cols: t.cols, vals: row}},
			args:   args,
		}
		if s.Where != nil {
			wv, err := eval(s.Where, env)
			if err != nil {
				return nil, err
			}
			if !passesWhere(wv) {
				continue
			}
		}
		newRow := make([]Value, len(row))
		copy(newRow, row)
		for i, expr := range s.Vals {
			v, err := eval(expr, env)
			if err != nil {
				return nil, err
			}
			ci := setIdx[i]
			newRow[ci] = coerceToType(v, t.cols[ci].Type)
		}
		if err := checkConstraints(t, newRow); err != nil {
			return nil, err
		}
		// Enforce PK uniqueness against other rows.
		if t.pk >= 0 {
			for _, other := range t.order {
				if other == id {
					continue
				}
				if equalStrict(t.rows[other][t.pk], newRow[t.pk]) {
					return nil, fmt.Errorf("sqlite: UNIQUE constraint failed: %s.%s", t.name, t.cols[t.pk].Name)
				}
			}
		}
		t.rows[id] = newRow
		res.RowsAffected++
	}
	return res, nil
}

func (db *Database) execDelete(s *DeleteStmt, args []Value) (*ExecResult, error) {
	t, ok := db.getTable(s.Table)
	if !ok {
		return nil, fmt.Errorf("sqlite: no such table: %s", s.Table)
	}
	res := &ExecResult{}
	for _, id := range append([]int64(nil), t.order...) {
		row := t.rows[id]
		if s.Where != nil {
			env := &evalRow{
				tables: []evalTable{{name: t.name, cols: t.cols, vals: row}},
				args:   args,
			}
			wv, err := eval(s.Where, env)
			if err != nil {
				return nil, err
			}
			if !passesWhere(wv) {
				continue
			}
		}
		t.deleteRow(id)
		res.RowsAffected++
	}
	return res, nil
}

// intFromExpr evaluates a constant expression (LIMIT/OFFSET) to an int.
func intFromExpr(expr Expr, args []Value) (int, error) {
	if expr == nil {
		return -1, nil
	}
	v, err := eval(expr, &evalRow{args: args})
	if err != nil {
		return 0, err
	}
	if v.IsNull() {
		return -1, nil
	}
	v = coerceToType(v, TypeInteger)
	if v.Type != TypeInteger {
		return 0, fmt.Errorf("sqlite: LIMIT/OFFSET must be an integer")
	}
	return int(v.i), nil
}

// sortRows performs a stable multi-key sort of output records by the ORDER BY
// terms. Order keys were precomputed during projection.
func sortRows(records []outRecord, terms []OrderTerm) {
	sort.SliceStable(records, func(i, j int) bool {
		for ti, term := range terms {
			c := compareNullable(records[i].orderKeys[ti], records[j].orderKeys[ti])
			if c == 0 {
				continue
			}
			if term.Desc {
				return c > 0
			}
			return c < 0
		}
		return false
	})
}

// compareNullable orders values placing NULL first (SQLite ascending default).
func compareNullable(a, b Value) int {
	if a.IsNull() || b.IsNull() {
		switch {
		case a.IsNull() && b.IsNull():
			return 0
		case a.IsNull():
			return -1
		default:
			return 1
		}
	}
	return compare(a, b)
}
