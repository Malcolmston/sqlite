package sqlite

import (
	"testing"
)

// valuesEqual compares two values by type and content for test assertions.
func valuesEqual(a, b Value) bool {
	if a.Type != b.Type {
		return false
	}
	switch a.Type {
	case TypeNull:
		return true
	case TypeInteger:
		return a.i == b.i
	case TypeReal:
		return a.f == b.f
	case TypeText:
		return a.s == b.s
	case TypeBlob:
		return string(a.b) == string(b.b)
	default:
		return false
	}
}

func TestScalarFuncsKnownValues(t *testing.T) {
	tests := []struct {
		name string
		got  Value
		want Value
	}{
		{"abs_neg_int", Abs(Int(-5)), Int(5)},
		{"abs_pos_int", Abs(Int(7)), Int(7)},
		{"abs_neg_real", Abs(Real(-2.5)), Real(2.5)},
		{"abs_null", Abs(Null()), Null()},
		{"abs_text_num", Abs(Text("-12")), Int(12)},
		{"abs_text_bad", Abs(Text("abc")), Real(0)},

		{"length_text", Length(Text("hello")), Int(5)},
		{"length_unicode", Length(Text("héllo")), Int(5)},
		{"length_int", Length(Int(12345)), Int(5)},
		{"length_blob", Length(Blob([]byte{1, 2, 3})), Int(3)},
		{"length_null", Length(Null()), Null()},

		{"upper", Upper(Text("aBc1")), Text("ABC1")},
		{"upper_null", Upper(Null()), Null()},
		{"lower", Lower(Text("aBcD")), Text("abcd")},

		{"trim_default", Trim(Text("  hi  ")), Text("hi")},
		{"ltrim_default", LTrim(Text("  hi  ")), Text("hi  ")},
		{"rtrim_default", RTrim(Text("  hi  ")), Text("  hi")},
		{"trim_chars", Trim(Text("xxhixx"), Text("x")), Text("hi")},
		{"trim_null", Trim(Null()), Null()},

		{"substr_3", Substr(Text("hello"), Int(1), Int(3)), Text("hel")},
		{"substr_2", Substr(Text("hello"), Int(2), Null()), Text("ello")},
		{"substr_neg", Substr(Text("hello"), Int(-3), Int(2)), Text("ll")},
		{"substr_neg_end", Substr(Text("hello"), Int(-3), Null()), Text("llo")},
		{"substr_unicode", Substr(Text("héllo"), Int(2), Int(2)), Text("él")},
		{"substr_null", Substr(Null(), Int(1), Int(1)), Null()},

		{"replace", Replace(Text("a-b-c"), Text("-"), Text("+")), Text("a+b+c")},
		{"replace_empty_from", Replace(Text("abc"), Text(""), Text("x")), Text("abc")},
		{"replace_null", Replace(Null(), Text("a"), Text("b")), Null()},

		{"round_2", Round(Real(3.14159), Int(2)), Real(3.14)},
		{"round_0", Round(Real(2.5), Null()), Real(3)},
		{"round_neg_half", Round(Real(-2.5), Null()), Real(-3)},
		{"round_down", Round(Real(2.4), Null()), Real(2)},
		{"round_1", Round(Real(123.456), Int(1)), Real(123.5)},

		{"coalesce_first", Coalesce(Null(), Int(2), Int(3)), Int(2)},
		{"coalesce_all_null", Coalesce(Null(), Null()), Null()},
		{"ifnull_a", IfNull(Int(1), Int(2)), Int(1)},
		{"ifnull_b", IfNull(Null(), Int(2)), Int(2)},
		{"nullif_eq", NullIf(Int(5), Int(5)), Null()},
		{"nullif_ne", NullIf(Int(5), Int(6)), Int(5)},

		{"typeof_int", TypeOf(Int(1)), Text("integer")},
		{"typeof_real", TypeOf(Real(1)), Text("real")},
		{"typeof_text", TypeOf(Text("x")), Text("text")},
		{"typeof_blob", TypeOf(Blob([]byte{1})), Text("blob")},
		{"typeof_null", TypeOf(Null()), Text("null")},

		{"hex_text", Hex(Text("abc")), Text("616263")},
		{"hex_blob", Hex(Blob([]byte{0xde, 0xad})), Text("DEAD")},
		{"hex_null", Hex(Null()), Text("")},

		{"quote_text", Quote(Text("a'b")), Text("'a''b'")},
		{"quote_null", Quote(Null()), Text("NULL")},
		{"quote_int", Quote(Int(42)), Text("42")},
		{"quote_blob", Quote(Blob([]byte{0x01, 0xff})), Text("X'01FF'")},

		{"instr_found", Instr(Text("hello"), Text("ll")), Int(3)},
		{"instr_missing", Instr(Text("hello"), Text("z")), Int(0)},
		{"instr_unicode", Instr(Text("héllo"), Text("llo")), Int(3)},
		{"instr_null", Instr(Null(), Text("x")), Null()},

		{"unicode_A", Unicode(Text("A")), Int(65)},
		{"unicode_empty", Unicode(Text("")), Null()},
		{"char_AB", Char(Int(65), Int(66)), Text("AB")},
		{"char_none", Char(), Text("")},

		{"sign_pos", Sign(Real(3.2)), Int(1)},
		{"sign_neg", Sign(Int(-4)), Int(-1)},
		{"sign_zero", Sign(Int(0)), Int(0)},
		{"sign_null", Sign(Null()), Null()},
		{"sign_bad", Sign(Text("abc")), Null()},
	}
	for _, tc := range tests {
		if !valuesEqual(tc.got, tc.want) {
			t.Errorf("%s: got %v (%s), want %v (%s)", tc.name, tc.got.GoValue(), tc.got.Type, tc.want.GoValue(), tc.want.Type)
		}
	}
}

