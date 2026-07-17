package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sort"
	"sync"
)

// DriverName is the name under which this driver registers itself with
// database/sql. Use it with sql.Open.
const DriverName = "mstsqlite"

func init() {
	sql.Register(DriverName, &Driver{})
}

// Driver implements database/sql/driver.Driver.
type Driver struct{}

// Open returns a new connection to the database named by the DSN. A DSN of
// ":memory:" or the empty string yields a private anonymous database; any other
// name is shared between all connections that use it.
func (d *Driver) Open(name string) (driver.Conn, error) {
	return &driverConn{c: newConn(name)}, nil
}

// driverConn adapts conn to the database/sql/driver interfaces.
type driverConn struct {
	mu     sync.Mutex
	c      *conn
	closed bool
}

var (
	_ driver.Conn           = (*driverConn)(nil)
	_ driver.ExecerContext  = (*driverConn)(nil)
	_ driver.QueryerContext = (*driverConn)(nil)
)

// Prepare compiles a statement.
func (dc *driverConn) Prepare(query string) (driver.Stmt, error) {
	stmt, n, err := parseWithCount(query)
	if err != nil {
		return nil, err
	}
	return &driverStmt{conn: dc, stmt: stmt, nParam: n, query: query}, nil
}

// Close closes the connection, rolling back any open transaction.
func (dc *driverConn) Close() error {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if dc.closed {
		return nil
	}
	dc.closed = true
	if dc.c.inTx {
		return dc.c.rollback()
	}
	return nil
}

// Begin starts a transaction.
func (dc *driverConn) Begin() (driver.Tx, error) {
	if err := dc.c.begin(); err != nil {
		return nil, err
	}
	return &driverTx{conn: dc}, nil
}

// ExecContext implements the driver.ExecerContext fast path.
func (dc *driverConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	stmt, _, err := parseWithCount(query)
	if err != nil {
		return nil, err
	}
	vals, err := valuesFromNamed(args)
	if err != nil {
		return nil, err
	}
	return dc.execStmt(stmt, vals)
}

// QueryContext implements the driver.QueryerContext fast path.
func (dc *driverConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	stmt, _, err := parseWithCount(query)
	if err != nil {
		return nil, err
	}
	vals, err := valuesFromNamed(args)
	if err != nil {
		return nil, err
	}
	return dc.queryStmt(stmt, vals)
}

func (dc *driverConn) execStmt(stmt Statement, vals []Value) (driver.Result, error) {
	switch stmt.(type) {
	case *BeginStmt:
		if err := dc.c.begin(); err != nil {
			return nil, err
		}
		return driverResult{}, nil
	case *CommitStmt:
		if err := dc.c.commit(); err != nil {
			return nil, err
		}
		return driverResult{}, nil
	case *RollbackStmt:
		if err := dc.c.rollback(); err != nil {
			return nil, err
		}
		return driverResult{}, nil
	}
	r, err := dc.c.exec(stmt, vals)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return driverResult{}, nil
	}
	return driverResult{lastID: r.LastInsertID, rows: r.RowsAffected}, nil
}

func (dc *driverConn) queryStmt(stmt Statement, vals []Value) (driver.Rows, error) {
	rs, err := dc.c.query(stmt, vals)
	if err != nil {
		return nil, err
	}
	if rs == nil {
		return nil, fmt.Errorf("sqlite: statement does not return rows")
	}
	return &driverRows{rs: rs}, nil
}

// driverStmt implements driver.Stmt.
type driverStmt struct {
	conn   *driverConn
	stmt   Statement
	nParam int
	query  string
}

var (
	_ driver.Stmt = (*driverStmt)(nil)
)

func (s *driverStmt) Close() error  { return nil }
func (s *driverStmt) NumInput() int { return s.nParam }

func (s *driverStmt) Exec(args []driver.Value) (driver.Result, error) {
	vals, err := valuesFromDriver(args)
	if err != nil {
		return nil, err
	}
	return s.conn.execStmt(s.stmt, vals)
}

func (s *driverStmt) Query(args []driver.Value) (driver.Rows, error) {
	vals, err := valuesFromDriver(args)
	if err != nil {
		return nil, err
	}
	return s.conn.queryStmt(s.stmt, vals)
}

// driverTx implements driver.Tx.
type driverTx struct{ conn *driverConn }

func (t *driverTx) Commit() error   { return t.conn.c.commit() }
func (t *driverTx) Rollback() error { return t.conn.c.rollback() }

// driverResult implements driver.Result.
type driverResult struct {
	lastID int64
	rows   int64
}

func (r driverResult) LastInsertId() (int64, error) { return r.lastID, nil }
func (r driverResult) RowsAffected() (int64, error) { return r.rows, nil }

// driverRows implements driver.Rows, streaming a materialized result set.
type driverRows struct {
	rs  *ResultSet
	pos int
}

func (r *driverRows) Columns() []string { return r.rs.Columns }
func (r *driverRows) Close() error      { return nil }

func (r *driverRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rs.Rows) {
		return io.EOF
	}
	row := r.rs.Rows[r.pos]
	r.pos++
	for i := range dest {
		if i < len(row) {
			dest[i] = row[i].GoValue()
		} else {
			dest[i] = nil
		}
	}
	return nil
}

// valuesFromDriver converts database/sql placeholder values into engine Values.
func valuesFromDriver(args []driver.Value) ([]Value, error) {
	vals := make([]Value, len(args))
	for i, a := range args {
		v, err := valueFromGo(a)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	return vals, nil
}

// valuesFromNamed converts context-API named values (ordered by Ordinal) into
// engine Values.
func valuesFromNamed(args []driver.NamedValue) ([]Value, error) {
	ordered := make([]driver.NamedValue, len(args))
	copy(ordered, args)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Ordinal < ordered[j].Ordinal })
	vals := make([]Value, len(ordered))
	for i, a := range ordered {
		v, err := valueFromGo(a.Value)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	return vals, nil
}

// parseWithCount parses a statement and reports the number of ? placeholders.
func parseWithCount(sql string) (Statement, int, error) {
	toks, err := tokenize(sql)
	if err != nil {
		return nil, 0, err
	}
	p := &parser{toks: toks}
	stmt, err := p.parseStatement()
	if err != nil {
		return nil, 0, err
	}
	if p.peek().typ == tokPunct && p.peek().text == ";" {
		p.advance()
	}
	if p.peek().typ != tokEOF {
		return nil, 0, fmt.Errorf("sqlite: unexpected trailing token %q", p.peek().text)
	}
	return stmt, p.nParam, nil
}
