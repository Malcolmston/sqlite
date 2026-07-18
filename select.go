package sqlite

import (
	"fmt"
	"strings"
)

// outRecord is one row on its way through the SELECT pipeline. out holds the
// projected output values; orderKeys holds the pre-computed ORDER BY key values.
type outRecord struct {
	out       []Value
	orderKeys []Value
}

// execSelect runs a SELECT statement and materializes its result set.
func (db *Database) execSelect(s *SelectStmt, args []Value) (*ResultSet, error) {
	// Build the source rows as a list of environments.
	envs, srcTables, err := db.scanSource(s, args)
	if err != nil {
		return nil, err
	}

	// Apply WHERE.
	if s.Where != nil {
		filtered := envs[:0:0]
		for _, env := range envs {
			v, err := eval(s.Where, env)
			if err != nil {
				return nil, err
			}
			if passesWhere(v) {
				filtered = append(filtered, env)
			}
		}
		envs = filtered
	}

	// Expand the projection into concrete output columns.
	proj, err := expandProjection(s.Columns, srcTables)
	if err != nil {
		return nil, err
	}
	colNames := make([]string, len(proj))
	for i, pc := range proj {
		colNames[i] = pc.name
	}

	aggregate := len(s.GroupBy) > 0 || projectionHasAggregate(proj) || (s.Having != nil)

	var records []outRecord
	if aggregate {
		records, err = db.evalGrouped(s, proj, envs, args)
	} else {
		records, err = db.evalSimple(s, proj, envs, args)
	}
	if err != nil {
		return nil, err
	}

	// DISTINCT.
	if s.Distinct {
		records = distinctRecords(records)
	}

	// ORDER BY (keys were precomputed during evaluation).
	if len(s.OrderBy) > 0 {
		sortRows(records, s.OrderBy)
	}

	// LIMIT / OFFSET.
	limit, err := intFromExpr(s.Limit, args)
	if err != nil {
		return nil, err
	}
	offset, err := intFromExpr(s.Offset, args)
	if err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}

	rs := &ResultSet{Columns: colNames}
	for i, rec := range records {
		if i < offset {
			continue
		}
		if limit >= 0 && len(rs.Rows) >= limit {
			break
		}
		rs.Rows = append(rs.Rows, rec.out)
	}
	if rs.Rows == nil {
		rs.Rows = [][]Value{}
	}
	return rs, nil
}

// srcTable describes a table (with alias) participating in the FROM clause.
type srcTable struct {
	name  string
	alias string
	cols  []column
}

// scanSource produces the cartesian/join environments for the FROM clause.
func (db *Database) scanSource(s *SelectStmt, args []Value) ([]*evalRow, []srcTable, error) {
	if s.From == "" {
		// No FROM: a single empty environment (SELECT of constant expressions).
		return []*evalRow{{args: args}}, nil, nil
	}
	t, ok := db.getTable(s.From)
	if !ok {
		return nil, nil, fmt.Errorf("sqlite: no such table: %s", s.From)
	}
	srcs := []srcTable{{name: t.name, alias: s.FromAlias, cols: t.cols}}

	var joinTable *table
	if s.Join != nil {
		jt, ok := db.getTable(s.Join.Table)
		if !ok {
			return nil, nil, fmt.Errorf("sqlite: no such table: %s", s.Join.Table)
		}
		joinTable = jt
		srcs = append(srcs, srcTable{name: jt.name, alias: s.Join.Alias, cols: jt.cols})
	}

	var envs []*evalRow
	for _, id := range t.order {
		leftVals := t.rows[id]
		if joinTable == nil {
			envs = append(envs, &evalRow{
				tables: []evalTable{{name: t.name, alias: s.FromAlias, cols: t.cols, vals: leftVals}},
				args:   args,
			})
			continue
		}
		for _, jid := range joinTable.order {
			rightVals := joinTable.rows[jid]
			env := &evalRow{
				tables: []evalTable{
					{name: t.name, alias: s.FromAlias, cols: t.cols, vals: leftVals},
					{name: joinTable.name, alias: s.Join.Alias, cols: joinTable.cols, vals: rightVals},
				},
				args: args,
			}
			ok, err := eval(s.Join.On, env)
			if err != nil {
				return nil, nil, err
			}
			if passesWhere(ok) {
				envs = append(envs, env)
			}
		}
	}
	return envs, srcs, nil
}

// projColumn is one expanded output column.
type projColumn struct {
	name string
	expr Expr // nil only for a "*" that was expanded into concrete refs
}

