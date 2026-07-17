package sqlite

import (
	"fmt"
	"strings"
	"sync"
)

// column is the stored form of a table column definition.
type column struct {
	Name       string
	Type       ValueType
	PrimaryKey bool
	NotNull    bool
}

// table is an in-memory relation: an ordered set of rows keyed by an internal
// rowid, plus a schema.
type table struct {
	name      string
	cols      []column
	colIndex  map[string]int // lower-cased name -> position
	pk        int            // index of the primary-key column, or -1
	rows      map[int64][]Value
	order     []int64 // rowids in insertion order
	nextRowID int64
}

func newTable(name string, defs []ColumnDef) *table {
	t := &table{
		name:      name,
		colIndex:  make(map[string]int, len(defs)),
		pk:        -1,
		rows:      make(map[int64][]Value),
		nextRowID: 1,
	}
	for i, d := range defs {
		t.cols = append(t.cols, column(d))
		t.colIndex[strings.ToLower(d.Name)] = i
		if d.PrimaryKey {
			t.pk = i
		}
	}
	return t
}

// clone deep-copies the table so it can serve as a transaction snapshot.
func (t *table) clone() *table {
	nt := &table{
		name:      t.name,
		cols:      t.cols, // schema is immutable after creation
		colIndex:  t.colIndex,
		pk:        t.pk,
		rows:      make(map[int64][]Value, len(t.rows)),
		order:     append([]int64(nil), t.order...),
		nextRowID: t.nextRowID,
	}
	for id, row := range t.rows {
		cp := make([]Value, len(row))
		copy(cp, row)
		nt.rows[id] = cp
	}
	return nt
}

// columnIndex resolves a column name (case-insensitive) to its position.
func (t *table) columnIndex(name string) (int, bool) {
	i, ok := t.colIndex[strings.ToLower(name)]
	return i, ok
}

// insertRow appends a fully-formed row, assigning a rowid and enforcing PK
// uniqueness.
func (t *table) insertRow(row []Value) (int64, error) {
	if t.pk >= 0 {
		pkVal := row[t.pk]
		for _, id := range t.order {
			if equalStrict(t.rows[id][t.pk], pkVal) {
				return 0, fmt.Errorf("sqlite: UNIQUE constraint failed: %s.%s", t.name, t.cols[t.pk].Name)
			}
		}
	}
	id := t.nextRowID
	t.nextRowID++
	t.rows[id] = row
	t.order = append(t.order, id)
	return id, nil
}

// deleteRow removes a row by rowid.
func (t *table) deleteRow(id int64) {
	delete(t.rows, id)
	for i, x := range t.order {
		if x == id {
			t.order = append(t.order[:i], t.order[i+1:]...)
			break
		}
	}
}

// Database is an in-memory SQL database. It is safe for concurrent use; every
// statement is serialized through a single mutex, and transactions hold that
// mutex for their whole lifetime, giving serializable isolation.
type Database struct {
	mu     sync.Mutex
	tables map[string]*table
}

// NewDatabase creates an empty in-memory database. Most users obtain a database
// through the database/sql driver instead of calling this directly.
func NewDatabase() *Database {
	return &Database{tables: make(map[string]*table)}
}

func (db *Database) getTable(name string) (*table, bool) {
	t, ok := db.tables[strings.ToLower(name)]
	return t, ok
}

// snapshot deep-copies every table for use as a rollback backup.
func (db *Database) snapshot() map[string]*table {
	m := make(map[string]*table, len(db.tables))
	for k, v := range db.tables {
		m[k] = v.clone()
	}
	return m
}

// restore replaces the live table set with a previously taken snapshot.
func (db *Database) restore(snap map[string]*table) {
	db.tables = snap
}

// --- shared registry so all connections to the same DSN share one database ---

var (
	registryMu sync.Mutex
	registry   = map[string]*Database{}
)

// sharedDatabase returns the Database associated with a DSN, creating it on first
// use. The special DSN "" or ":memory:" without a name yields a fresh anonymous
// database per call so independent handles do not collide.
func sharedDatabase(dsn string) *Database {
	name := strings.TrimSpace(dsn)
	// Anonymous in-memory databases are not shared.
	if name == "" || name == ":memory:" {
		return NewDatabase()
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if db, ok := registry[name]; ok {
		return db
	}
	db := NewDatabase()
	registry[name] = db
	return db
}
