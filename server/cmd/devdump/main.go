package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

// 开发期小工具：直接看库里的关键状态。
// 用法： go run ./cmd/devdump -db ./data/app.db
func main() {
	dbPath := flag.String("db", "./data/app.db", "SQLite 文件路径")
	flag.Parse()

	db, err := sql.Open("sqlite", "file:"+*dbPath+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	fmt.Println("== family ==")
	rows, err := db.Query(`SELECT id, name, rev FROM families WHERE deleted_at IS NULL ORDER BY created_at LIMIT 1`)
	if err != nil {
		log.Fatal(err)
	}
	for rows.Next() {
		var id, name string
		var rev int64
		if err := rows.Scan(&id, &name, &rev); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  %s  %-10s rev=%d\n", id[:8], name, rev)
	}
	rows.Close()

	fmt.Println("== members ==")
	rows, err = db.Query(`
		SELECT u.account, u.nickname, u.status, m.role
		FROM users u LEFT JOIN family_members m ON m.user_id = u.id
		ORDER BY u.created_at`)
	if err != nil {
		log.Fatal(err)
	}
	for rows.Next() {
		var account, nickname, role string
		var status int
		if err := rows.Scan(&account, &nickname, &status, &role); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  %-14s %-8s status=%d role=%s\n", account, nickname, status, role)
	}
	rows.Close()

	var txCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM transactions`).Scan(&txCount)
	fmt.Printf("== transactions: %d ==\n", txCount)
}
