package sqlite

// Upstream-parity tests.
//
// These tests encode concrete known-answer vectors taken directly from the
// SQLite project's own regression suite (github.com/sqlite/sqlite, files
// test/func.test and test/expr.test). Each vector is the exact SQL and the
// exact result that upstream SQLite asserts, re-expressed as a deterministic
// assertion against this port's public Database API. They exist to keep the
// pure-Go engine's scalar-function and expression behaviour aligned with the
// original C library.

import (
	"math"
	"testing"
)

// scalar runs a single-column, single-row query and returns the lone Value.
func scalar(t *testing.T, db *Database, sql string) Value {
	t.Helper()
	rs, err := db.Query(sql)
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", sql, err)
	}
	if len(rs.Rows) != 1 || len(rs.Rows[0]) != 1 {
		t.Fatalf("%s: expected 1x1 result, got %dx%d", sql, len(rs.Rows), colCount(rs))
	}
	return rs.Rows[0][0]
}

func colCount(rs *ResultSet) int {
	if len(rs.Rows) == 0 {
		return 0
	}
	return len(rs.Rows[0])
}

// wantText asserts that sql yields a single TEXT value equal to want.
func wantText(t *testing.T, db *Database, sql, want string) {
	t.Helper()
	v := scalar(t, db, sql)
	if v.Type != TypeText || v.Str() != want {
		t.Errorf("%s = (%s %q); want (TEXT %q)", sql, v.Type, v.String(), want)
	}
}

// wantInt asserts that sql yields a single INTEGER value equal to want.
func wantInt(t *testing.T, db *Database, sql string, want int64) {
	t.Helper()
	v := scalar(t, db, sql)
	if v.Type != TypeInteger || v.Int64() != want {
		t.Errorf("%s = (%s %q); want (INTEGER %d)", sql, v.Type, v.String(), want)
	}
}

// wantReal asserts that sql yields a REAL value within a tight tolerance.
func wantReal(t *testing.T, db *Database, sql string, want float64) {
	t.Helper()
	v := scalar(t, db, sql)
	if v.Type != TypeReal {
		t.Errorf("%s type = %s; want REAL", sql, v.Type)
		return
	}
	got := v.Float64()
	if math.IsInf(want, 0) || want == 0 {
		if got != want {
			t.Errorf("%s = %v; want %v", sql, got, want)
		}
		return
	}
	if math.Abs(got-want) > math.Abs(want)*1e-12 {
		t.Errorf("%s = %v; want %v", sql, got, want)
	}
}

// wantNull asserts that sql yields SQL NULL.
func wantNull(t *testing.T, db *Database, sql string) {
	t.Helper()
	v := scalar(t, db, sql)
	if v.Type != TypeNull {
		t.Errorf("%s = (%s %q); want NULL", sql, v.Type, v.String())
	}
}

// wantErr asserts that sql fails to evaluate.
func wantErr(t *testing.T, db *Database, sql string) {
	t.Helper()
	if _, err := db.Query(sql); err == nil {
		t.Errorf("%s: expected an error, got none", sql)
	}
}

// TestParitySubstr mirrors func.test func-2.x — substr(X,Y,Z) over characters,
// including negative and zero start positions.
func TestParitySubstr(t *testing.T) {
	db := NewDatabase()
	wantText(t, db, "SELECT substr('abcdefg',1,2)", "ab")
	wantText(t, db, "SELECT substr('abcdefg',2,1)", "b")
	wantText(t, db, "SELECT substr('abcdefg',3,3)", "cde")
	wantText(t, db, "SELECT substr('abcdefg',-1,1)", "g")
	wantText(t, db, "SELECT substr('abcdefg',-2,1)", "f")
	wantText(t, db, "SELECT substr('abcdefg',-2,2)", "fg")
	wantText(t, db, "SELECT substr('abcdefg',2)", "bcdefg")
	// substring is a documented alias for substr.
	wantText(t, db, "SELECT substring('abcdefg',3,2)", "cd")
}

