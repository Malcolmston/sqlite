package sqlite_test

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/malcolmston/sqlite"
)

// Example demonstrates opening an in-memory database through database/sql,
// creating a table, inserting rows with placeholder arguments, and running an
// aggregate query.
func Example() {
	db, err := sql.Open("mstsqlite", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	// A single shared connection keeps the anonymous in-memory database stable.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE fruit (name TEXT NOT NULL, qty INTEGER)`); err != nil {
		log.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO fruit (name, qty) VALUES (?, ?), (?, ?), (?, ?)`,
		"apple", 3, "banana", 7, "cherry", 5,
	); err != nil {
		log.Fatal(err)
	}

	rows, err := db.Query(`SELECT name, qty FROM fruit WHERE qty >= ? ORDER BY qty DESC`, 5)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		var qty int
		if err := rows.Scan(&name, &qty); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s: %d\n", name, qty)
	}

	var total int
	if err := db.QueryRow(`SELECT SUM(qty) FROM fruit`).Scan(&total); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("total: %d\n", total)

	// Output:
	// banana: 7
	// cherry: 5
	// total: 15
}
