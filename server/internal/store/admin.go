package store

import (
	"context"
	"database/sql"
	"strings"

	"family-ledger/server/internal/domain"
)

// AdminMemberRow 后台成员列表行：用户信息 + 在唯一家庭里的角色。
type AdminMemberRow struct {
	UserID    string
	Account   string
	Nickname  string
	Role      string
	Status    int
	JoinedAt  int64
	CreatedAt int64
}

func (d *DB) AdminListMembers(ctx context.Context, q Querier, familyID, keyword string, limit, offset int) ([]AdminMemberRow, error) {
	var sb strings.Builder
	sb.WriteString(`
		SELECT u.id, u.account, u.nickname, m.role, u.status, m.joined_at, u.created_at
		FROM family_members m JOIN users u ON u.id = m.user_id
		WHERE m.family_id = ?`)
	args := []any{familyID}
	if keyword != "" {
		sb.WriteString(` AND (u.account LIKE ? OR u.nickname LIKE ?)`)
		like := "%" + keyword + "%"
		args = append(args, like, like)
	}
	sb.WriteString(` ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END,
		m.joined_at ASC LIMIT ? OFFSET ?`)
	args = append(args, limit, offset)

	rows, err := q.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]AdminMemberRow, 0, limit)
	for rows.Next() {
		var r AdminMemberRow
		if err := rows.Scan(&r.UserID, &r.Account, &r.Nickname, &r.Role, &r.Status,
			&r.JoinedAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) AdminCountMembers(ctx context.Context, q Querier, familyID, keyword string) (int, error) {
	var sb strings.Builder
	sb.WriteString(`
		SELECT COUNT(*)
		FROM family_members m JOIN users u ON u.id = m.user_id
		WHERE m.family_id = ?`)
	args := []any{familyID}
	if keyword != "" {
		sb.WriteString(` AND (u.account LIKE ? OR u.nickname LIKE ?)`)
		like := "%" + keyword + "%"
		args = append(args, like, like)
	}
	var n int
	err := q.QueryRowContext(ctx, sb.String(), args...).Scan(&n)
	return n, err
}

// AdminOverview 单家庭概览。
type AdminOverview struct {
	Members       int
	Transactions  int
	AmountCents   int64
	NewTxToday    int
	NewUsersToday int
}

func (d *DB) AdminOverview(ctx context.Context, q Querier, familyID string, since int64) (*AdminOverview, error) {
	var o AdminOverview
	err := q.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM family_members WHERE family_id = ?),
		       (SELECT COUNT(*) FROM transactions WHERE family_id = ? AND deleted_at IS NULL),
		       (SELECT COALESCE(SUM(amount_cents),0) FROM transactions
		        WHERE family_id = ? AND deleted_at IS NULL AND type = 0 AND scope <> 'transfer'),
		       (SELECT COUNT(*) FROM transactions WHERE family_id = ? AND created_at >= ? AND deleted_at IS NULL),
		       (SELECT COUNT(*) FROM family_members WHERE family_id = ? AND joined_at >= ?)`,
		familyID, familyID, familyID, familyID, since, familyID, since).
		Scan(&o.Members, &o.Transactions, &o.AmountCents, &o.NewTxToday, &o.NewUsersToday)
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// AdminTxRow 后台账目行（含已删除，便于审计）。
type AdminTxRow struct {
	domain.Transaction
	CategoryName string
	MemberName   string
}

func (d *DB) AdminTransactions(ctx context.Context, q Querier, familyID string, limit, offset int) ([]AdminTxRow, error) {
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

func (d *DB) AdminCountTransactions(ctx context.Context, q Querier, familyID string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM transactions WHERE family_id = ?`, familyID).Scan(&n)
	return n, err
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
