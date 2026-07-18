package sqlite

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// ScalarFunc is the signature of a SQL scalar function: it receives the already
// evaluated argument values and returns a single result value. Implementations
// must be deterministic and must follow SQLite's three-valued NULL logic where
// applicable.
type ScalarFunc func(args []Value) (Value, error)

// LookupScalar returns the built-in scalar function registered under the given
// name together with true, or (nil, false) if no such function exists. Names are
// matched case-insensitively, mirroring SQLite where SQL function names are not
// case sensitive.
func LookupScalar(name string) (ScalarFunc, bool) {
	fn, ok := funcScalarRegistry[strings.ToUpper(name)]
	return fn, ok
}

// CallScalar evaluates the named built-in scalar function with the supplied
// arguments. It returns an error if the function is unknown or if the wrong
// number of arguments is supplied. Names are matched case-insensitively.
func CallScalar(name string, args []Value) (Value, error) {
	fn, ok := LookupScalar(name)
	if !ok {
		return Null(), &FuncError{Func: name, Msg: "no such function"}
	}
	return fn(args)
}

// IsScalarFunc reports whether name refers to a registered built-in scalar
// function (case-insensitive).
func IsScalarFunc(name string) bool {
	_, ok := LookupScalar(name)
	return ok
}

// ScalarNames returns the sorted, upper-cased names of every registered built-in
// scalar function. The returned slice is freshly allocated on each call and may
// be modified by the caller.
func ScalarNames() []string {
	names := make([]string, 0, len(funcScalarRegistry))
	for n := range funcScalarRegistry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// FuncError describes a failure raised while invoking a built-in SQL function,
// such as an unknown name or an incorrect number of arguments.
type FuncError struct {
	Func string // the function name as requested
	Msg  string // a human-readable description of the problem
}

// Error implements the error interface.
func (e *FuncError) Error() string {
	return "sqlite: " + e.Func + "(): " + e.Msg
}

// funcArity builds a FuncError describing a wrong-argument-count condition.
func funcArity(name string) error {
	return &FuncError{Func: name, Msg: "wrong number of arguments"}
}

// --- numeric / byte coercion helpers (SQLite-style affinity) -----------------

// funcNumeric applies numeric affinity to v, returning an INTEGER or REAL Value
// and true, or (NULL, false) when v cannot be interpreted as a number.
func funcNumeric(v Value) (Value, bool) {
	switch v.Type {
	case TypeInteger, TypeReal:
		return v, true
	case TypeText:
		s := strings.TrimSpace(v.s)
		if s == "" {
			return Null(), false
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return Int(n), true
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return Real(f), true
		}
		return Null(), false
	default:
		return Null(), false
	}
}

// funcToInt converts v to an int64, truncating REAL toward zero and applying
// numeric affinity to TEXT. Non-numeric input yields 0.
func funcToInt(v Value) int64 {
	switch v.Type {
	case TypeInteger:
		return v.i
	case TypeReal:
		return int64(v.f)
	case TypeText:
		if nv, ok := funcNumeric(v); ok {
			return funcToInt(nv)
		}
		return 0
	default:
		return 0
	}
}

// funcBytes returns the raw bytes of v: the blob content for BLOBs, otherwise the
// UTF-8 encoding of the value's textual rendering.
func funcBytes(v Value) []byte {
	if v.Type == TypeBlob {
		return v.b
	}
	return []byte(v.String())
}

// funcASCIIUpper upper-cases only ASCII letters, matching SQLite's default
// upper() which does not fold non-ASCII characters.
func funcASCIIUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// funcASCIILower lower-cases only ASCII letters, matching SQLite's default
// lower().
func funcASCIILower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// --- exported scalar functions ----------------------------------------------

// Abs returns the absolute value of v, following SQLite's abs(): NULL yields
// NULL, non-numeric text yields REAL 0, and the most-negative integer is
// returned as a REAL to avoid overflow.
func Abs(v Value) Value {
	if v.IsNull() {
		return Null()
	}
	switch v.Type {
	case TypeInteger:
		if v.i == math.MinInt64 {
			return Real(math.Abs(float64(v.i)))
		}
		if v.i < 0 {
			return Int(-v.i)
		}
		return Int(v.i)
	case TypeReal:
		return Real(math.Abs(v.f))
	default:
		nv, ok := funcNumeric(v)
		if !ok {
			return Real(0)
		}
		return Abs(nv)
	}
}

// Length returns the length of v, following SQLite's length(): NULL yields NULL,
// BLOBs report their byte count, and every other type reports the number of
// Unicode characters (runes) in its textual rendering.
func Length(v Value) Value {
	if v.IsNull() {
		return Null()
	}
	if v.Type == TypeBlob {
		return Int(int64(len(v.b)))
	}
	return Int(int64(len([]rune(v.String()))))
}

// Upper returns v converted to upper case (ASCII letters only, like SQLite's
// upper()). NULL yields NULL.
func Upper(v Value) Value {
	if v.IsNull() {
		return Null()
	}
	return Text(funcASCIIUpper(v.String()))
}

// Lower returns v converted to lower case (ASCII letters only, like SQLite's
// lower()). NULL yields NULL.
func Lower(v Value) Value {
	if v.IsNull() {
		return Null()
	}
	return Text(funcASCIILower(v.String()))
}

// funcTrim implements the shared body of Trim/LTrim/RTrim.
func funcTrim(v Value, chars []Value, left, right bool) Value {
	if v.IsNull() {
		return Null()
	}
	cutset := " "
	if len(chars) > 0 {
		if chars[0].IsNull() {
			return Null()
		}
		cutset = chars[0].String()
	}
	s := v.String()
	if cutset == "" {
		return Text(s)
	}
	switch {
	case left && right:
		s = strings.Trim(s, cutset)
	case left:
		s = strings.TrimLeft(s, cutset)
	case right:
		s = strings.TrimRight(s, cutset)
	}
	return Text(s)
}

// Trim removes leading and trailing characters from v. With no extra argument it
// strips spaces; when a single characters value is supplied it strips any of the
// characters contained in that value, matching SQLite's trim(X) and trim(X,Y).
// NULL input (or a NULL character set) yields NULL.
func Trim(v Value, chars ...Value) Value { return funcTrim(v, chars, true, true) }

// LTrim removes leading characters from v, mirroring SQLite's ltrim(X) and
// ltrim(X,Y). See [Trim] for the argument semantics.
func LTrim(v Value, chars ...Value) Value { return funcTrim(v, chars, true, false) }

// RTrim removes trailing characters from v, mirroring SQLite's rtrim(X) and
// rtrim(X,Y). See [Trim] for the argument semantics.
func RTrim(v Value, chars ...Value) Value { return funcTrim(v, chars, false, true) }

// Substr returns a substring of v using SQLite's substr(X,Y,Z) semantics over
// Unicode characters. start is 1-based; a negative start counts from the end of
// the string. If length is NULL the substring runs to the end of v; a negative
// length selects characters preceding the start position. NULL input (or a NULL
// start) yields NULL.
func Substr(v, start, length Value) Value {
	if v.IsNull() || start.IsNull() {
		return Null()
	}
	runes := []rune(v.String())
	n := int64(len(runes))
	p1 := funcToInt(start)
	hasZ := !length.IsNull()
	var p2 int64
	if hasZ {
		p2 = funcToInt(length)
	}
	if p1 < 0 {
		p1 += n
		if p1 < 0 {
			if hasZ {
				p2 += p1
				if p2 < 0 {
					p2 = 0
				}
			}
			p1 = 0
		}
	} else if p1 > 0 {
		p1--
	} else if hasZ && p2 > 0 {
		p2--
	}
	if hasZ {
		if p2 < 0 {
			p1 += p2
			p2 = -p2
			if p1 < 0 {
				p2 += p1
				if p2 < 0 {
					p2 = 0
				}
				p1 = 0
			}
		}
	} else {
		p2 = n
	}
	if p1 >= n || p1 < 0 {
		if p1 < 0 {
			p1 = 0
		} else {
			return Text("")
		}
	}
	end := p1 + p2
	if end > n {
		end = n
	}
	if end < p1 {
		end = p1
	}
	return Text(string(runes[p1:end]))
}

// Replace returns v with every non-overlapping occurrence of from replaced by
// to, mirroring SQLite's replace(). If any argument is NULL the result is NULL;
// an empty from leaves v unchanged.
func Replace(v, from, to Value) Value {
	if v.IsNull() || from.IsNull() || to.IsNull() {
		return Null()
	}
	f := from.String()
	if f == "" {
		return Text(v.String())
	}
	return Text(strings.ReplaceAll(v.String(), f, to.String()))
}

// Round returns v rounded to the number of decimal places given by digits,
// always as a REAL, matching SQLite's round(). Rounding is half-away-from-zero.
// A NULL v yields NULL; a NULL digits is treated as zero; non-numeric input
// rounds to 0. Negative digit counts are clamped to zero.
func Round(v, digits Value) Value {
	if v.IsNull() {
		return Null()
	}
	nv, ok := funcNumeric(v)
	if !ok {
		return Real(0)
	}
	x := nv.asFloat()
	var d int64
	if !digits.IsNull() {
		d = funcToInt(digits)
	}
	if d < 0 {
		d = 0
	}
	// SQLite caps the requested precision at 30 digits; beyond that the value is
	// already at (or past) the resolution of a float64, so rounding is a no-op.
	// Clamping also prevents 10**d from overflowing to +Inf, which would turn a
	// finite input into NaN.
	if d > 30 {
		d = 30
	}
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return Real(x)
	}
	pow := math.Pow(10, float64(d))
	return Real(math.Round(x*pow) / pow)
}

