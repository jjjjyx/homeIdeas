package backup_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"family-ledger/server/internal/backup"
	"family-ledger/server/internal/store"
)

// 备份必须生成一个可独立打开的完整库，并按 keep 清理旧文件。
func TestCreateAndPrune(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "app.db")

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("打开库失败: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	// 写入一点数据，确认快照里真的有内容
	if _, err := db.SQL().ExecContext(ctx,
		`INSERT INTO system_config(key, value, updated_at) VALUES('k','v',1)`); err != nil {
		t.Fatalf("写入测试数据失败: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("关闭库失败: %v", err)
	}

	dir := filepath.Join(root, "backup")
	target, err := backup.Create(ctx, dbPath, dir, 2)
	if err != nil {
		t.Fatalf("备份失败: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("备份文件不存在: %v", err)
	}

	// 快照可以独立打开，且数据在
	snap, err := store.Open(target)
	if err != nil {
		t.Fatalf("打开快照失败: %v", err)
	}
	var value string
	if err := snap.SQL().QueryRowContext(ctx,
		`SELECT value FROM system_config WHERE key = 'k'`).Scan(&value); err != nil {
		t.Fatalf("快照里读不到数据: %v", err)
	}
	if value != "v" {
		t.Fatalf("快照内容不对: %s", value)
	}
	_ = snap.Close()

	// 再生成 3 份（文件名秒级时间戳可能相同，这里直接手工造文件验证清理逻辑）
	for i := 0; i < 3; i++ {
		name := filepath.Join(dir, "app-2000-01-0"+string(rune('1'+i))+"-000000.db")
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatalf("造文件失败: %v", err)
		}
	}
	if _, err := backup.Create(ctx, dbPath, dir, 2); err != nil {
		t.Fatalf("第二次备份失败: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取备份目录失败: %v", err)
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}
	if count > 2 {
		t.Fatalf("应只保留 2 份备份，实际 %d 份", count)
	}
}
