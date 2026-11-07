package store

import (
	"context"
	"database/sql"
	"errors"

	"family-ledger/server/internal/domain"
)

const inviteColumns = `id, family_id, code, role, created_by, expires_at, used_by, used_at, created_at`

func (d *DB) CreateInvite(ctx context.Context, q Querier, inv *domain.Invite) error {
	// 未使用的邀请码必须写 NULL 而不是空串，否则按 NULL 判定的条件会失效。
	var usedBy any
	if inv.UsedBy != "" {
		usedBy = inv.UsedBy
	}
	_, err := q.ExecContext(ctx,
		`INSERT INTO invites (`+inviteColumns+`) VALUES (?,?,?,?,?,?,?,?,?)`,
		inv.ID, inv.FamilyID, inv.Code, inv.Role, inv.CreatedBy, inv.ExpiresAt, usedBy, inv.UsedAt, inv.CreatedAt)
	return err
}

func (d *DB) InviteByCode(ctx context.Context, q Querier, code string) (*domain.Invite, error) {
	var inv domain.Invite
	var usedBy sql.NullString
	var expiresAt, usedAt sql.NullInt64
	err := q.QueryRowContext(ctx,
		`SELECT `+inviteColumns+` FROM invites WHERE code = ?`, code).
		Scan(&inv.ID, &inv.FamilyID, &inv.Code, &inv.Role, &inv.CreatedBy,
			&expiresAt, &usedBy, &usedAt, &inv.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	inv.UsedBy = usedBy.String
	if expiresAt.Valid {
		v := expiresAt.Int64
		inv.ExpiresAt = &v
	}
	if usedAt.Valid {
		v := usedAt.Int64
		inv.UsedAt = &v
	}
	return &inv, nil
}

// MarkInviteUsed 原子核销：只允许从未使用状态改成已使用，返回是否命中。
func (d *DB) MarkInviteUsed(ctx context.Context, q Querier, inviteID, userID string, now int64) (bool, error) {
	res, err := q.ExecContext(ctx,
		`UPDATE invites SET used_by = ?, used_at = ? WHERE id = ? AND (used_by IS NULL OR used_by = '')`,
		userID, now, inviteID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