// expandProjection turns the result-column list (including * and t.*) into a
// flat list of named expressions.
func expandProjection(cols []ResultColumn, srcs []srcTable) ([]projColumn, error) {
	var out []projColumn
	for _, rc := range cols {
		if rc.Star {
			if len(srcs) == 0 {
				return nil, fmt.Errorf("sqlite: no tables to expand * ")
			}
			for _, st := range srcs {
				if rc.Table != "" && !strings.EqualFold(rc.Table, st.name) && !strings.EqualFold(rc.Table, st.alias) {
					continue
				}
				for _, c := range st.cols {
					out = append(out, projColumn{
						name: c.Name,
						expr: &ColumnRef{Table: tableRef(st), Name: c.Name},
					})
				}
			}
			continue
		}
		name := rc.Alias
		if name == "" {
			name = deriveName(rc.Expr)
		}
		out = append(out, projColumn{name: name, expr: rc.Expr})
	}
	return out, nil
}

func tableRef(st srcTable) string {
	if st.alias != "" {
		return st.alias
	}
	return st.name
}

func deriveName(expr Expr) string {
	switch e := expr.(type) {
	case *ColumnRef:
		return e.Name
	case *FuncExpr:
		if e.Star {
			return e.Name + "(*)"
		}
		var parts []string
		for _, a := range e.Args {
			parts = append(parts, deriveName(a))
		}
		return e.Name + "(" + strings.Join(parts, ",") + ")"
	case *Literal:
		return e.Val.String()
	default:
		return "expr"
	}
}

func projectionHasAggregate(proj []projColumn) bool {
	for _, pc := range proj {
		if containsAggregate(pc.expr) {
			return true
		}
	}
	return false
}

func containsAggregate(expr Expr) bool {
	switch e := expr.(type) {
	case *FuncExpr:
		if isAggregateName(e.Name) {
			return true
		}
		for _, a := range e.Args {
			if containsAggregate(a) {
				return true
			}
		}
		return false
	case *UnaryExpr:
		return containsAggregate(e.Expr)
	case *BinaryExpr:
		return containsAggregate(e.Left) || containsAggregate(e.Right)
	case *IsNullExpr:
		return containsAggregate(e.Expr)
	case *InExpr:
		if containsAggregate(e.Expr) {
			return true
		}
		for _, it := range e.List {
			if containsAggregate(it) {
				return true
			}
		}
	case *LikeExpr:
		return containsAggregate(e.Expr) || containsAggregate(e.Pattern)
	}
	return false
}

func isAggregateName(name string) bool {
	switch name {
	case "COUNT", "SUM", "AVG", "MIN", "MAX":
		return true
	default:
		return false
	}
}

