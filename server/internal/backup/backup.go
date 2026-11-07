// Package backup 实现 SQLite 在线备份。
//
// 为什么不用直接复制文件：WAL 模式下数据库由 app.db / app.db-wal / app.db-shm
// 三个文件共同组成，cp 出来的 app.db 可能是「半个事务」。
// VACUUM INTO 会在一个读事务内导出一致的完整快照，且不阻塞写入。
package backup

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Create 生成一份快照，返回文件路径。
func Create(ctx context.Context, dbPath, dir string, keep int) (string, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return "", fmt.Errorf("数据库不存在: %s", dbPath)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("创建备份目录失败: %w", err)
	}

	target := uniqueTarget(dir, time.Now())

	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return "", err
	}
	defer db.Close()

	// VACUUM INTO 的目标必须是字面量，不能参数化；路径里的单引号需要转义
	escaped := strings.ReplaceAll(target, "'", "''")
	if _, err := db.ExecContext(ctx, fmt.Sprintf("VACUUM INTO '%s'", escaped)); err != nil {
		return "", fmt.Errorf("VACUUM INTO 失败: %w", err)
	}

	if keep > 0 {
		if err := prune(dir, keep); err != nil {
			return target, fmt.Errorf("备份已生成但清理旧文件失败: %w", err)
		}
	}
	return target, nil
}

// uniqueTarget 生成不冲突的文件名：秒级时间戳 + 毫秒，撞名再补序号。
// VACUUM INTO 不允许覆盖已存在的文件，所以这里必须保证唯一。
func uniqueTarget(dir string, now time.Time) string {
	base := now.Format("20060102-150405.000")
	target := filepath.Join(dir, "app-"+base+".db")
	for i := 1; ; i++ {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			return target
		}
		target = filepath.Join(dir, fmt.Sprintf("app-%s-%d.db", base, i))
	}
}

// prune 只保留最新的 keep 份（文件名带时间戳，字典序即时间序）。
func prune(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "app-") && strings.HasSuffix(name, ".db") {
			names = append(names, name)
		}
	}
	if len(names) <= keep {
		return nil
	}
	sort.Strings(names)
	for _, name := range names[:len(names)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}
