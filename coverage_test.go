package sqlite

import (
	"testing"
	"time"
)

func TestValueBasics(t *testing.T) {
	cases := []struct {
		v    Value
		str  string
		gv   interface{}
		null bool
	}{
		{Null(), "NULL", nil, true},
		{Int(42), "42", int64(42), false},
		{Real(3.5), "3.5", float64(3.5), false},
		{Text("hi"), "hi", "hi", false},
		{Blob([]byte("ab")), "ab", []byte("ab"), false},
	}
	for _, c := range cases {
		if c.v.String() != c.str {
			t.Errorf("String()=%q want %q", c.v.String(), c.str)
		}
		if c.v.IsNull() != c.null {
			t.Errorf("IsNull mismatch for %v", c.v)
		}
	}
	if Int(7).Int64() != 7 || Real(1.5).Float64() != 1.5 || Text("x").Str() != "x" || string(Blob([]byte("z")).Bytes()) != "z" {
		t.Fatal("accessor mismatch")
	}
	if TypeInteger.String() != "INTEGER" || TypeNull.String() != "NULL" || TypeReal.String() != "REAL" ||
		TypeText.String() != "TEXT" || TypeBlob.String() != "BLOB" || ValueType(99).String() != "UNKNOWN" {
		t.Fatal("ValueType.String mismatch")
	}
}

func TestValueFromGo(t *testing.T) {
	ts := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	cases := []struct {
		in   interface{}
		want ValueType
	}{
		{nil, TypeNull},
		{true, TypeInteger},
		{false, TypeInteger},
		{int(1), TypeInteger},
		{int8(1), TypeInteger},
		{int16(1), TypeInteger},
		{int32(1), TypeInteger},
		{int64(1), TypeInteger},
		{uint(1), TypeInteger},
		{uint8(1), TypeInteger},
		{uint16(1), TypeInteger},
		{uint32(1), TypeInteger},
		{uint64(1), TypeInteger},
		{float32(1), TypeReal},
		{float64(1), TypeReal},
		{"s", TypeText},
		{[]byte("b"), TypeBlob},
		{ts, TypeText},
	}
	for _, c := range cases {
		v, err := valueFromGo(c.in)
		if err != nil {
			t.Fatalf("valueFromGo(%v): %v", c.in, err)
		}
		if v.Type != c.want {
			t.Errorf("valueFromGo(%T)=%v want %v", c.in, v.Type, c.want)
		}
	}
	if _, err := valueFromGo(struct{}{}); err == nil {
		t.Fatal("expected error for unsupported type")
	}
	if v, _ := valueFromGo(true); v.Int64() != 1 {
		t.Fatal("true should be 1")
	}
}

func TestCoerceAndCompare(t *testing.T) {
	if v := coerceToType(Real(3.0), TypeInteger); v.Type != TypeInteger || v.Int64() != 3 {
		t.Fatal("real->int coerce")
	}
	if v := coerceToType(Text("5"), TypeInteger); v.Type != TypeInteger || v.Int64() != 5 {
		t.Fatal("text->int coerce")
	}
	if v := coerceToType(Text("2.5"), TypeReal); v.Type != TypeReal || v.Float64() != 2.5 {
		t.Fatal("text->real coerce")
	}
	if v := coerceToType(Int(4), TypeReal); v.Type != TypeReal {
		t.Fatal("int->real coerce")
	}
	if v := coerceToType(Int(4), TypeText); v.Type != TypeText || v.Str() != "4" {
		t.Fatal("int->text coerce")
	}
	if v := coerceToType(Null(), TypeInteger); !v.IsNull() {
		t.Fatal("null coerce stays null")
	}
	// cross-type ordering: NULL rank < number < text < blob
	if compare(Int(1), Text("a")) >= 0 {
		t.Fatal("number should sort before text")
	}
	if compare(Text("z"), Blob([]byte("a"))) >= 0 {
		t.Fatal("text should sort before blob")
	}
	if compare(Real(1), Real(2)) != -1 || compare(Real(2), Real(1)) != 1 || compare(Int(3), Int(3)) != 0 {
		t.Fatal("numeric compare")
	}
	if !equalStrict(Null(), Null()) || equalStrict(Null(), Int(1)) {
		t.Fatal("equalStrict null handling")
	}
}

