// Package sqlite implements a small, embedded SQL database engine written in
// pure Go, using only the standard library. It provides a SQL tokenizer,
// recursive-descent parser, and a tree-walking executor over an in-memory,
// row-oriented store, and exposes everything through a database/sql driver.
//
// # Driver
//
// The package registers a database/sql driver named "mstsqlite" (see
// [DriverName]). Open a database with:
//
//	db, err := sql.Open("mstsqlite", ":memory:")
//
// A DSN of ":memory:" or "" creates a private anonymous database. Any other DSN
// names a database that is shared between every connection opened with the same
// name for the lifetime of the process, so a connection pool sees a consistent
// view.
//
// The driver supports the '?' positional placeholder for arguments and scans
// results into the usual Go types: nil, int64, float64, string and []byte.
//
// # Supported SQL
//
// The engine implements a useful subset of SQL:
//
//   - CREATE TABLE with typed columns (INTEGER, TEXT, REAL, BLOB), plus the
//     column constraints PRIMARY KEY and NOT NULL. CREATE TABLE IF NOT EXISTS
//     and DROP TABLE [IF EXISTS] are supported.
//   - INSERT INTO t [(cols...)] VALUES (...), (...), ... including multi-row
//     inserts.
//   - SELECT with an explicit column list or '*', column aliases (AS),
//     DISTINCT, WHERE, GROUP BY, HAVING, ORDER BY (ASC/DESC), and LIMIT/OFFSET.
//   - A single INNER JOIN of two tables with an ON condition.
//   - WHERE/HAVING expressions: comparisons (= <> < > <= >=), AND/OR/NOT, IN
//     (list), LIKE (with % and _ wildcards), IS NULL / IS NOT NULL, arithmetic
//     (+ - * / %) and string concatenation (||).
//   - Aggregate functions COUNT (including COUNT(*) and COUNT(DISTINCT x)), SUM,
//     AVG, MIN and MAX.
//   - UPDATE t SET col = expr [, ...] WHERE ... and DELETE FROM t WHERE ...
//   - Transactions via BEGIN / COMMIT / ROLLBACK, exposed through the standard
//     [database/sql.DB.Begin] and [database/sql.Tx] API. Transactions use
//     snapshot rollback and serializable isolation (one writer at a time).
//
// # Type system
//
// Following SQLite, values carry their own dynamic storage class: NULL,
// INTEGER (int64), REAL (float64), TEXT (UTF-8 string) or BLOB ([]byte). NULL
// propagates through comparisons and arithmetic using three-valued logic, and a
// NULL predicate is treated as false by WHERE and HAVING. Declared column types
// apply a light affinity: inserted values are coerced toward the column type
// when it is lossless to do so.
//
// # Limits
//
// This is a compact, didactic engine, not a full SQLite replacement. Notable
// limits: data lives only in memory (there is no on-disk file format); there is
// no query planner or secondary indexes (queries scan and, for JOIN, form a
// nested loop); only INNER JOIN of exactly two tables is supported; subqueries,
// views, triggers, common table expressions, window functions, ALTER TABLE and
// foreign keys are not implemented; and only a single PRIMARY KEY column
// (enforced as UNIQUE + NOT NULL) is supported per table.
//
// # Direct API
//
// Besides the database/sql driver, the [Database] type can be used directly via
// [Database.Exec] and [Database.Query], and [Parse] exposes the AST for callers
// that want to inspect statements.
package sqlite