// Coalesce returns the first non-NULL argument, or NULL if every argument is
// NULL, matching SQLite's coalesce().
func Coalesce(vals ...Value) Value {
	for _, v := range vals {
		if !v.IsNull() {
			return v
		}
	}
	return Null()
}

// IfNull returns a when a is non-NULL, otherwise b. It mirrors SQLite's
// ifnull(a,b), equivalent to coalesce(a,b) with exactly two arguments.
func IfNull(a, b Value) Value {
	if a.IsNull() {
		return b
	}
	return a
}

// NullIf returns NULL when a and b compare equal, otherwise a. It mirrors
// SQLite's nullif(a,b).
func NullIf(a, b Value) Value {
	if a.IsNull() || b.IsNull() {
		return a
	}
	if compare(a, b) == 0 {
		return Null()
	}
	return a
}

// TypeOf returns the SQLite storage-class name of v: one of "null", "integer",
// "real", "text" or "blob". It mirrors SQLite's typeof().
func TypeOf(v Value) Value {
	switch v.Type {
	case TypeInteger:
		return Text("integer")
	case TypeReal:
		return Text("real")
	case TypeText:
		return Text("text")
	case TypeBlob:
		return Text("blob")
	default:
		return Text("null")
	}
}

// Hex returns the upper-case hexadecimal encoding of v interpreted as a blob,
// matching SQLite's hex(). NULL yields an empty string.
func Hex(v Value) Value {
	var b []byte
	switch v.Type {
	case TypeNull:
		b = nil
	case TypeBlob:
		b = v.b
	default:
		b = []byte(v.String())
	}
	return Text(strings.ToUpper(hex.EncodeToString(b)))
}

