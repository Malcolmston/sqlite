package sqlite

import (
	"database/sql"
	"reflect"
	"testing"
)

// openDB opens a fresh anonymous database pinned to a single connection so the
// in-memory data is stable across statements.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open(DriverName, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...interface{}) sql.Result {
	t.Helper()
	res, err := db.Exec(q, args...)
	if err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
	return res
}

// queryStrings returns each row rendered as a slice of strings for easy
// comparison, using NullString scanning.
func queryStrings(t *testing.T, db *sql.DB, q string, args ...interface{}) [][]string {
	t.Helper()
	rows, err := db.Query(q, args...)
	if err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for rows.Next() {
		cells := make([]sql.NullString, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		row := make([]string, len(cols))
		for i, c := range cells {
			if c.Valid {
				row[i] = c.String
			} else {
				row[i] = "<nil>"
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func seedUsers(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, age INTEGER, city TEXT)`)
	mustExec(t, db, `INSERT INTO users (id, name, age, city) VALUES
		(1, 'alice', 30, 'NYC'),
		(2, 'bob', 25, 'LA'),
		(3, 'carol', 30, 'NYC'),
		(4, 'dave', NULL, 'LA'),
		(5, 'eve', 42, 'SF')`)
}

func TestCreateInsertSelect(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	got := queryStrings(t, db, `SELECT name FROM users ORDER BY name`)
	want := [][]string{{"alice"}, {"bob"}, {"carol"}, {"dave"}, {"eve"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSelectStar(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	got := queryStrings(t, db, `SELECT * FROM users WHERE id = 1`)
	want := [][]string{{"1", "alice", "30", "NYC"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWhereComparisonsAndLogic(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	got := queryStrings(t, db, `SELECT name FROM users WHERE age > 25 AND city = 'NYC' ORDER BY name`)
	want := [][]string{{"alice"}, {"carol"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	got = queryStrings(t, db, `SELECT name FROM users WHERE age < 26 OR city = 'SF' ORDER BY name`)
	want = [][]string{{"bob"}, {"eve"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	got = queryStrings(t, db, `SELECT name FROM users WHERE NOT city = 'LA' ORDER BY name`)
	want = [][]string{{"alice"}, {"carol"}, {"eve"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestNullSemantics(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	// dave has NULL age; comparison with NULL is unknown -> excluded.
	got := queryStrings(t, db, `SELECT name FROM users WHERE age > 0 ORDER BY name`)
	want := [][]string{{"alice"}, {"bob"}, {"carol"}, {"eve"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	got = queryStrings(t, db, `SELECT name FROM users WHERE age IS NULL`)
	want = [][]string{{"dave"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IS NULL got %v want %v", got, want)
	}
	got = queryStrings(t, db, `SELECT name FROM users WHERE age IS NOT NULL ORDER BY name`)
	want = [][]string{{"alice"}, {"bob"}, {"carol"}, {"eve"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IS NOT NULL got %v want %v", got, want)
	}
}

func TestInAndLike(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	got := queryStrings(t, db, `SELECT name FROM users WHERE id IN (1, 3, 5) ORDER BY name`)
	want := [][]string{{"alice"}, {"carol"}, {"eve"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IN got %v want %v", got, want)
	}
	got = queryStrings(t, db, `SELECT name FROM users WHERE id NOT IN (1, 3, 5) ORDER BY name`)
	want = [][]string{{"bob"}, {"dave"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NOT IN got %v want %v", got, want)
	}
	got = queryStrings(t, db, `SELECT name FROM users WHERE name LIKE '_a%' ORDER BY name`)
	want = [][]string{{"carol"}, {"dave"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LIKE got %v want %v", got, want)
	}
	got = queryStrings(t, db, `SELECT name FROM users WHERE name NOT LIKE 'a%' AND name LIKE '%e' ORDER BY name`)
	want = [][]string{{"dave"}, {"eve"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NOT LIKE got %v want %v", got, want)
	}
}

func TestOrderLimitOffset(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	got := queryStrings(t, db, `SELECT name FROM users ORDER BY age DESC LIMIT 2`)
	want := [][]string{{"eve"}, {"alice"}} // alice/carol tie at 30, stable insertion order
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	got = queryStrings(t, db, `SELECT name FROM users ORDER BY id LIMIT 2 OFFSET 2`)
	want = [][]string{{"carol"}, {"dave"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("offset got %v want %v", got, want)
	}
	// NULLs sort first ascending.
	got = queryStrings(t, db, `SELECT name FROM users ORDER BY age ASC LIMIT 1`)
	want = [][]string{{"dave"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("null-order got %v want %v", got, want)
	}
}

func TestAggregatesNoGroup(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	var count, sum, mn, mx int
	var avg float64
	err := db.QueryRow(`SELECT COUNT(*), SUM(age), AVG(age), MIN(age), MAX(age) FROM users`).
		Scan(&count, &sum, &avg, &mn, &mx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 || sum != 127 || mn != 25 || mx != 42 {
		t.Fatalf("agg wrong: count=%d sum=%d min=%d max=%d", count, sum, mn, mx)
	}
	if avg < 31.7 || avg > 31.8 { // (30+25+30+42)/4 = 31.75
		t.Fatalf("avg wrong: %v", avg)
	}
	var c int
	if err := db.QueryRow(`SELECT COUNT(age) FROM users`).Scan(&c); err != nil {
		t.Fatal(err)
	}
	if c != 4 { // NULL age excluded
		t.Fatalf("COUNT(age) = %d want 4", c)
	}
}

func TestGroupByHaving(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	got := queryStrings(t, db, `SELECT city, COUNT(*) FROM users GROUP BY city ORDER BY city`)
	want := [][]string{{"LA", "2"}, {"NYC", "2"}, {"SF", "1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("group got %v want %v", got, want)
	}
	got = queryStrings(t, db, `SELECT city, COUNT(*) AS n FROM users GROUP BY city HAVING COUNT(*) > 1 ORDER BY city`)
	want = [][]string{{"LA", "2"}, {"NYC", "2"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("having got %v want %v", got, want)
	}
}

func TestCountDistinct(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	var n int
	if err := db.QueryRow(`SELECT COUNT(DISTINCT city) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("distinct city = %d want 3", n)
	}
}

func TestDistinctRows(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	got := queryStrings(t, db, `SELECT DISTINCT city FROM users ORDER BY city`)
	want := [][]string{{"LA"}, {"NYC"}, {"SF"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("distinct got %v want %v", got, want)
	}
}

func TestInnerJoin(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	mustExec(t, db, `CREATE TABLE orders (id INTEGER PRIMARY KEY, uid INTEGER, amount REAL)`)
	mustExec(t, db, `INSERT INTO orders (id, uid, amount) VALUES (1,1,10.0),(2,1,5.0),(3,2,7.5),(4,5,2.0)`)
	got := queryStrings(t, db, `SELECT u.name, SUM(o.amount) AS total FROM users u INNER JOIN orders o ON u.id = o.uid GROUP BY u.name ORDER BY total DESC`)
	want := [][]string{{"alice", "15"}, {"bob", "7.5"}, {"eve", "2"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("join got %v want %v", got, want)
	}
	// Qualified star.
	got = queryStrings(t, db, `SELECT o.* FROM users u INNER JOIN orders o ON u.id = o.uid WHERE u.name = 'alice' ORDER BY o.id`)
	want = [][]string{{"1", "1", "10"}, {"2", "1", "5"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("qualified star got %v want %v", got, want)
	}
}

func TestUpdateDelete(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	res := mustExec(t, db, `UPDATE users SET age = age + 1, city = 'BOS' WHERE city = 'NYC'`)
	n, _ := res.RowsAffected()
	if n != 2 {
		t.Fatalf("update affected %d want 2", n)
	}
	got := queryStrings(t, db, `SELECT name, age, city FROM users WHERE city = 'BOS' ORDER BY name`)
	want := [][]string{{"alice", "31", "BOS"}, {"carol", "31", "BOS"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("post-update got %v want %v", got, want)
	}
	res = mustExec(t, db, `DELETE FROM users WHERE age IS NULL`)
	n, _ = res.RowsAffected()
	if n != 1 {
		t.Fatalf("delete affected %d want 1", n)
	}
	var c int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&c); err != nil {
		t.Fatal(err)
	}
	if c != 4 {
		t.Fatalf("count after delete = %d want 4", c)
	}
}

func TestPlaceholders(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	got := queryStrings(t, db, `SELECT name FROM users WHERE city = ? AND age > ? ORDER BY name`, "NYC", 20)
	want := [][]string{{"alice"}, {"carol"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("placeholder got %v want %v", got, want)
	}
}

func TestTransactionCommit(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO users (id, name, age, city) VALUES (6, 'frank', 50, 'SF')`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var c int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&c); err != nil {
		t.Fatal(err)
	}
	if c != 6 {
		t.Fatalf("after commit count = %d want 6", c)
	}
}

func TestTransactionRollback(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`DELETE FROM users`); err != nil {
		t.Fatal(err)
	}
	var during int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&during); err != nil {
		t.Fatal(err)
	}
	if during != 0 {
		t.Fatalf("during tx count = %d want 0", during)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var after int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != 5 {
		t.Fatalf("after rollback count = %d want 5", after)
	}
}

func TestConstraints(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	if _, err := db.Exec(`INSERT INTO users (id, name, age, city) VALUES (1, 'dup', 1, 'X')`); err == nil {
		t.Fatal("expected PK uniqueness error")
	}
	if _, err := db.Exec(`INSERT INTO users (id, name, age, city) VALUES (7, NULL, 1, 'X')`); err == nil {
		t.Fatal("expected NOT NULL error")
	}
}

func TestExpressionsAndConcat(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	got := queryStrings(t, db, `SELECT name || '@' || city AS handle FROM users WHERE id = 1`)
	want := [][]string{{"alice@NYC"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("concat got %v want %v", got, want)
	}
	got = queryStrings(t, db, `SELECT age * 2 - 5 FROM users WHERE id = 2`)
	want = [][]string{{"45"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("arith got %v want %v", got, want)
	}
}

func TestSelectConstantNoFrom(t *testing.T) {
	db := openDB(t)
	got := queryStrings(t, db, `SELECT 1 + 2, 'hi', 3.5 * 2`)
	want := [][]string{{"3", "hi", "7"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("const got %v want %v", got, want)
	}
}

func TestDropAndIfNotExists(t *testing.T) {
	db := openDB(t)
	mustExec(t, db, `CREATE TABLE t (a INTEGER)`)
	mustExec(t, db, `CREATE TABLE IF NOT EXISTS t (a INTEGER)`)
	mustExec(t, db, `DROP TABLE t`)
	mustExec(t, db, `DROP TABLE IF EXISTS t`)
	if _, err := db.Exec(`DROP TABLE t`); err == nil {
		t.Fatal("expected no such table error")
	}
}

func TestBlobAndReal(t *testing.T) {
	db := openDB(t)
	mustExec(t, db, `CREATE TABLE b (data BLOB, r REAL)`)
	mustExec(t, db, `INSERT INTO b (data, r) VALUES (x'48656c6c6f', 3.14)`)
	rows, err := db.Query(`SELECT data, r FROM b`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		t.Fatal("no row")
	}
	var data []byte
	var r float64
	if err := rows.Scan(&data, &r); err != nil {
		t.Fatal(err)
	}
	if string(data) != "Hello" || r != 3.14 {
		t.Fatalf("blob/real got %q %v", data, r)
	}
}

func TestSharedNamedDatabase(t *testing.T) {
	db1, err := sql.Open(DriverName, "shared-test-db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db1.Close() }()
	if _, err := db1.Exec(`CREATE TABLE IF NOT EXISTS shared (x INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db1.Exec(`INSERT INTO shared (x) VALUES (99)`); err != nil {
		t.Fatal(err)
	}
	db2, err := sql.Open(DriverName, "shared-test-db")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db2.Close() }()
	var x int
	if err := db2.QueryRow(`SELECT x FROM shared`).Scan(&x); err != nil {
		t.Fatal(err)
	}
	if x != 99 {
		t.Fatalf("shared db x = %d want 99", x)
	}
}

func TestPreparedStatement(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	stmt, err := db.Prepare(`SELECT name FROM users WHERE city = ? AND age >= ? ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stmt.Close() }()
	rows, err := stmt.Query("NYC", 30)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	_ = rows.Close()
	if !reflect.DeepEqual(names, []string{"alice", "carol"}) {
		t.Fatalf("prepared query got %v", names)
	}

	ins, err := db.Prepare(`INSERT INTO users (id, name, age, city) VALUES (?, ?, ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ins.Close() }()
	res, err := ins.Exec(10, "grace", 33, "SF")
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("prepared insert affected %d", n)
	}
}

func TestErrors(t *testing.T) {
	db := openDB(t)
	seedUsers(t, db)
	cases := []string{
		`SELCT 1`,               // parse error
		`SELECT * FROM missing`, // no such table
		`SELECT nope FROM users`,
		`INSERT INTO users (id) VALUES (1, 2)`, // arity mismatch
		`CREATE TABLE users (a INTEGER)`,       // already exists
	}
	for _, q := range cases {
		if _, err := db.Exec(q); err == nil {
			// Some are queries; try Query too.
			if _, err2 := db.Query(q); err2 == nil {
				t.Fatalf("expected error for %q", q)
			}
		}
	}
}
