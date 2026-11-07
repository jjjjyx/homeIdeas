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

	fmt.Println("== users ==")
	rows, err := db.Query(`SELECT account, nickname, status, is_platform_admin FROM users ORDER BY created_at`)
	if err != nil {
		log.Fatal(err)
	}
	for rows.Next() {
		var account, nickname string
		var status, admin int
		if err := rows.Scan(&account, &nickname, &status, &admin); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  %-14s %-8s status=%d admin=%d\n", account, nickname, status, admin)
	}
	rows.Close()

	fmt.Println("== families ==")
	rows, err = db.Query(`SELECT id, name, rev FROM families`)
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

	fmt.Println("== invites ==")
	rows, err = db.Query(`SELECT code, role, COALESCE(used_by,'-') FROM invites ORDER BY created_at DESC LIMIT 5`)
	if err != nil {
		log.Fatal(err)
	}
	for rows.Next() {
		var code, role, usedBy string
		if err := rows.Scan(&code, &role, &usedBy); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  %s %-7s used_by=%s\n", code, role, usedBy)
	}
	rows.Close()

	var txCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM transactions`).Scan(&txCount)
	fmt.Printf("== transactions: %d ==\n", txCount)
}