// TestParityRound mirrors func.test func-4.x — round() is always REAL and uses
// half-away-from-zero rounding, with the precision argument capped.
func TestParityRound(t *testing.T) {
	db := NewDatabase()
	wantText(t, db, "SELECT typeof(round(5.1,1))", "real")
	wantText(t, db, "SELECT typeof(round(5.1))", "real")
	wantReal(t, db, "SELECT round(40223.4999999999)", 40223.0)
	wantReal(t, db, "SELECT round(40224.4999999999)", 40224.0)
	wantReal(t, db, "SELECT round(40225.4999999999)", 40225.0)
	wantReal(t, db, "SELECT round(40223.4999999999,15)", 40223.4999999999)
	wantReal(t, db, "SELECT round(1234567890.5)", 1234567891.0)
	wantReal(t, db, "SELECT round(1234567890123.35,1)", 1234567890123.4)
	wantReal(t, db, "SELECT round(1234567890123.445,2)", 1234567890123.45)
	wantReal(t, db, "SELECT round(9999999999999.55,1)", 9999999999999.6)
	// func-4.40: an absurd precision is clamped and returns the value unchanged.
	wantReal(t, db, "SELECT round(123.456 , 4294967297)", 123.456)
}

// TestParityRoundInfinity mirrors func.test func-4.39 — rounding ±Inf yields
// ±Inf, and oversized float literals parse to ±Inf.
func TestParityRoundInfinity(t *testing.T) {
	db := NewDatabase()
	wantReal(t, db, "SELECT round(1e500)", math.Inf(1))
	wantReal(t, db, "SELECT round(-1e500)", math.Inf(-1))
}

// TestParityAbs mirrors func.test func-4.x and func-18.x — abs() including the
// most-negative-integer overflow case.
func TestParityAbs(t *testing.T) {
	db := NewDatabase()
	wantInt(t, db, "SELECT abs(-2)", 2)
	wantInt(t, db, "SELECT abs(2)", 2)
	wantReal(t, db, "SELECT abs(-12345.6789)", 12345.6789)
	// func-18.31: abs of the largest-magnitude representable negative integer.
	wantInt(t, db, "SELECT abs(-9223372036854775807)", 9223372036854775807)
	// func-18.32: abs(-9223372036854775808) overflows.
	wantErr(t, db, "SELECT abs(-9223372036854775807-1)")
	// func-4.2 / func-4.1: wrong arity is an error.
	wantErr(t, db, "SELECT abs()")
	wantErr(t, db, "SELECT abs(1,2)")
}

// TestParityCoalesceNullif mirrors func.test func-6.x.
func TestParityCoalesceNullif(t *testing.T) {
	db := NewDatabase()
	wantText(t, db, "SELECT coalesce(nullif(1,1),'nil')", "nil")
	wantInt(t, db, "SELECT coalesce(nullif(1,2),'nil')", 1)
	wantInt(t, db, "SELECT coalesce(nullif(1,NULL),'nil')", 1)
	wantInt(t, db, "SELECT ifnull(NULL,7)", 7)
	wantInt(t, db, "SELECT ifnull(3,7)", 3)
}

// TestParityReplace mirrors func.test func-21.x — replace() with NULL and empty
// arguments.
func TestParityReplace(t *testing.T) {
	db := NewDatabase()
	wantErr(t, db, "SELECT replace(1,2)")
	wantErr(t, db, "SELECT replace(1,2,3,4)")
	wantNull(t, db, "SELECT replace('This is the main test string', NULL, 'ALT')")
	wantNull(t, db, "SELECT replace(NULL, 'main', 'ALT')")
	wantNull(t, db, "SELECT replace('This is the main test string', 'main', NULL)")
	wantText(t, db, "SELECT replace('This is the main test string', 'main', 'ALT')", "This is the ALT test string")
	wantText(t, db, "SELECT replace('This is the main test string', 'main', 'larger-main')", "This is the larger-main test string")
	wantText(t, db, "SELECT replace('aaaaaaa', 'a', '0123456789')", "0123456789012345678901234567890123456789012345678901234567890123456789")
	wantText(t, db, "SELECT typeof(replace(1,'',0))", "text")
}

