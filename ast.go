package sqlite

// This file defines the abstract syntax tree produced by the parser and consumed
// by the executor.

// Statement is implemented by every top-level SQL statement.
type Statement interface{ stmtNode() }

// ColumnDef describes one column in a CREATE TABLE statement.
type ColumnDef struct {
	Name       string
	Type       ValueType
	PrimaryKey bool
	NotNull    bool
}

// CreateTableStmt represents CREATE TABLE.
type CreateTableStmt struct {
	Table       string
	IfNotExists bool
	Columns     []ColumnDef
}

// DropTableStmt represents DROP TABLE.
type DropTableStmt struct {
	Table    string
	IfExists bool
}

// InsertStmt represents INSERT INTO ... VALUES.
type InsertStmt struct {
	Table   string
	Columns []string // may be empty meaning "all columns in order"
	Rows    [][]Expr
}

// ResultColumn is one entry in a SELECT projection list.
type ResultColumn struct {
	Star  bool   // SELECT *
	Table string // for qualified star like t.*
	Expr  Expr   // expression when Star is false
	Alias string // optional AS alias
}

// OrderTerm is one ORDER BY term.
type OrderTerm struct {
	Expr Expr
	Desc bool
}

// JoinClause describes an INNER JOIN of the primary table with another.
type JoinClause struct {
	Table string
	Alias string
	On    Expr
}

// SelectStmt represents SELECT.
type SelectStmt struct {
	Distinct  bool
	Columns   []ResultColumn
	From      string
	FromAlias string
	Join      *JoinClause
	Where     Expr
	GroupBy   []Expr
	Having    Expr
	OrderBy   []OrderTerm
	Limit     Expr
	Offset    Expr
}

// UpdateStmt represents UPDATE ... SET ... WHERE.
type UpdateStmt struct {
	Table string
	Cols  []string
	Vals  []Expr
	Where Expr
}

// DeleteStmt represents DELETE FROM ... WHERE.
type DeleteStmt struct {
	Table string
	Where Expr
}

// BeginStmt, CommitStmt and RollbackStmt are transaction control statements.
type (
	// BeginStmt starts a transaction.
	BeginStmt struct{}
	// CommitStmt commits the current transaction.
	CommitStmt struct{}
	// RollbackStmt rolls back the current transaction.
	RollbackStmt struct{}
)

func (*CreateTableStmt) stmtNode() {}
func (*DropTableStmt) stmtNode()   {}
func (*InsertStmt) stmtNode()      {}
func (*SelectStmt) stmtNode()      {}
func (*UpdateStmt) stmtNode()      {}
func (*DeleteStmt) stmtNode()      {}
func (*BeginStmt) stmtNode()       {}
func (*CommitStmt) stmtNode()      {}
func (*RollbackStmt) stmtNode()    {}

// Expr is implemented by every expression node.
type Expr interface{ exprNode() }

// Literal is a constant value.
type Literal struct{ Val Value }

// Param is a ? placeholder; Index is its zero-based ordinal.
type Param struct{ Index int }

// ColumnRef references a column, optionally qualified by table/alias.
type ColumnRef struct {
	Table string
	Name  string
}

// UnaryExpr is a prefix operator (NOT, -, +).
type UnaryExpr struct {
	Op   string
	Expr Expr
}

// BinaryExpr is an infix operator (=, <, AND, +, ||, ...).
type BinaryExpr struct {
	Op    string
	Left  Expr
	Right Expr
}

// IsNullExpr represents "expr IS NULL" or "expr IS NOT NULL".
type IsNullExpr struct {
	Expr Expr
	Not  bool
}

// InExpr represents "expr IN (list)" or the negated form.
type InExpr struct {
	Expr Expr
	List []Expr
	Not  bool
}

// LikeExpr represents "expr LIKE pattern" (and NOT LIKE).
type LikeExpr struct {
	Expr    Expr
	Pattern Expr
	Not     bool
}

// FuncExpr is an aggregate or scalar function call.
type FuncExpr struct {
	Name     string // upper-cased
	Args     []Expr
	Star     bool // COUNT(*)
	Distinct bool
}

func (*Literal) exprNode()    {}
func (*Param) exprNode()      {}
func (*ColumnRef) exprNode()  {}
func (*UnaryExpr) exprNode()  {}
func (*BinaryExpr) exprNode() {}
func (*IsNullExpr) exprNode() {}
func (*InExpr) exprNode()     {}
func (*LikeExpr) exprNode()   {}
func (*FuncExpr) exprNode()   {}
