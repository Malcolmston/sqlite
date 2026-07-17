package sqlite

import (
	"fmt"
)

// conn is a single logical connection to a Database. It carries transaction
// state. A conn is not safe for concurrent use by multiple goroutines, matching
// the database/sql contract.
type conn struct {
	db     *Database
	inTx   bool
	backup map[string]*table // rollback snapshot while a transaction is open
}

// newConn creates a connection bound to the shared database for the DSN.
func newConn(dsn string) *conn {
	return &conn{db: sharedDatabase(dsn)}
}

// begin starts a transaction, taking a rollback snapshot and holding the
// database lock for the transaction's lifetime to provide serializable
// isolation.
func (c *conn) begin() error {
	if c.inTx {
		return fmt.Errorf("sqlite: nested transactions are not supported")
	}
	c.db.mu.Lock()
	c.backup = c.db.snapshot()
	c.inTx = true
	return nil
}

// commit finalizes the transaction, discarding the rollback snapshot.
func (c *conn) commit() error {
	if !c.inTx {
		return fmt.Errorf("sqlite: no transaction is active")
	}
	c.backup = nil
	c.inTx = false
	c.db.mu.Unlock()
	return nil
}

// rollback restores the pre-transaction snapshot.
func (c *conn) rollback() error {
	if !c.inTx {
		return fmt.Errorf("sqlite: no transaction is active")
	}
	c.db.restore(c.backup)
	c.backup = nil
	c.inTx = false
	c.db.mu.Unlock()
	return nil
}

// withLock runs fn while holding the database lock, unless a transaction is
// already active (in which case the lock is already held by this connection).
func (c *conn) withLock(fn func() error) error {
	if c.inTx {
		return fn()
	}
	c.db.mu.Lock()
	defer c.db.mu.Unlock()
	return fn()
}

// exec runs a non-query statement.
func (c *conn) exec(stmt Statement, args []Value) (*ExecResult, error) {
	var res *ExecResult
	err := c.withLock(func() error {
		// Transaction control statements are handled here so that BEGIN/COMMIT/
		// ROLLBACK issued as plain Exec calls work too.
		switch stmt.(type) {
		case *BeginStmt, *CommitStmt, *RollbackStmt:
			return fmt.Errorf("sqlite: use the transaction API for BEGIN/COMMIT/ROLLBACK")
		}
		r, _, err := c.db.execStatement(stmt, args)
		res = r
		return err
	})
	return res, err
}

// query runs a SELECT statement.
func (c *conn) query(stmt Statement, args []Value) (*ResultSet, error) {
	var rs *ResultSet
	err := c.withLock(func() error {
		_, r, err := c.db.execStatement(stmt, args)
		rs = r
		return err
	})
	return rs, err
}

// Exec parses and executes a non-query SQL statement against the database
// directly (bypassing database/sql). It is convenient for setup code and tests.
func (db *Database) Exec(sql string, args ...interface{}) (ExecResult, error) {
	vals, err := toValues(args)
	if err != nil {
		return ExecResult{}, err
	}
	stmt, err := Parse(sql)
	if err != nil {
		return ExecResult{}, err
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	r, _, err := db.execStatement(stmt, vals)
	if err != nil {
		return ExecResult{}, err
	}
	if r == nil {
		return ExecResult{}, nil
	}
	return *r, nil
}

// Query parses and executes a SELECT statement, returning the full result set.
func (db *Database) Query(sql string, args ...interface{}) (*ResultSet, error) {
	vals, err := toValues(args)
	if err != nil {
		return nil, err
	}
	stmt, err := Parse(sql)
	if err != nil {
		return nil, err
	}
	if _, ok := stmt.(*SelectStmt); !ok {
		return nil, fmt.Errorf("sqlite: Query requires a SELECT statement")
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	_, rs, err := db.execStatement(stmt, vals)
	return rs, err
}

func toValues(args []interface{}) ([]Value, error) {
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