// Quote returns an SQL literal that, when parsed, reproduces v: 'NULL' for NULL,
// the number for INTEGER/REAL, a single-quoted string (with embedded quotes
// doubled) for TEXT, and an X'..' blob literal for BLOB. It mirrors SQLite's
// quote().
func Quote(v Value) Value {
	switch v.Type {
	case TypeNull:
		return Text("NULL")
	case TypeInteger:
		return Text(v.String())
	case TypeReal:
		// SQLite renders non-finite reals as the sentinel literals ±9.0e+999
		// rather than "Inf", so that the quoted text is itself parseable SQL.
		if math.IsInf(v.f, 1) {
			return Text("9.0e+999")
		}
		if math.IsInf(v.f, -1) {
			return Text("-9.0e+999")
		}
		return Text(v.String())
	case TypeBlob:
		return Text("X'" + strings.ToUpper(hex.EncodeToString(v.b)) + "'")
	default:
		return Text("'" + strings.ReplaceAll(v.s, "'", "''") + "'")
	}
}

// Instr returns the 1-based position of the first occurrence of needle within
// haystack, or 0 when it does not occur, matching SQLite's instr(). Text is
// searched by character position; if either argument is a BLOB the search is
// byte-oriented. A NULL argument yields NULL.
func Instr(haystack, needle Value) Value {
	if haystack.IsNull() || needle.IsNull() {
		return Null()
	}
	if haystack.Type == TypeBlob || needle.Type == TypeBlob {
		idx := bytes.Index(funcBytes(haystack), funcBytes(needle))
		if idx < 0 {
			return Int(0)
		}
		return Int(int64(idx + 1))
	}
	hs := haystack.String()
	idx := strings.Index(hs, needle.String())
	if idx < 0 {
		return Int(0)
	}
	return Int(int64(len([]rune(hs[:idx])) + 1))
}