// TestParityTrim mirrors func.test func-22.x.
func TestParityTrim(t *testing.T) {
	db := NewDatabase()
	wantErr(t, db, "SELECT trim(1,2,3)")
	wantErr(t, db, "SELECT ltrim(1,2,3)")
	wantErr(t, db, "SELECT rtrim(1,2,3)")
	wantText(t, db, "SELECT trim('  hi  ')", "hi")
	wantText(t, db, "SELECT ltrim('  hi  ')", "hi  ")
	wantText(t, db, "SELECT rtrim('  hi  ')", "  hi")
	wantText(t, db, "SELECT trim('  hi  ','xyz')", "  hi  ")
	wantText(t, db, "SELECT trim('xyxzy  hi  zzzy','xyz')", "  hi  ")
	wantText(t, db, "SELECT ltrim('xyxzy  hi  zzzy','xyz')", "  hi  zzzy")
	wantText(t, db, "SELECT rtrim('xyxzy  hi  zzzy','xyz')", "xyxzy  hi  ")
	wantText(t, db, "SELECT trim('  hi  ','')", "  hi  ")
	wantNull(t, db, "SELECT trim(NULL)")
}

// TestParityHex mirrors func.test func-9.x (UTF-8 encoding).
func TestParityHex(t *testing.T) {
	db := NewDatabase()
	wantText(t, db, "SELECT hex(x'00112233445566778899aAbBcCdDeEfF')", "00112233445566778899AABBCCDDEEFF")
	wantText(t, db, "SELECT hex(replace('abcdefg','ef','12'))", "61626364313267")
	wantText(t, db, "SELECT hex(replace('abcdefg','','12'))", "61626364656667")
	wantText(t, db, "SELECT hex(replace('aabcdefg','a','aaa'))", "616161616161626364656667")
}

// TestParityQuote mirrors func.test func-16.x — quote() including the ±Inf
// sentinel literals.
func TestParityQuote(t *testing.T) {
	db := NewDatabase()
	wantText(t, db, "SELECT quote(4.2e+859)", "9.0e+999")
	wantText(t, db, "SELECT quote(-7.8e+904)", "-9.0e+999")
	wantText(t, db, "SELECT quote('a''b')", "'a''b'")
	wantText(t, db, "SELECT quote(x'6162')", "X'6162'")
	wantText(t, db, "SELECT quote(NULL)", "NULL")
}

// TestParityUpperLower mirrors func.test func-5.x — ASCII-only case folding.
func TestParityUpperLower(t *testing.T) {
	db := NewDatabase()
	wantText(t, db, "SELECT upper('This Program Is Free Software')", "THIS PROGRAM IS FREE SOFTWARE")
	wantText(t, db, "SELECT lower('This Program Is Free Software')", "this program is free software")
}

// TestParityUnicodeChar mirrors func.test func-30.x — unicode()/char() round
// trips over code points.
func TestParityUnicodeChar(t *testing.T) {
	db := NewDatabase()
	wantInt(t, db, "SELECT unicode('$')", 36)
	wantInt(t, db, "SELECT unicode('¢')", 162)
	wantInt(t, db, "SELECT unicode('€')", 8364)
	wantText(t, db, "SELECT char(36,162,8364)", "$¢€")
	wantInt(t, db, "SELECT unicode(char(8364))", 8364)
}

// TestParityLength mirrors func.test func-1.x and func-8.x — length() counts
// characters, not bytes.
func TestParityLength(t *testing.T) {
	db := NewDatabase()
	wantInt(t, db, "SELECT length('abc')", 3)
	wantInt(t, db, "SELECT length(char(350,351,352,353,354))", 5)
	wantNull(t, db, "SELECT length(NULL)")
}
