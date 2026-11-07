package store

import (
	"context"
	"database/sql"
	"errors"

	"family-ledger/server/internal/domain"
)

const userColumns = `id, account, password_hash, nickname, avatar_url, phone, status, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(s rowScanner) (*domain.User, error) {
	var u domain.User
	if err := s.Scan(&u.ID, &u.Account, &u.PasswordHash, &u.Nickname, &u.AvatarURL,
		&u.Phone, &u.Status, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

func (d *DB) CreateUser(ctx context.Context, q Querier, u *domain.User) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO users (`+userColumns+`) VALUES (?,?,?,?,?,?,?,?,?)`,
		u.ID, u.Account, u.PasswordHash, u.Nickname, u.AvatarURL, u.Phone, u.Status, u.CreatedAt, u.UpdatedAt)
	return err
}

func (d *DB) UserByAccount(ctx context.Context, q Querier, account string) (*domain.User, error) {
	return scanUser(q.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE account = ?`, account))
}

func (d *DB) UserByID(ctx context.Context, q Querier, id string) (*domain.User, error) {
	return scanUser(q.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

func (d *DB) CountUsers(ctx context.Context, q Querier) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (d *DB) UpdateUserPassword(ctx context.Context, q Querier, userID, hash string, now int64) error {
	_, err := q.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, hash, now, userID)
	return err
}

func (d *DB) SetUserStatus(ctx context.Context, q Querier, userID string, status int, now int64) error {
	_, err := q.ExecContext(ctx, `UPDATE users SET status = ?, updated_at = ? WHERE id = ?`, status, now, userID)
	return err
}

// RevokeTokensOfUser 吊销某个用户的全部刷新令牌（移除成员 / 封禁时使用）。
func (d *DB) RevokeTokensOfUser(ctx context.Context, q Querier, userID string, now int64) error {
	_, err := q.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, now, userID)
	return err
}
