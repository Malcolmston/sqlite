package sqlite

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// ValueType enumerates the storage classes supported by the engine. They mirror
// SQLite's dynamic type system: every Value carries its own type tag.
type ValueType int

const (
	// TypeNull is the SQL NULL value.
	TypeNull ValueType = iota
	// TypeInteger is a 64-bit signed integer.
	TypeInteger
	// TypeReal is a 64-bit IEEE-754 floating point number.
	TypeReal
	// TypeText is a UTF-8 string.
	TypeText
	// TypeBlob is an arbitrary byte string.
	TypeBlob
)

func (t ValueType) String() string {
	switch t {
	case TypeNull:
		return "NULL"
	case TypeInteger:
		return "INTEGER"
	case TypeReal:
		return "REAL"
	case TypeText:
		return "TEXT"
	case TypeBlob:
		return "BLOB"
	default:
		return "UNKNOWN"
	}
}

// Value is a single typed SQL value. The zero Value is SQL NULL.
type Value struct {
	Type ValueType
	i    int64
	f    float64
	s    string
	b    []byte
}

// Null returns a NULL value.
func Null() Value { return Value{Type: TypeNull} }

// Int returns an INTEGER value.
func Int(v int64) Value { return Value{Type: TypeInteger, i: v} }

// Real returns a REAL value.
func Real(v float64) Value { return Value{Type: TypeReal, f: v} }

// Text returns a TEXT value.
func Text(v string) Value { return Value{Type: TypeText, s: v} }

// Blob returns a BLOB value.
func Blob(v []byte) Value { return Value{Type: TypeBlob, b: v} }

// IsNull reports whether the value is SQL NULL.
func (v Value) IsNull() bool { return v.Type == TypeNull }

// Int64 returns the integer content of the value.
func (v Value) Int64() int64 { return v.i }

// Float64 returns the real content of the value.
func (v Value) Float64() float64 { return v.f }

// Str returns the text content of the value.
func (v Value) Str() string { return v.s }

// Bytes returns the blob content of the value.
func (v Value) Bytes() []byte { return v.b }

// GoValue converts the Value into a plain Go value suitable for database/sql
// scanning: nil, int64, float64, string, or []byte.
func (v Value) GoValue() interface{} {
	switch v.Type {
	case TypeNull:
		return nil
	case TypeInteger:
		return v.i
	case TypeReal:
		return v.f
	case TypeText:
		return v.s
	case TypeBlob:
		return v.b
	default:
		return nil
	}
}

// String renders the value for display and debugging.
func (v Value) String() string {
	switch v.Type {
	case TypeNull:
		return "NULL"
	case TypeInteger:
		return strconv.FormatInt(v.i, 10)
	case TypeReal:
		return strconv.FormatFloat(v.f, 'g', -1, 64)
	case TypeText:
		return v.s
	case TypeBlob:
		return string(v.b)
	default:
		return "?"
	}
}

// valueFromGo builds a Value from an arbitrary Go value, as delivered through
// database/sql placeholder arguments.
func valueFromGo(x interface{}) (Value, error) {
	switch t := x.(type) {
	case nil:
		return Null(), nil
	case bool:
		if t {
			return Int(1), nil
		}
		return Int(0), nil
	case int:
		return Int(int64(t)), nil
	case int8:
		return Int(int64(t)), nil
	case int16:
		return Int(int64(t)), nil
	case int32:
		return Int(int64(t)), nil
	case int64:
		return Int(t), nil
	case uint:
		return Int(int64(t)), nil
	case uint8:
		return Int(int64(t)), nil
	case uint16:
		return Int(int64(t)), nil
	case uint32:
		return Int(int64(t)), nil
	case uint64:
		return Int(int64(t)), nil
	case float32:
		return Real(float64(t)), nil
	case float64:
		return Real(t), nil
	case string:
		return Text(t), nil
	case []byte:
		return Blob(t), nil
	case time.Time:
		return Text(t.Format(time.RFC3339Nano)), nil
	default:
		return Null(), fmt.Errorf("sqlite: unsupported argument type %T", x)
	}
}

// isNumeric reports whether the value is INTEGER or REAL.
func (v Value) isNumeric() bool {
	return v.Type == TypeInteger || v.Type == TypeReal
}

// asFloat returns the value's numeric content as a float64.
func (v Value) asFloat() float64 {
	switch v.Type {
	case TypeInteger:
		return float64(v.i)
	case TypeReal:
		return v.f
	default:
		return 0
	}
}

// truthy reports whether the value is considered true in a boolean context.
// NULL is not truthy.
func (v Value) truthy() bool {
	switch v.Type {
	case TypeNull:
		return false
	case TypeInteger:
		return v.i != 0
	case TypeReal:
		return v.f != 0
	case TypeText:
		return v.s != ""
	case TypeBlob:
		return len(v.b) != 0
	default:
		return false
	}
}

// typeRank orders storage classes for cross-type comparison, following SQLite:
// NULL < numbers < text < blob.
func typeRank(t ValueType) int {
	switch t {
	case TypeNull:
		return 0
	case TypeInteger, TypeReal:
		return 1
	case TypeText:
		return 2
	case TypeBlob:
		return 3
	default:
		return 4
	}
}

// compare orders two non-NULL values, returning -1, 0 or 1. NULL handling is the
// caller's responsibility.
func compare(a, b Value) int {
	ra, rb := typeRank(a.Type), typeRank(b.Type)
	if ra != rb {
		if ra < rb {
			return -1
		}
		return 1
	}
	switch a.Type {
	case TypeInteger, TypeReal:
		af, bf := a.asFloat(), b.asFloat()
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		default:
			return 0
		}
	case TypeText:
		return strings.Compare(a.s, b.s)
	case TypeBlob:
		return bytes.Compare(a.b, b.b)
	default:
		return 0
	}
}

// equalStrict reports value equality treating NULL as equal to NULL. It is used
// for GROUP BY key comparison and DISTINCT-like semantics.
func equalStrict(a, b Value) bool {
	if a.Type == TypeNull || b.Type == TypeNull {
		return a.Type == TypeNull && b.Type == TypeNull
	}
	return compare(a, b) == 0
}

// numericAdd adds two numeric values, promoting to REAL when either operand is
// REAL.
func numericAdd(a, b Value) Value {
	if a.Type == TypeInteger && b.Type == TypeInteger {
		return Int(a.i + b.i)
	}
	return Real(a.asFloat() + b.asFloat())
}

// coerceToType converts a value toward a declared column type where it is safe
// and lossless-ish, mirroring SQLite's type affinity in a simplified form.
func coerceToType(v Value, target ValueType) Value {
	if v.Type == TypeNull {
		return v
	}
	switch target {
	case TypeInteger:
		switch v.Type {
		case TypeReal:
			if v.f == math.Trunc(v.f) {
				return Int(int64(v.f))
			}
		case TypeText:
			if n, err := strconv.ParseInt(strings.TrimSpace(v.s), 10, 64); err == nil {
				return Int(n)
			}
		}
	case TypeReal:
		switch v.Type {
		case TypeInteger:
			return Real(float64(v.i))
		case TypeText:
			if f, err := strconv.ParseFloat(strings.TrimSpace(v.s), 64); err == nil {
				return Real(f)
			}
		}
	case TypeText:
		if v.Type != TypeText && v.Type != TypeBlob {
			return Text(v.String())
		}
	}
	return v
}