func TestGlobKnownValues(t *testing.T) {
	tests := []struct {
		pattern, text string
		want          bool
	}{
		{"*.txt", "file.txt", true},
		{"*.txt", "file.md", false},
		{"f?o", "foo", true},
		{"f?o", "fao", true},
		{"f?o", "fo", false},
		{"[a-c]*", "banana", true},
		{"[a-c]*", "durian", false},
		{"[^a-c]*", "durian", true},
		{"A*", "apple", false},
		{"a*e", "apple", true},
		{"", "", true},
		{"*", "anything", true},
		{"h[ae]llo", "hello", true},
		{"h[ae]llo", "hallo", true},
		{"h[ae]llo", "hillo", false},
	}
	for _, tc := range tests {
		if got := Glob(tc.pattern, tc.text); got != tc.want {
			t.Errorf("Glob(%q,%q)=%v, want %v", tc.pattern, tc.text, got, tc.want)
		}
	}
}

func TestCallScalarAndRegistry(t *testing.T) {
	v, err := CallScalar("upper", []Value{Text("go")})
	if err != nil || !valuesEqual(v, Text("GO")) {
		t.Fatalf("CallScalar upper: v=%v err=%v", v, err)
	}
	if _, err := CallScalar("abs", nil); err == nil {
		t.Errorf("expected arity error for abs()")
	}
	if _, err := CallScalar("nope", nil); err == nil {
		t.Errorf("expected unknown-function error")
	}
	if !IsScalarFunc("SUBSTR") || IsScalarFunc("bogus") {
		t.Errorf("IsScalarFunc mismatch")
	}
	names := ScalarNames()
	if len(names) == 0 {
		t.Fatalf("ScalarNames empty")
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("ScalarNames not sorted: %v", names)
		}
	}
	if fn, ok := LookupScalar("length"); !ok {
		t.Errorf("LookupScalar length missing")
	} else if r, _ := fn([]Value{Text("abcd")}); !valuesEqual(r, Int(4)) {
		t.Errorf("length via LookupScalar = %v", r.GoValue())
	}
}

// TestScalarFuncsEndToEnd exercises scalar functions through the SQL engine to
// verify wiring in both the WHERE (engine.eval) and SELECT/aggregate paths.
func TestScalarFuncsEndToEnd(t *testing.T) {
	db := NewDatabase()
	if _, err := db.Exec(`CREATE TABLE t (name TEXT, n INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO t (name, n) VALUES (?, ?), (?, ?), (?, ?)`,
		"  apple  ", -3, "Banana", 7, "cherry", -1); err != nil {
		t.Fatal(err)
	}

	// Scalar function in the projection.
	rs, err := db.Query(`SELECT UPPER(TRIM(name)), ABS(n) FROM t ORDER BY n`)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		s string
		n int64
	}{{"APPLE", 3}, {"CHERRY", 1}, {"BANANA", 7}}
	if len(rs.Rows) != 3 {
		t.Fatalf("got %d rows", len(rs.Rows))
	}
	for i, row := range rs.Rows {
		if row[0].Str() != want[i].s || row[1].Int64() != want[i].n {
			t.Errorf("row %d = (%q,%d), want (%q,%d)", i, row[0].Str(), row[1].Int64(), want[i].s, want[i].n)
		}
	}

	// Scalar function in the WHERE clause.
	rs, err = db.Query(`SELECT name FROM t WHERE LENGTH(TRIM(name)) = ?`, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.Rows) != 1 || rs.Rows[0][0].Str() != "  apple  " {
		t.Fatalf("WHERE length filter = %v", rs.Rows)
	}

	// Scalar wrapping an aggregate.
	rs, err = db.Query(`SELECT ABS(SUM(n)) FROM t`)
	if err != nil {
		t.Fatal(err)
	}
	if got := rs.Rows[0][0].Int64(); got != 3 {
		t.Fatalf("ABS(SUM(n)) = %d, want 3", got)
	}
}

func BenchmarkGlob(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Glob("[a-c]*n?na", "banana")
	}
}

func BenchmarkCallScalar(b *testing.B) {
	args := []Value{Text("  hello world  ")}
	for i := 0; i < b.N; i++ {
		_, _ = CallScalar("trim", args)
	}
}

func BenchmarkSubstr(b *testing.B) {
	v := Text("the quick brown fox jumps over the lazy dog")
	for i := 0; i < b.N; i++ {
		Substr(v, Int(5), Int(11))
	}
}
