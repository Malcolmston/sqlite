// Library content for the sqlite documentation site. Mirrors the shape used by
// the malcolmston/go landing site's data.ts so the sibling sites stay in sync.
export interface Lib {
  id: string; name: string; icon: string; accent: string; pkg: string; node: string;
  repo: string; docs: string; tagline: string; blurb: string; tags: string[];
  features: string[]; node_code: string; go_code: string; integrate: string;
}

export const NODE_ACCENT = '#8cc84b';

export const SQLITE: Lib = {
  id:"sqlite", name:"SQLite", icon:'<i class="fa-solid fa-database"></i>', accent:"#f5a623",
  pkg:"github.com/malcolmston/sqlite", node:"sqlite/sqlite",
  repo:"https://github.com/malcolmston/sqlite", docs:"https://malcolmston.github.io/sqlite/",
  tagline:"A pure-Go embedded SQL engine with a database/sql driver.",
  blurb:"A small, dependency-free SQL database engine written in pure Go (standard library only, no cgo). It ships a "+
    "SQL tokenizer, a recursive-descent parser and a tree-walking executor over an in-memory, row-oriented store, "+
    "and plugs into the standard database/sql package through a registered driver named \"mstsqlite\". The engine "+
    "implements a genuinely useful subset of SQL — CREATE TABLE / INSERT / SELECT (WHERE, GROUP BY, HAVING, "+
    "ORDER BY, LIMIT, a two-table INNER JOIN and aggregates), UPDATE, DELETE and transactions — with SQLite-style "+
    "dynamic typing and three-valued NULL logic. Import path github.com/malcolmston/sqlite; package sqlite.",
  tags:["database/sql driver","pure Go","no cgo","in-memory","SQL parser","tree-walking executor","transactions","dynamic typing"],
  features:[
    "Registers the <code>&quot;mstsqlite&quot;</code> <code>database/sql</code> driver (see <code>DriverName</code>) — open with <code>sql.Open(&quot;mstsqlite&quot;, &quot;:memory:&quot;)</code>",
    "Full DDL/DML subset — <code>CREATE TABLE</code> (typed columns, <code>PRIMARY KEY</code>/<code>NOT NULL</code>, <code>IF NOT EXISTS</code>), <code>INSERT</code>, <code>UPDATE</code>, <code>DELETE</code>, <code>DROP TABLE</code>",
    "Rich <code>SELECT</code> — <code>WHERE</code>, <code>GROUP BY</code>, <code>HAVING</code>, <code>ORDER BY</code>, <code>LIMIT</code>/<code>OFFSET</code>, <code>DISTINCT</code>, <code>AS</code> aliases and a two-table <code>INNER JOIN</code>",
    "Aggregates — <code>COUNT</code> (incl. <code>COUNT(*)</code> / <code>COUNT(DISTINCT x)</code>), <code>SUM</code>, <code>AVG</code>, <code>MIN</code>, <code>MAX</code>",
    "Expressions — comparisons, <code>AND</code>/<code>OR</code>/<code>NOT</code>, <code>IN</code>, <code>LIKE</code> (<code>%</code>/<code>_</code>), <code>IS NULL</code>, arithmetic and <code>||</code> concatenation",
    "Transactions via <code>BEGIN</code>/<code>COMMIT</code>/<code>ROLLBACK</code> — snapshot rollback, serializable isolation (one writer at a time)",
    "SQLite-style dynamic typing — the <code>Value</code>/<code>ValueType</code> storage classes (NULL, INTEGER, REAL, TEXT, BLOB) with three-valued NULL logic",
    "Direct, non-<code>database/sql</code> API — <code>NewDatabase</code>, <code>Database.Exec</code>, <code>Database.Query</code> (returning <code>ResultSet</code>/<code>ExecResult</code>) and <code>Parse</code> for AST inspection"
  ],
  node_code:
`#include <sqlite3.h>
#include <stdio.h>

int main(void) {
    sqlite3 *db;
    sqlite3_open(":memory:", &db);
    sqlite3_exec(db, "CREATE TABLE fruit (name TEXT, qty INTEGER)", 0, 0, 0);
    sqlite3_exec(db,
        "INSERT INTO fruit VALUES ('apple', 3), ('banana', 7)", 0, 0, 0);

    sqlite3_stmt *st;
    sqlite3_prepare_v2(db,
        "SELECT name, qty FROM fruit WHERE qty >= 5 ORDER BY qty DESC",
        -1, &st, 0);
    while (sqlite3_step(st) == SQLITE_ROW)
        printf("%s: %d\\n", sqlite3_column_text(st, 0),
                            sqlite3_column_int(st, 1));
    sqlite3_finalize(st);
    sqlite3_close(db);
}`,
  go_code:
`import (
    "database/sql"
    "fmt"

    _ "github.com/malcolmston/sqlite" // registers the "mstsqlite" driver
)

db, _ := sql.Open("mstsqlite", ":memory:")
db.SetMaxOpenConns(1) // pin the anonymous in-memory DB to one connection

db.Exec(` + "`CREATE TABLE fruit (name TEXT, qty INTEGER)`" + `)
db.Exec(` + "`INSERT INTO fruit VALUES (?, ?), (?, ?)`" + `, "apple", 3, "banana", 7)

rows, _ := db.Query(` + "`SELECT name, qty FROM fruit WHERE qty >= ? ORDER BY qty DESC`" + `, 5)
for rows.Next() {
    var name string
    var qty int
    rows.Scan(&name, &qty)
    fmt.Printf("%s: %d\\n", name, qty)
}`,
  integrate:
`<span class="tok-c">// Aggregate with GROUP BY / HAVING / ORDER BY over the database/sql handle.</span>
rows, _ := db.Query(` + "`SELECT name, SUM(qty) AS total FROM fruit\n    GROUP BY name HAVING SUM(qty) > ? ORDER BY total DESC`" + `, 2)

<span class="tok-c">// Transactions use snapshot rollback and serializable isolation.</span>
tx, _ := db.Begin()
tx.Exec(` + "`UPDATE fruit SET qty = qty + 1 WHERE name = ?`" + `, "apple")
tx.Commit() <span class="tok-c">// or tx.Rollback() to discard</span>

<span class="tok-c">// A single two-table INNER JOIN ... ON is supported.</span>
db.Query(` + "`SELECT f.name, b.aisle FROM fruit f INNER JOIN bins b ON b.name = f.name`" + `)

<span class="tok-c">// Skip database/sql entirely and drive the engine in-process.</span>
store := sqlite.NewDatabase()
store.Exec(` + "`CREATE TABLE t (a INTEGER, b TEXT)`" + `)
rs, _ := store.Query(` + "`SELECT a, b FROM t WHERE a = ?`" + `, 1)
fmt.Println(rs.Columns, rs.Rows)

<span class="tok-c">// Inspect the parsed AST without executing anything.</span>
stmt, _ := sqlite.Parse(` + "`SELECT * FROM t WHERE a > 10`" + `)
_ = stmt`
};