// Unicode returns the Unicode code point of the first character of v's textual
// rendering, matching SQLite's unicode(). NULL or an empty string yields NULL.
func Unicode(v Value) Value {
	if v.IsNull() {
		return Null()
	}
	r := []rune(v.String())
	if len(r) == 0 {
		return Null()
	}
	return Int(int64(r[0]))
}

// Char returns a string built from the Unicode code points given by its integer
// arguments, matching SQLite's char(). NULL arguments are skipped; no arguments
// yields an empty string.
func Char(vals ...Value) Value {
	var sb strings.Builder
	for _, v := range vals {
		if v.IsNull() {
			continue
		}
		sb.WriteRune(rune(funcToInt(v)))
	}
	return Text(sb.String())
}

// Sign returns -1, 0 or 1 as an INTEGER according to the sign of v, matching
// SQLite's sign(). NULL or non-numeric input yields NULL.
func Sign(v Value) Value {
	if v.IsNull() {
		return Null()
	}
	nv, ok := funcNumeric(v)
	if !ok {
		return Null()
	}
	f := nv.asFloat()
	switch {
	case f > 0:
		return Int(1)
	case f < 0:
		return Int(-1)
	default:
		return Int(0)
	}
}

// Glob reports whether text matches the Unix-style glob pattern, using the same
// case-sensitive semantics as SQLite's GLOB operator: '*' matches any sequence
// of characters, '?' matches any single character, and '[...]' matches a
// character class (with ranges such as a-z and negation via a leading '^' or
// '!').
func Glob(pattern, text string) bool {
	return funcGlobMatch([]rune(pattern), []rune(text))
}

