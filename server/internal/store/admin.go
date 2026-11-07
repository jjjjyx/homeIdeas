package store

import (
	"context"
	"database/sql"
	"strings"

	"family-ledger/server/internal/domain"
)

// AdminUserRow 用户列表行（带家庭数）
type AdminUserRow struct {
	domain.User
	FamilyCount int
}

func (d *DB) AdminListUsers(ctx context.Context, q Querier, keyword string, limit, offset int) ([]AdminUserRow, error) {
	var sb strings.Builder
	sb.WriteString(`
		SELECT u.id, u.account, u.password_hash, u.nickname, u.avatar_url, u.phone,
		       u.status, u.is_platform_admin, u.created_at, u.updated_at,
		       (SELECT COUNT(*) FROM family_members m WHERE m.user_id = u.id)
		FROM users u WHERE 1=1`)
	args := make([]any, 0, 3)
	if keyword != "" {
		sb.WriteString(` AND (u.account LIKE ? OR u.nickname LIKE ?)`)
		like := "%" + keyword + "%"
		args = append(args, like, like)
	}
	sb.WriteString(` ORDER BY u.created_at DESC LIMIT ? OFFSET ?`)
	args = append(args, limit, offset)

	rows, err := q.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AdminUserRow, 0, limit)
	for rows.Next() {
		var r AdminUserRow
		var admin int
		if err := rows.Scan(&r.ID, &r.Account, &r.PasswordHash, &r.Nickname, &r.AvatarURL,
			&r.Phone, &r.Status, &admin, &r.CreatedAt, &r.UpdatedAt, &r.FamilyCount); err != nil {
			return nil, err
		}
		r.IsPlatformAdmin = admin == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// AdminFamilyRow 家庭列表行
type AdminFamilyRow struct {
	domain.Family
	OwnerNickname    string
	MemberCount      int
	TransactionCount int
}

func (d *DB) AdminListFamilies(ctx context.Context, q Querier, keyword string, limit, offset int) ([]AdminFamilyRow, error) {
	var sb strings.Builder
	sb.WriteString(`
		SELECT f.id, f.name, f.owner_id, f.currency, f.rev, f.created_at, f.updated_at,
		       COALESCE(u.nickname, ''),
		       (SELECT COUNT(*) FROM family_members m WHERE m.family_id = f.id),
		       (SELECT COUNT(*) FROM transactions t WHERE t.family_id = f.id AND t.deleted_at IS NULL)
		FROM families f LEFT JOIN users u ON u.id = f.owner_id
		WHERE f.deleted_at IS NULL`)
	args := make([]any, 0, 3)
	if keyword != "" {
		sb.WriteString(` AND f.name LIKE ?`)
		args = append(args, "%"+keyword+"%")
	}
	sb.WriteString(` ORDER BY f.created_at DESC LIMIT ? OFFSET ?`)
	args = append(args, limit, offset)

	rows, err := q.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AdminFamilyRow, 0, limit)
	for rows.Next() {
		var r AdminFamilyRow
		if err := rows.Scan(&r.ID, &r.Name, &r.OwnerID, &r.Currency, &r.Rev,
			&r.CreatedAt, &r.UpdatedAt, &r.OwnerNickname, &r.MemberCount, &r.TransactionCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AdminOverview 平台概览
type AdminOverview struct {
	Users         int
	Families      int
	Transactions  int
	AmountCents   int64
	NewUsersToday int
	NewTxToday    int
}

func (d *DB) AdminOverview(ctx context.Context, q Querier, since int64) (*AdminOverview, error) {
	var o AdminOverview
	err := q.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM users),
		       (SELECT COUNT(*) FROM families WHERE deleted_at IS NULL),
		       (SELECT COUNT(*) FROM transactions WHERE deleted_at IS NULL),
		       (SELECT COALESCE(SUM(amount_cents),0) FROM transactions WHERE deleted_at IS NULL AND type = 0),
		       (SELECT COUNT(*) FROM users WHERE created_at >= ?),
		       (SELECT COUNT(*) FROM transactions WHERE created_at >= ? AND deleted_at IS NULL)`,
		since, since).Scan(&o.Users, &o.Families, &o.Transactions, &o.AmountCents, &o.NewUsersToday, &o.NewTxToday)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// AdminFamilyTransactions 后台按家庭查账（含已删除，便于审计）
type AdminTxRow struct {
	domain.Transaction
	CategoryName string
	MemberName   string
}

func (d *DB) AdminFamilyTransactions(ctx context.Context, q Querier, familyID string, limit, offset int) ([]AdminTxRow, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT t.id, t.family_id, t.type, t.amount_cents, t.category_id,
		       COALESCE(t.account_id,''), t.member_id, t.occurred_at, t.note, t.visibility,
		       t.tags, t.rev, t.created_at, t.updated_at, t.deleted_at,
		       COALESCE(c.name,''), COALESCE(u.nickname,'')
		FROM transactions t
		LEFT JOIN categories c ON c.id = t.category_id
		LEFT JOIN users u ON u.id = t.member_id
		WHERE t.family_id = ?
		ORDER BY t.occurred_at DESC LIMIT ? OFFSET ?`, familyID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AdminTxRow, 0, limit)
	for rows.Next() {
		var r AdminTxRow
		var deletedAt sql.NullInt64
		if err := rows.Scan(&r.ID, &r.FamilyID, &r.Type, &r.AmountCents, &r.CategoryID,
			&r.AccountID, &r.MemberID, &r.OccurredAt, &r.Note, &r.Visibility,
			&r.Tags, &r.Rev, &r.CreatedAt, &r.UpdatedAt, &deletedAt,
			&r.CategoryName, &r.MemberName); err != nil {
			return nil, err
		}
		if deletedAt.Valid {
			v := deletedAt.Int64
			r.DeletedAt = &v
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type AuditRow struct {
	ID         string
	FamilyID   string
	UserID     string
	Action     string
	TargetType string
	TargetID   string
	Detail     string
	IP         string
	CreatedAt  int64
	Nickname   string
}

func (d *DB) AdminAuditLogs(ctx context.Context, q Querier, action string, limit, offset int) ([]AuditRow, error) {
	var sb strings.Builder
	sb.WriteString(`
		SELECT a.id, COALESCE(a.family_id,''), COALESCE(a.user_id,''), a.action,
		       a.target_type, a.target_id, a.detail, a.ip, a.created_at, COALESCE(u.nickname,'')
		FROM audit_logs a LEFT JOIN users u ON u.id = a.user_id WHERE 1=1`)
	args := make([]any, 0, 3)
	if action != "" {
		sb.WriteString(` AND a.action LIKE ?`)
		args = append(args, action+"%")
	}
	sb.WriteString(` ORDER BY a.created_at DESC LIMIT ? OFFSET ?`)
	args = append(args, limit, offset)

	rows, err := q.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AuditRow, 0, limit)
	for rows.Next() {
		var r AuditRow
		if err := rows.Scan(&r.ID, &r.FamilyID, &r.UserID, &r.Action, &r.TargetType,
			&r.TargetID, &r.Detail, &r.IP, &r.CreatedAt, &r.Nickname); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AdminCountFamilies 与列表同条件的总数，供分页使用
func (d *DB) AdminCountFamilies(ctx context.Context, q Querier, keyword string) (int, error) {
	sqlStr := `SELECT COUNT(*) FROM families f WHERE f.deleted_at IS NULL`
	args := make([]any, 0, 1)
	if keyword != "" {
		sqlStr += ` AND f.name LIKE ?`
		args = append(args, "%"+keyword+"%")
	}
	var n int
	err := q.QueryRowContext(ctx, sqlStr, args...).Scan(&n)
	return n, err
}

// AdminCountAuditLogs 审计日志总数
func (d *DB) AdminCountAuditLogs(ctx context.Context, q Querier, action string) (int, error) {
	sqlStr := `SELECT COUNT(*) FROM audit_logs a WHERE 1=1`
	args := make([]any, 0, 1)
	if action != "" {
		sqlStr += ` AND a.action LIKE ?`
		args = append(args, action+"%")
	}
	var n int
	err := q.QueryRowContext(ctx, sqlStr, args...).Scan(&n)
	return n, err
}

// AdminSetUserStatus 封禁/解封，同时吊销该用户所有 refresh token。
func (d *DB) AdminSetUserStatus(ctx context.Context, userID string, status int, now int64) error {
	return d.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE users SET status = ?, updated_at = ? WHERE id = ?`, status, now, userID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return domain.ErrNotFound
		}
		if status != 1 {
			if _, err := tx.ExecContext(ctx,
				`UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`,
				now, userID); err != nil {
				return err
			}
		}
		return nil
	})
}