func TestDirectAPI(t *testing.T) {
	db := NewDatabase()
	if _, err := db.Exec(`CREATE TABLE t (a INTEGER, b TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO t (a, b) VALUES (?, ?)`, 10, "x"); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO t (a, b) VALUES (?, ?)`, 20, "y")
	if err != nil {
		t.Fatal(err)
	}
	if res.RowsAffected != 1 {
		t.Fatalf("rows affected %d", res.RowsAffected)
	}
	rs, err := db.Query(`SELECT a, b FROM t WHERE a > ? ORDER BY a`, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.Rows) != 2 || rs.Columns[0] != "a" {
		t.Fatalf("unexpected result %+v", rs)
	}
	if rs.Rows[0][0].Int64() != 10 {
		t.Fatalf("first row a = %v", rs.Rows[0][0])
	}
	// Query must reject non-select.
	if _, err := db.Query(`CREATE TABLE z (a INTEGER)`); err == nil {
		t.Fatal("Query should reject non-SELECT")
	}
	// bad arg type
	if _, err := db.Exec(`INSERT INTO t (a) VALUES (?)`, struct{}{}); err == nil {
		t.Fatal("expected bad arg error")
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		"",
		"SELECT",
		"SELECT 1 FROM",
		"CREATE TABLE",
		"INSERT INTO t",
		"UPDATE",
		"DELETE FROM",
		"SELECT * FROM t WHERE (1",
		"SELECT @@@",
		"CREATE TABLE t ()",
		"FOO bar",
		"SELECT 1 EXTRA GARBAGE",
	}
	for _, q := range bad {
		if _, err := Parse(q); err == nil {
			t.Errorf("expected parse error for %q", q)
		}
	}
	// Valid parses of assorted statements.
	good := []string{
		"BEGIN",
		"BEGIN TRANSACTION",
		"COMMIT",
		"ROLLBACK TRANSACTION",
		"CREATE TABLE t (a INT, b REAL, c BLOB, d TEXT, e INTEGER PRIMARY KEY NOT NULL);",
		"DROP TABLE IF EXISTS t",
		"SELECT DISTINCT a AS x FROM t",
		"SELECT a b FROM t",           // implicit alias
		"SELECT * FROM t LIMIT 5, 10", // offset,count form
		"SELECT -a, +b, NOT c FROM t",
		"UPDATE t SET a = 1, b = 2 WHERE a = 3",
	}
	for _, q := range good {
		if _, err := Parse(q); err != nil {
			t.Errorf("unexpected parse error for %q: %v", q, err)
		}
	}
}

func TestTokenizerFeatures(t *testing.T) {
	// Comments (line and block), quoted identifier, escaped string, blob.
	src := `-- line comment
	SELECT /* block */ "weird name", 'it''s', x'00ff', 1.5e2, .5 FROM t`
	toks, err := tokenize(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) == 0 {
		t.Fatal("no tokens")
	}
	// Quoted identifier with doubled quote.
	toks2, err := tokenize(`SELECT "a""b" FROM t`)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tk := range toks2 {
		if tk.typ == tokIdent && tk.text == `a"b` {
			found = true
		}
	}
	if !found {
		t.Fatal("quoted identifier with escaped quote not lexed")
	}
	// Tokenizer errors.
	for _, bad := range []string{`SELECT 'unterminated`, `SELECT "unterminated`, `SELECT x'0`, `SELECT x'abc'`, "SELECT \x00"} {
		if _, err := tokenize(bad); err == nil {
			t.Errorf("expected tokenize error for %q", bad)
		}
	}
}

func TestAggregateExpressions(t *testing.T) {
	db := NewDatabase()
	mustExecD(t, db, `CREATE TABLE s (grp TEXT, n INTEGER)`)
	mustExecD(t, db, `INSERT INTO s (grp, n) VALUES ('a',1),('a',2),('a',3),('b',10),('b',20),('c',5)`)

	// Arithmetic over an aggregate in the projection (evalAggBinary arith).
	rs := queryD(t, db, `SELECT grp, SUM(n) * 2 AS d FROM s GROUP BY grp ORDER BY grp`)
	if rs.Rows[0][1].Int64() != 12 || rs.Rows[1][1].Int64() != 60 {
		t.Fatalf("agg arith wrong: %+v", rs.Rows)
	}
	// Unary minus over aggregate (applyUnary).
	rs = queryD(t, db, `SELECT -SUM(n) FROM s`)
	if rs.Rows[0][0].Int64() != -41 {
		t.Fatalf("neg sum wrong: %v", rs.Rows[0][0])
	}
	// HAVING with AND/OR and comparison (combineLogical, compareOp, evalAggBinary cmp).
	rs = queryD(t, db, `SELECT grp FROM s GROUP BY grp HAVING SUM(n) >= 6 AND COUNT(*) > 1 ORDER BY grp`)
	if len(rs.Rows) != 2 || rs.Rows[0][0].Str() != "a" || rs.Rows[1][0].Str() != "b" {
		t.Fatalf("having and/cmp wrong: %+v", rs.Rows)
	}
	rs = queryD(t, db, `SELECT grp FROM s GROUP BY grp HAVING COUNT(*) = 1 OR SUM(n) > 25 ORDER BY grp`)
	if len(rs.Rows) != 2 || rs.Rows[0][0].Str() != "b" || rs.Rows[1][0].Str() != "c" {
		t.Fatalf("having or wrong: %+v", rs.Rows)
	}
	// HAVING with IN over a grouped column (evalAggIn).
	rs = queryD(t, db, `SELECT grp FROM s GROUP BY grp HAVING grp IN ('a','c') ORDER BY grp`)
	if len(rs.Rows) != 2 {
		t.Fatalf("having in wrong: %+v", rs.Rows)
	}
	// HAVING with NOT and IS NULL over aggregate expression, plus LIKE.
	rs = queryD(t, db, `SELECT grp FROM s GROUP BY grp HAVING NOT (SUM(n) > 6) AND grp LIKE 'c%'`)
	if len(rs.Rows) != 1 || rs.Rows[0][0].Str() != "c" {
		t.Fatalf("having not/like wrong: %+v", rs.Rows)
	}
	rs = queryD(t, db, `SELECT grp FROM s GROUP BY grp HAVING SUM(n) IS NOT NULL ORDER BY grp`)
	if len(rs.Rows) != 3 {
		t.Fatalf("having is not null wrong: %+v", rs.Rows)
	}
	// ORDER BY an aggregate expression, DESC.
	rs = queryD(t, db, `SELECT grp, COUNT(*) FROM s GROUP BY grp ORDER BY COUNT(*) DESC, grp`)
	if rs.Rows[0][0].Str() != "a" {
		t.Fatalf("order by agg wrong: %+v", rs.Rows)
	}
	// AVG real result and MIN/MAX with a param inside an aggregate query.
	rs = queryD(t, db, `SELECT AVG(n), MIN(n), MAX(n) FROM s`)
	if rs.Rows[0][0].Float64() < 6.8 || rs.Rows[0][0].Float64() > 6.9 {
		t.Fatalf("avg wrong: %v", rs.Rows[0][0])
	}
	// String concat over aggregate/group column.
	rs = queryD(t, db, `SELECT grp || '!' FROM s GROUP BY grp ORDER BY grp LIMIT 1`)
	if rs.Rows[0][0].Str() != "a!" {
		t.Fatalf("agg concat wrong: %v", rs.Rows[0][0])
	}
}

