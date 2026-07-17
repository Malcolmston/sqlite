# sqlite

Idiomatic embedded SQL database for Go — a small, dependency-free SQL engine
written in **pure Go** (standard library only, no cgo). It ships a SQL
tokenizer, parser and tree-walking executor over an in-memory store, and plugs
into the standard `database/sql` package through a registered driver named
**`mstsqlite`**.

> Module path: `github.com/malcolmston/sqlite` · Version: `0.1.0`

## Install

```sh
go get github.com/malcolmston/sqlite
```

Requires Go 1.24+. There are **no third-party dependencies**.

## Quick start (database/sql)

```go
package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/malcolmston/sqlite" // registers the "mstsqlite" driver
)

func main() {
	db, err := sql.Open("mstsqlite", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // keep the anonymous in-memory DB on one connection

	if _, err := db.Exec(`CREATE TABLE fruit (name TEXT NOT NULL, qty INTEGER)`); err != nil {
		log.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO fruit (name, qty) VALUES (?, ?), (?, ?)`,
		"apple", 3, "banana", 7,
	); err != nil {
		log.Fatal(err)
	}

	rows, err := db.Query(`SELECT name, qty FROM fruit WHERE qty >= ? ORDER BY qty DESC`, 5)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var qty int
		if err := rows.Scan(&name, &qty); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s: %d\n", name, qty)
	}

	var total int
	if err := db.QueryRow(`SELECT SUM(qty) FROM fruit`).Scan(&total); err != nil {
		log.Fatal(err)
	}
	fmt.Println("total:", total)
}
```

### DSN / database names

- `":memory:"` or `""` — a **private** anonymous in-memory database. Pin the pool
  to a single connection (`db.SetMaxOpenConns(1)`) so every statement sees the
  same data.
- any other string, e.g. `"appdb"` — a **named** in-memory database that is
  shared between all connections (and all `sql.DB` handles) using that name for
  the lifetime of the process.

### Transactions

```go
tx, err := db.Begin()
if err != nil {
	log.Fatal(err)
}
if _, err := tx.Exec(`INSERT INTO fruit (name, qty) VALUES (?, ?)`, "cherry", 5); err != nil {
	tx.Rollback()
	log.Fatal(err)
}
tx.Commit() // or tx.Rollback() to discard
```

Transactions use snapshot rollback and run with serializable isolation (a single
writer at a time).

## Supported SQL

- **DDL:** `CREATE TABLE` with typed columns (`INTEGER`, `TEXT`, `REAL`, `BLOB`),
  plus `PRIMARY KEY` and `NOT NULL` constraints; `CREATE TABLE IF NOT EXISTS`;
  `DROP TABLE [IF EXISTS]`.
- **INSERT:** `INSERT INTO t [(cols…)] VALUES (…), (…), …` (multi-row).
- **SELECT:** column lists and `*` (including `t.*`), `AS` aliases, `DISTINCT`,
  `WHERE`, `GROUP BY`, `HAVING`, `ORDER BY` (`ASC`/`DESC`), `LIMIT`/`OFFSET`, and
  a two-table `INNER JOIN … ON …`.
- **Expressions:** `=  <>  <  >  <=  >=`, `AND`/`OR`/`NOT`, `IN (…)`, `LIKE`
  (with `%` and `_`), `IS NULL` / `IS NOT NULL`, arithmetic `+ - * / %`, and
  string concatenation `||`.
- **Aggregates:** `COUNT` (incl. `COUNT(*)` and `COUNT(DISTINCT x)`), `SUM`,
  `AVG`, `MIN`, `MAX`.
- **DML:** `UPDATE t SET … WHERE …`, `DELETE FROM t WHERE …`.
- **Transactions:** `BEGIN` / `COMMIT` / `ROLLBACK` (via `database/sql`).

Values follow SQLite-style dynamic typing (`NULL`, `INTEGER`, `REAL`, `TEXT`,
`BLOB`) with three-valued NULL logic, and `?` positional placeholders are
supported everywhere arguments are accepted.

## Direct (non-`database/sql`) API

```go
store := sqlite.NewDatabase()
store.Exec(`CREATE TABLE t (a INTEGER, b TEXT)`)
store.Exec(`INSERT INTO t (a, b) VALUES (?, ?)`, 1, "x")
rs, _ := store.Query(`SELECT a, b FROM t WHERE a = ?`, 1)
fmt.Println(rs.Columns, rs.Rows)
```

`sqlite.Parse(sql)` is also exported for callers that want to inspect the AST.

## Limits

In-memory only (no on-disk format); no query planner or secondary indexes; only
a single two-table `INNER JOIN`; no subqueries, views, triggers, CTEs, window
functions, `ALTER TABLE` or foreign keys; one `PRIMARY KEY` column per table.
See the package `doc.go` for the full godoc overview.

## License

See repository.
