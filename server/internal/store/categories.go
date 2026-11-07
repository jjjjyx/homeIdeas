package store

import (
	"context"
	"database/sql"
	"errors"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/pkg/id"
)

type seedCategory struct {
	Name  string
	Type  int
	Icon  string
	Color int64
}

// defaultCategories 与 App 端内置分类保持一致，新家庭创建时自动种入。
var defaultCategories = []seedCategory{
	{"餐饮", domain.TypeExpense, "restaurant", 0xFFEF5350},
	{"交通", domain.TypeExpense, "bus", 0xFF42A5F5},
	{"购物", domain.TypeExpense, "shopping", 0xFFAB47BC},
	{"居家", domain.TypeExpense, "home", 0xFF26A69A},
	{"医疗", domain.TypeExpense, "hospital", 0xFFEC407A},
	{"教育", domain.TypeExpense, "school", 0xFF5C6BC0},
	{"娱乐", domain.TypeExpense, "game", 0xFFFFA726},
	{"人情", domain.TypeExpense, "gift", 0xFF8D6E63},
	{"宠物", domain.TypeExpense, "pet", 0xFF9CCC65},
	{"旅行", domain.TypeExpense, "travel", 0xFF29B6F6},
	// 通讯/保险是账单导入后按真实消费补的：话费、宽带、车险、医疗险都很常见
	{"通讯", domain.TypeExpense, "phone", 0xFF78909C},
	// 工作：API 充值、服务器、工具订阅这类"为工作/项目花的钱"，和「教育」区分开
	{"工作", domain.TypeExpense, "briefcase", 0xFF455A64},
	{"保险", domain.TypeExpense, "shield", 0xFF546E7A},
	{"其他", domain.TypeExpense, "other", 0xFF78909C},
	{"工资", domain.TypeIncome, "salary", 0xFF66BB6A},
	{"奖金", domain.TypeIncome, "card", 0xFF26C6DA},
	{"理财", domain.TypeIncome, "invest", 0xFF7E57C2},
	{"红包", domain.TypeIncome, "gift", 0xFFEF5350},
	{"其他收入", domain.TypeIncome, "other", 0xFF78909C},
}

func (d *DB) SeedDefaultCategories(ctx context.Context, q Querier, familyID string, rev int64, now int64) error {
	for i, c := range defaultCategories {
		_, err := q.ExecContext(ctx, `
			INSERT INTO categories (id, family_id, name, type, icon_key, color, sort_order, is_system, rev, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,1,?,?,?)`,
			id.New(), familyID, c.Name, c.Type, c.Icon, c.Color, i, rev, now, now)
		if err != nil {
			return err
		}
	}
	return nil
}

const categoryColumns = `id, family_id, name, type, icon_key, color, sort_order, rev, created_at, updated_at, deleted_at`

type Category struct {
	ID        string
	FamilyID  string
	Name      string
	Type      int
	IconKey   string
	Color     int64
	SortOrder int
	Rev       int64
	CreatedAt int64
	UpdatedAt int64
	DeletedAt *int64
}

func scanCategory(row rowScanner) (*Category, error) {
	var c Category
	var deletedAt sql.NullInt64
	if err := row.Scan(&c.ID, &c.FamilyID, &c.Name, &c.Type, &c.IconKey, &c.Color,
		&c.SortOrder, &c.Rev, &c.CreatedAt, &c.UpdatedAt, &deletedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if deletedAt.Valid {
		v := deletedAt.Int64
		c.DeletedAt = &v
	}
	return &c, nil
}

func (d *DB) CategoriesOfFamily(ctx context.Context, q Querier, familyID string) ([]Category, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT `+categoryColumns+` FROM categories
		WHERE family_id = ? AND deleted_at IS NULL
		ORDER BY type ASC, sort_order ASC`, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Category, 0, 16)
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (d *DB) CategoriesChangedSince(ctx context.Context, q Querier, familyID string, since int64, limit int) ([]Category, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+categoryColumns+` FROM categories WHERE family_id = ? AND rev > ? ORDER BY rev ASC LIMIT ?`,
		familyID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Category, 0, limit)
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// CategoryBelongsToFamily 校验分类归属，防止把别人家的分类写进自己家的账。
func (d *DB) CategoryBelongsToFamily(ctx context.Context, q Querier, categoryID, familyID string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM categories WHERE id = ? AND family_id = ? AND deleted_at IS NULL`,
		categoryID, familyID).Scan(&n)
	return n > 0, err
}

// EnsureDefaultCategories 给唯一家庭补齐缺失的预置分类。
// 老家庭是在「通讯/保险」加入之前创建的，启动时补一次即可（幂等）。
func (d *DB) EnsureDefaultCategories(ctx context.Context, now int64) error {
	family, err := d.PrimaryFamily(ctx, d.SQL())
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil // 还没初始化，等首次注册时再种
		}
		return err
	}

	existing := map[string]bool{}
	nameRows, err := d.SQL().QueryContext(ctx,
		`SELECT name FROM categories WHERE family_id = ? AND deleted_at IS NULL`, family.ID)
	if err != nil {
		return err
	}
	for nameRows.Next() {
		var name string
		if err := nameRows.Scan(&name); err != nil {
			nameRows.Close()
			return err
		}
		existing[name] = true
	}
	nameRows.Close()

	for i, c := range defaultCategories {
		if existing[c.Name] {
			continue
		}
		err := d.WithTx(ctx, func(tx *sql.Tx) error {
			rev, err := d.BumpRev(ctx, tx, family.ID, now)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `
				INSERT INTO categories (id, family_id, name, type, icon_key, color, sort_order, is_system, rev, created_at, updated_at)
				VALUES (?,?,?,?,?,?,?,1,?,?,?)`,
				id.New(), family.ID, c.Name, c.Type, c.Icon, c.Color, i, rev, now, now)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