func TestArithmeticEdgeCases(t *testing.T) {
	db := NewDatabase()
	rs := queryD(t, db, `SELECT 7 / 2, 7.0 / 2, 7 % 3, 2.5 * 2, 10 - 3, 10 - 2.5`)
	r := rs.Rows[0]
	if r[0].Int64() != 3 || r[1].Float64() != 3.5 || r[2].Int64() != 1 {
		t.Fatalf("arith int/real/mod wrong: %+v", r)
	}
	if r[3].Float64() != 5 || r[4].Int64() != 7 || r[5].Float64() != 7.5 {
		t.Fatalf("arith mul/sub wrong: %+v", r)
	}
	// Division by zero yields NULL.
	rs = queryD(t, db, `SELECT 1 / 0, 1 % 0`)
	if !rs.Rows[0][0].IsNull() || !rs.Rows[0][1].IsNull() {
		t.Fatalf("div by zero should be NULL: %+v", rs.Rows[0])
	}
	// NULL propagation through arithmetic and comparison.
	rs = queryD(t, db, `SELECT NULL + 1, NULL = 1, NULL || 'x'`)
	for i, v := range rs.Rows[0] {
		if !v.IsNull() {
			t.Fatalf("col %d should be NULL: %v", i, v)
		}
	}
}

func TestThreeValuedLogic(t *testing.T) {
	db := NewDatabase()
	mustExecD(t, db, `CREATE TABLE t (a INTEGER)`)
	mustExecD(t, db, `INSERT INTO t (a) VALUES (1),(NULL),(0)`)
	// NULL AND false => false; only a=1 with (a=1 OR NULL) style.
	rs := queryD(t, db, `SELECT a FROM t WHERE a = 1 OR a IS NULL ORDER BY a`)
	if len(rs.Rows) != 2 {
		t.Fatalf("3vl or wrong: %+v", rs.Rows)
	}
	// IN with NULL member and no match yields unknown -> excluded.
	rs = queryD(t, db, `SELECT a FROM t WHERE a IN (2, NULL)`)
	if len(rs.Rows) != 0 {
		t.Fatalf("in-null wrong: %+v", rs.Rows)
	}
}

func TestNestedTransactionError(t *testing.T) {
	c := newConn(":memory:")
	if err := c.begin(); err != nil {
		t.Fatal(err)
	}
	if err := c.begin(); err == nil {
		t.Fatal("expected nested transaction error")
	}
	if err := c.commit(); err != nil {
		t.Fatal(err)
	}
	if err := c.commit(); err == nil {
		t.Fatal("expected error committing with no tx")
	}
	if err := c.rollback(); err == nil {
		t.Fatal("expected error rolling back with no tx")
	}
}

func mustExecD(t *testing.T, db *Database, q string, args ...interface{}) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func queryD(t *testing.T, db *Database, q string, args ...interface{}) *ResultSet {
	t.Helper()
	rs, err := db.Query(q, args...)
	if err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	return rs
}
