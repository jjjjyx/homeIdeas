package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"family-ledger/server/internal/backup"
)

// 单独的小工具：生成一份 SQLite 快照并按保留份数清理旧备份。
//
//	go run ./cmd/backup -db ./data/app.db -dir ./data/backup -keep 14
func main() {
	dbPath := flag.String("db", "./data/app.db", "SQLite 文件路径")
	dir := flag.String("dir", "./data/backup", "备份输出目录")
	keep := flag.Int("keep", 14, "保留最近几份")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	target, err := backup.Create(ctx, *dbPath, *dir, *keep)
	if err != nil {
		slog.Error("备份失败", "err", err)
		os.Exit(1)
	}
	fmt.Println(target)
}