func funcGlobMatch(p, s []rune) bool {
	for len(p) > 0 {
		switch p[0] {
		case '*':
			for len(p) > 1 && p[1] == '*' {
				p = p[1:]
			}
			if len(p) == 1 {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if funcGlobMatch(p[1:], s[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(s) == 0 {
				return false
			}
			p, s = p[1:], s[1:]
		case '[':
			if len(s) == 0 {
				return false
			}
			ok, np := funcGlobClass(p, s[0])
			if !ok {
				return false
			}
			p, s = np, s[1:]
		default:
			if len(s) == 0 || s[0] != p[0] {
				return false
			}
			p, s = p[1:], s[1:]
		}
	}
	return len(s) == 0
}

// funcGlobClass evaluates a '[...]' character class at the head of p against the
// rune c, returning whether it matched and the remainder of p after the closing
// ']'.
func funcGlobClass(p []rune, c rune) (bool, []rune) {
	i := 1
	negate := false
	if i < len(p) && (p[i] == '^' || p[i] == '!') {
		negate = true
		i++
	}
	matched := false
	first := true
	for i < len(p) {
		if p[i] == ']' && !first {
			break
		}
		first = false
		if i+2 < len(p) && p[i+1] == '-' && p[i+2] != ']' {
			if c >= p[i] && c <= p[i+2] {
				matched = true
			}
			i += 3
			continue
		}
		if p[i] == c {
			matched = true
		}
		i++
	}
	if i < len(p) && p[i] == ']' {
		i++
	}
	if negate {
		matched = !matched
	}
	return matched, p[i:]
}

// --- registry ---------------------------------------------------------------

// funcScalarRegistry maps upper-cased SQL function names to their scalar
// implementations, adapting variadic and optional-argument forms.
var funcScalarRegistry = map[string]ScalarFunc{
	"ABS": func(a []Value) (Value, error) {
		if len(a) != 1 {
			return Null(), funcArity("abs")
		}
		// abs() of the most-negative 64-bit integer cannot be represented as a
		// positive integer; SQLite reports "integer overflow" rather than
		// silently promoting to REAL.
		if a[0].Type == TypeInteger && a[0].i == math.MinInt64 {
			return Null(), fmt.Errorf("integer overflow")
		}
		return Abs(a[0]), nil
	},
	"LENGTH": func(a []Value) (Value, error) {
		if len(a) != 1 {
			return Null(), funcArity("length")
		}
		return Length(a[0]), nil
	},
	"UPPER": func(a []Value) (Value, error) {
		if len(a) != 1 {
			return Null(), funcArity("upper")
		}
		return Upper(a[0]), nil
	},
	"LOWER": func(a []Value) (Value, error) {
		if len(a) != 1 {
			return Null(), funcArity("lower")
		}
		return Lower(a[0]), nil
	},
	"TRIM": func(a []Value) (Value, error) {
		if len(a) < 1 || len(a) > 2 {
			return Null(), funcArity("trim")
		}
		return Trim(a[0], a[1:]...), nil
	},
	"LTRIM": func(a []Value) (Value, error) {
		if len(a) < 1 || len(a) > 2 {
			return Null(), funcArity("ltrim")
		}
		return LTrim(a[0], a[1:]...), nil
	},
	"RTRIM": func(a []Value) (Value, error) {
		if len(a) < 1 || len(a) > 2 {
			return Null(), funcArity("rtrim")
		}
		return RTrim(a[0], a[1:]...), nil
	},
	"SUBSTR":    funcSubstrDispatch,
	"SUBSTRING": funcSubstrDispatch,
	"REPLACE": func(a []Value) (Value, error) {
		if len(a) != 3 {
			return Null(), funcArity("replace")
		}
		return Replace(a[0], a[1], a[2]), nil
	},
	"ROUND": func(a []Value) (Value, error) {
		if len(a) < 1 || len(a) > 2 {
			return Null(), funcArity("round")
		}
		d := Null()
		if len(a) == 2 {
			d = a[1]
		}
		return Round(a[0], d), nil
	},
	"COALESCE": func(a []Value) (Value, error) {
		if len(a) < 1 {
			return Null(), funcArity("coalesce")
		}
		return Coalesce(a...), nil
	},
	"IFNULL": func(a []Value) (Value, error) {
		if len(a) != 2 {
			return Null(), funcArity("ifnull")
		}
		return IfNull(a[0], a[1]), nil
	},
	"NULLIF": func(a []Value) (Value, error) {
		if len(a) != 2 {
			return Null(), funcArity("nullif")
		}
		return NullIf(a[0], a[1]), nil
	},
	"TYPEOF": func(a []Value) (Value, error) {
		if len(a) != 1 {
			return Null(), funcArity("typeof")
		}
		return TypeOf(a[0]), nil
	},
	"HEX": func(a []Value) (Value, error) {
		if len(a) != 1 {
			return Null(), funcArity("hex")
		}
		return Hex(a[0]), nil
	},
	"QUOTE": func(a []Value) (Value, error) {
		if len(a) != 1 {
			return Null(), funcArity("quote")
		}
		return Quote(a[0]), nil
	},
	"INSTR": func(a []Value) (Value, error) {
		if len(a) != 2 {
			return Null(), funcArity("instr")
		}
		return Instr(a[0], a[1]), nil
	},
	"UNICODE": func(a []Value) (Value, error) {
		if len(a) != 1 {
			return Null(), funcArity("unicode")
		}
		return Unicode(a[0]), nil
	},
	"CHAR": func(a []Value) (Value, error) {
		return Char(a...), nil
	},
	"SIGN": func(a []Value) (Value, error) {
		if len(a) != 1 {
			return Null(), funcArity("sign")
		}
		return Sign(a[0]), nil
	},
	"GLOB": func(a []Value) (Value, error) {
		if len(a) != 2 {
			return Null(), funcArity("glob")
		}
		if a[0].IsNull() || a[1].IsNull() {
			return Null(), nil
		}
		return boolVal(Glob(a[0].String(), a[1].String())), nil
	},
}

// funcSubstrDispatch adapts the 2- and 3-argument forms of substr/substring.
func funcSubstrDispatch(a []Value) (Value, error) {
	switch len(a) {
	case 2:
		return Substr(a[0], a[1], Null()), nil
	case 3:
		return Substr(a[0], a[1], a[2]), nil
	default:
		return Null(), funcArity("substr")
	}
}
