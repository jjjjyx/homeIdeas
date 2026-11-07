package store

import (
	"context"
	"database/sql"
	"errors"

	"family-ledger/server/internal/domain"
)

const tokenColumns = `id, user_id, token_hash, device, expires_at, revoked_at, created_at, device_id`

func (d *DB) CreateRefreshToken(ctx context.Context, q Querier, t *domain.RefreshToken) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO refresh_tokens (`+tokenColumns+`) VALUES (?,?,?,?,?,?,?,?)`,
		t.ID, t.UserID, t.TokenHash, t.Device, t.ExpiresAt, t.RevokedAt, t.CreatedAt, t.DeviceID)
	return err
}

func (d *DB) RefreshTokenByHash(ctx context.Context, q Querier, hash string) (*domain.RefreshToken, error) {
	var t domain.RefreshToken
	err := q.QueryRowContext(ctx,
		`SELECT `+tokenColumns+` FROM refresh_tokens WHERE token_hash = ?`, hash).
		Scan(&t.ID, &t.UserID, &t.TokenHash, &t.Device, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt, &t.DeviceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

func (d *DB) RevokeRefreshToken(ctx context.Context, q Querier, id string, at int64) error {
	_, err := q.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, at, id)
	return err
}

func (d *DB) RevokeAllRefreshTokens(ctx context.Context, q Querier, userID string, at int64) error {
	_, err := q.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, at, userID)
	return err
}

// RevokeByDevice 吊销某台设备的所有刷新令牌
func (d *DB) RevokeByDevice(ctx context.Context, q Querier, deviceID string, at int64) error {
	_, err := q.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = ? WHERE device_id = ? AND revoked_at IS NULL`, at, deviceID)
	return err
}
