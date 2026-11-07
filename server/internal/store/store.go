package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB 包一层 *sql.DB，负责连接配置、迁移和事务。
type DB struct {
	sqlDB *sql.DB
	path  string
}

// Querier 让仓储函数既能跑在连接池上，也能跑在事务里。
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func Open(path string) (*DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建数据目录失败: %w", err)
		}
	}
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)",
		path,
	)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// 家庭规模下以「简单正确」优先：单连接串行化读写，彻底避开 SQLITE_BUSY。
	// 将来要提并发，改成读写双池即可，业务代码不用动。
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return &DB{sqlDB: sqlDB, path: path}, nil
}

func (d *DB) SQL() *sql.DB { return d.sqlDB }

func (d *DB) Path() string { return d.path }

func (d *DB) Close() error { return d.sqlDB.Close() }

func (d *DB) Ping(ctx context.Context) error {
	return d.sqlDB.PingContext(ctx)
}

func (d *DB) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := d.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Migrate 按文件名顺序执行未应用的迁移，每个文件一个事务。
func (d *DB) Migrate(ctx context.Context) error {
	const createTable = `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`
	if _, err := d.sqlDB.ExecContext(ctx, createTable); err != nil {
		return err
	}

	applied := map[int]bool{}
	rows, err := d.sqlDB.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		version, convErr := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if convErr != nil {
			return fmt.Errorf("迁移文件名不规范: %s", name)
		}
		if applied[version] {
			continue
		}
		content, readErr := migrationsFS.ReadFile("migrations/" + name)
		if readErr != nil {
			return readErr
		}
		tx, beginErr := d.sqlDB.BeginTx(ctx, nil)
		if beginErr != nil {
			return beginErr
		}
		for _, stmt := range splitStatements(string(content)) {
			if _, execErr := tx.ExecContext(ctx, stmt); execErr != nil {
				_ = tx.Rollback()
				return fmt.Errorf("执行迁移 %s 失败: %w", name, execErr)
			}
		}
		if _, execErr := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES(?,?)`, version, NowMS()); execErr != nil {
			_ = tx.Rollback()
			return execErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return commitErr
		}
	}
	return nil
}

// splitStatements 按分号切分 DDL。当前迁移文件里没有分号出现在字符串或触发器内。
func splitStatements(content string) []string {
	raw := strings.Split(content, ";")
	out := make([]string, 0, len(raw))
	for _, stmt := range raw {
		if s := strings.TrimSpace(stmt); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func NowMS() int64 { return time.Now().UnixMilli() }
