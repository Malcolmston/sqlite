# Changelog

## 0.2.0

### Added

Built-in SQL scalar functions, closing a significant gap toward SQLite parity.
The parser already accepted arbitrary `FUNC(...)` calls; the executor now
evaluates a registry of core scalar functions in every expression context
(projections, `WHERE`, and around aggregates such as `ABS(SUM(n))`).

New exported API in `funcs.go`:

- Function registry and dispatch: `ScalarFunc`, `LookupScalar`, `CallScalar`,
  `IsScalarFunc`, `ScalarNames`, and the `FuncError` error type.
- String functions: `Length`, `Upper`, `Lower`, `Trim`, `LTrim`, `RTrim`,
  `Substr`, `Replace`, `Instr`, `Unicode`, `Char`, `Hex`, `Quote`.
- Numeric functions: `Abs`, `Round`, `Sign`.
- General functions: `Coalesce`, `IfNull`, `NullIf`, `TypeOf`.
- Pattern matching: `Glob` (implements SQLite `GLOB` semantics).

All functions follow SQLite's dynamic-typing and three-valued NULL rules and are
reachable from SQL, e.g. `SELECT UPPER(TRIM(name)), ABS(n) FROM t`. Every new
symbol has complete godoc and deterministic known-answer tests; benchmarks cover
`Glob`, `Substr`, and scalar dispatch.