// evalSimple handles a non-aggregate projection over each source environment.
func (db *Database) evalSimple(s *SelectStmt, proj []projColumn, envs []*evalRow, args []Value) ([]outRecord, error) {
	records := make([]outRecord, 0, len(envs))
	for _, env := range envs {
		out := make([]Value, len(proj))
		for i, pc := range proj {
			v, err := eval(pc.expr, env)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		keys, err := orderKeysFor(s.OrderBy, proj, out, func(e Expr) (Value, error) { return eval(e, env) })
		if err != nil {
			return nil, err
		}
		records = append(records, outRecord{out: out, orderKeys: keys})
	}
	return records, nil
}

// evalGrouped handles GROUP BY and/or aggregate projections.
func (db *Database) evalGrouped(s *SelectStmt, proj []projColumn, envs []*evalRow, args []Value) ([]outRecord, error) {
	type group struct {
		key  []Value
		rows []*evalRow
	}
	var groups []*group

	if len(s.GroupBy) == 0 {
		// Single implicit group over all rows (may be empty).
		groups = append(groups, &group{rows: envs})
	} else {
		index := map[string]*group{}
		for _, env := range envs {
			key := make([]Value, len(s.GroupBy))
			for i, ge := range s.GroupBy {
				v, err := eval(ge, env)
				if err != nil {
					return nil, err
				}
				key[i] = v
			}
			gk := groupKeyString(key)
			g, ok := index[gk]
			if !ok {
				g = &group{key: key}
				index[gk] = g
				groups = append(groups, g)
			}
			g.rows = append(g.rows, env)
		}
	}

	records := make([]outRecord, 0, len(groups))
	for _, g := range groups {
		evalFn := func(e Expr) (Value, error) { return evalAgg(e, g.rows, args) }
		if s.Having != nil {
			hv, err := evalFn(s.Having)
			if err != nil {
				return nil, err
			}
			if !passesWhere(hv) {
				continue
			}
		}
		out := make([]Value, len(proj))
		for i, pc := range proj {
			v, err := evalFn(pc.expr)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		keys, err := orderKeysFor(s.OrderBy, proj, out, evalFn)
		if err != nil {
			return nil, err
		}
		records = append(records, outRecord{out: out, orderKeys: keys})
	}
	return records, nil
}

func groupKeyString(key []Value) string {
	var sb strings.Builder
	for _, v := range key {
		sb.WriteByte(byte(v.Type) + 1)
		sb.WriteString(v.String())
		sb.WriteByte(0)
	}
	return sb.String()
}

// orderKeysFor computes ORDER BY key values for a record, resolving output-alias
// references before falling back to the supplied expression evaluator.
func orderKeysFor(terms []OrderTerm, proj []projColumn, out []Value, evalFn func(Expr) (Value, error)) ([]Value, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	keys := make([]Value, len(terms))
	for i, term := range terms {
		if cr, ok := term.Expr.(*ColumnRef); ok && cr.Table == "" {
			matched := false
			for j, pc := range proj {
				if strings.EqualFold(pc.name, cr.Name) {
					keys[i] = out[j]
					matched = true
					break
				}
			}
			if matched {
				continue
			}
		}
		v, err := evalFn(term.Expr)
		if err != nil {
			return nil, err
		}
		keys[i] = v
	}
	return keys, nil
}

// evalAgg evaluates an expression that may contain aggregate functions over the
// rows of a group. Bare column references use the first row of the group.
func evalAgg(expr Expr, rows []*evalRow, args []Value) (Value, error) {
	switch e := expr.(type) {
	case *FuncExpr:
		if isAggregateName(e.Name) {
			return computeAggregate(e, rows)
		}
		fn, ok := LookupScalar(e.Name)
		if !ok {
			return Null(), fmt.Errorf("sqlite: unknown function %s()", e.Name)
		}
		argv := make([]Value, len(e.Args))
		for i, a := range e.Args {
			av, err := evalAgg(a, rows, args)
			if err != nil {
				return Null(), err
			}
			argv[i] = av
		}
		return fn(argv)
	case *Literal:
		return e.Val, nil
	case *Param:
		if e.Index < 0 || e.Index >= len(args) {
			return Null(), fmt.Errorf("sqlite: missing argument for placeholder %d", e.Index+1)
		}
		return args[e.Index], nil
	case *ColumnRef:
		if len(rows) == 0 {
			return Null(), nil
		}
		return rows[0].resolve(e)
	case *UnaryExpr:
		v, err := evalAgg(e.Expr, rows, args)
		if err != nil {
			return Null(), err
		}
		return applyUnary(e.Op, v)
	case *BinaryExpr:
		return evalAggBinary(e, rows, args)
	case *IsNullExpr:
		v, err := evalAgg(e.Expr, rows, args)
		if err != nil {
			return Null(), err
		}
		isNull := v.IsNull()
		if e.Not {
			isNull = !isNull
		}
		return boolVal(isNull), nil
	case *InExpr:
		return evalAggIn(e, rows, args)
	case *LikeExpr:
		v, err := evalAgg(e.Expr, rows, args)
		if err != nil {
			return Null(), err
		}
		pat, err := evalAgg(e.Pattern, rows, args)
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
	default:
		return Null(), fmt.Errorf("sqlite: cannot evaluate expression %T", expr)
	}
}

func applyUnary(op string, v Value) (Value, error) {
	switch op {
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
			return Int(-v.Int64()), nil
		}
		return Real(-v.asFloat()), nil
	case "+":
		return v, nil
	}
	return Null(), fmt.Errorf("sqlite: unknown unary operator %q", op)
}

func evalAggBinary(e *BinaryExpr, rows []*evalRow, args []Value) (Value, error) {
	if e.Op == "AND" || e.Op == "OR" {
		l, err := evalAgg(e.Left, rows, args)
		if err != nil {
			return Null(), err
		}
		r, err := evalAgg(e.Right, rows, args)
		if err != nil {
			return Null(), err
		}
		return combineLogical(e.Op, l, r), nil
	}
	l, err := evalAgg(e.Left, rows, args)
	if err != nil {
		return Null(), err
	}
	r, err := evalAgg(e.Right, rows, args)
	if err != nil {
		return Null(), err
	}
	switch e.Op {
	case "=", "<>", "<", ">", "<=", ">=":
		if l.IsNull() || r.IsNull() {
			return Null(), nil
		}
		c := compare(l, r)
		return boolVal(compareOp(e.Op, c)), nil
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

func compareOp(op string, c int) bool {
	switch op {
	case "=":
		return c == 0
	case "<>":
		return c != 0
	case "<":
		return c < 0
	case ">":
		return c > 0
	case "<=":
		return c <= 0
	case ">=":
		return c >= 0
	}
	return false
}

func combineLogical(op string, l, r Value) Value {
	lNull, rNull := l.IsNull(), r.IsNull()
	if op == "AND" {
		if (!lNull && !l.truthy()) || (!rNull && !r.truthy()) {
			return boolVal(false)
		}
		if lNull || rNull {
			return Null()
		}
		return boolVal(true)
	}
	if (!lNull && l.truthy()) || (!rNull && r.truthy()) {
		return boolVal(true)
	}
	if lNull || rNull {
		return Null()
	}
	return boolVal(false)
}

func evalAggIn(e *InExpr, rows []*evalRow, args []Value) (Value, error) {
	v, err := evalAgg(e.Expr, rows, args)
	if err != nil {
		return Null(), err
	}
	if v.IsNull() {
		return Null(), nil
	}
	sawNull := false
	for _, item := range e.List {
		iv, err := evalAgg(item, rows, args)
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

// computeAggregate evaluates COUNT/SUM/AVG/MIN/MAX over a group's rows.
func computeAggregate(fn *FuncExpr, rows []*evalRow) (Value, error) {
	switch fn.Name {
	case "COUNT":
		if fn.Star {
			return Int(int64(len(rows))), nil
		}
		if len(fn.Args) != 1 {
			return Null(), fmt.Errorf("sqlite: COUNT expects one argument")
		}
		var n int64
		seen := map[string]bool{}
		for _, env := range rows {
			v, err := eval(fn.Args[0], env)
			if err != nil {
				return Null(), err
			}
			if v.IsNull() {
				continue
			}
			if fn.Distinct {
				k := aggKey(v)
				if seen[k] {
					continue
				}
				seen[k] = true
			}
			n++
		}
		return Int(n), nil
	case "SUM", "AVG":
		if len(fn.Args) != 1 {
			return Null(), fmt.Errorf("sqlite: %s expects one argument", fn.Name)
		}
		var sum float64
		var count int64
		allInt := true
		var intSum int64
		seen := map[string]bool{}
		for _, env := range rows {
			v, err := eval(fn.Args[0], env)
			if err != nil {
				return Null(), err
			}
			if v.IsNull() || !v.isNumeric() {
				if v.IsNull() {
					continue
				}
			}
			if fn.Distinct {
				k := aggKey(v)
				if seen[k] {
					continue
				}
				seen[k] = true
			}
			if v.Type != TypeInteger {
				allInt = false
			}
			intSum += v.Int64()
			sum += v.asFloat()
			count++
		}
		if count == 0 {
			return Null(), nil // SUM/AVG of no rows is NULL
		}
		if fn.Name == "AVG" {
			return Real(sum / float64(count)), nil
		}
		if allInt {
			return Int(intSum), nil
		}
		return Real(sum), nil
	case "MIN", "MAX":
		if len(fn.Args) != 1 {
			return Null(), fmt.Errorf("sqlite: %s expects one argument", fn.Name)
		}
		var best Value
		have := false
		for _, env := range rows {
			v, err := eval(fn.Args[0], env)
			if err != nil {
				return Null(), err
			}
			if v.IsNull() {
				continue
			}
			if !have {
				best = v
				have = true
				continue
			}
			c := compare(v, best)
			if (fn.Name == "MIN" && c < 0) || (fn.Name == "MAX" && c > 0) {
				best = v
			}
		}
		if !have {
			return Null(), nil
		}
		return best, nil
	default:
		return Null(), fmt.Errorf("sqlite: unknown aggregate %s", fn.Name)
	}
}

func aggKey(v Value) string {
	return string(rune(int(v.Type)+1)) + v.String()
}

func distinctRecords(records []outRecord) []outRecord {
	var out []outRecord
	seen := map[string]bool{}
	for _, rec := range records {
		var sb strings.Builder
		for _, v := range rec.out {
			sb.WriteByte(byte(v.Type) + 1)
			sb.WriteString(v.String())
			sb.WriteByte(0)
		}
		k := sb.String()
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, rec)
	}
	return out
}
