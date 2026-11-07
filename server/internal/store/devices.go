package store

import (
	"context"
	"database/sql"
	"errors"

	"family-ledger/server/internal/domain"
)

const deviceColumns = `id, device_uid, device_key, user_id, family_id, role, model, os_version, app_version,
	ip, status, pair_code, pair_secret, created_at, expires_at, approved_at, approved_by, last_seen_at`

func scanDevice(row rowScanner) (*domain.Device, error) {
	var d domain.Device
	var userID, familyID, role, pairSecret sql.NullString
	var approvedAt, lastSeen sql.NullInt64
	var approvedBy sql.NullString
	err := row.Scan(&d.ID, &d.DeviceUID, &d.DeviceKey, &userID, &familyID, &role, &d.Model, &d.OSVersion,
		&d.AppVersion, &d.IP, &d.Status, &d.PairCode, &pairSecret, &d.CreatedAt, &d.ExpiresAt,
		&approvedAt, &approvedBy, &lastSeen)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	d.UserID = userID.String
	d.FamilyID = familyID.String
	d.Role = role.String
	d.ApprovedBy = approvedBy.String
	if approvedAt.Valid {
		v := approvedAt.Int64
		d.ApprovedAt = &v
	}
	if lastSeen.Valid {
		v := lastSeen.Int64
		d.LastSeenAt = &v
	}
	return &d, nil
}

func (d *DB) CreateDevice(ctx context.Context, q Querier, dev *domain.Device, pairSecretHash string) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO devices (id, device_uid, device_key, user_id, family_id, role, model, os_version, app_version,
			ip, status, pair_code, pair_secret, created_at, expires_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		dev.ID, dev.DeviceUID, dev.DeviceKey, nullIfEmpty(dev.UserID), nullIfEmpty(dev.FamilyID), nullIfEmpty(dev.Role),
		dev.Model, dev.OSVersion, dev.AppVersion, dev.IP, dev.Status,
		dev.PairCode, pairSecretHash, dev.CreatedAt, dev.ExpiresAt)
	return err
}

// DeviceByPairCode 取未结束的配对请求
func (d *DB) DeviceByPairCode(ctx context.Context, q Querier, pairCode string) (*domain.Device, string, error) {
	var dev domain.Device
	var userID, familyID, role, secret sql.NullString
	var approvedAt, lastSeen sql.NullInt64
	var approvedBy sql.NullString
	err := q.QueryRowContext(ctx, `
		SELECT id, device_uid, device_key, user_id, family_id, role, model, os_version, app_version,
		       ip, status, pair_code, pair_secret, created_at, expires_at, approved_at, approved_by, last_seen_at
		FROM devices WHERE pair_code = ? ORDER BY created_at DESC LIMIT 1`, pairCode).
		Scan(&dev.ID, &dev.DeviceUID, &dev.DeviceKey, &userID, &familyID, &role, &dev.Model, &dev.OSVersion,
			&dev.AppVersion, &dev.IP, &dev.Status, &dev.PairCode, &secret, &dev.CreatedAt, &dev.ExpiresAt,
			&approvedAt, &approvedBy, &lastSeen)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", domain.ErrNotFound
		}
		return nil, "", err
	}
	dev.UserID = userID.String
	dev.FamilyID = familyID.String
	dev.Role = role.String
	dev.ApprovedBy = approvedBy.String
	if approvedAt.Valid {
		v := approvedAt.Int64
		dev.ApprovedAt = &v
	}
	if lastSeen.Valid {
		v := lastSeen.Int64
		dev.LastSeenAt = &v
	}
	return &dev, secret.String, nil
}

func (d *DB) DeviceByID(ctx context.Context, q Querier, id string) (*domain.Device, error) {
	return scanDevice(q.QueryRowContext(ctx, `SELECT `+deviceColumns+` FROM devices WHERE id = ?`, id))
}

func (d *DB) PendingDevices(ctx context.Context, q Querier, limit int) ([]domain.Device, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := q.QueryContext(ctx, `SELECT `+deviceColumns+`
		FROM devices WHERE status = 'pending' ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Device, 0, limit)
	for rows.Next() {
		dev, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *dev)
	}
	return out, rows.Err()
}

func (d *DB) AllDevices(ctx context.Context, q Querier, limit int, includeExpired bool) ([]domain.Device, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	// 过期的配对请求没有价值，默认不展示
	query := `SELECT ` + deviceColumns + ` FROM devices ORDER BY created_at DESC LIMIT ?`
	if !includeExpired {
		query = `SELECT ` + deviceColumns + ` FROM devices WHERE status <> 'expired' ORDER BY created_at DESC LIMIT ?`
	}
	rows, err := q.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Device, 0, limit)
	for rows.Next() {
		dev, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *dev)
	}
	return out, rows.Err()
}

// ApproveDevice 绑定用户/家庭/角色
func (d *DB) ApproveDevice(ctx context.Context, q Querier, id, userID, familyID, role, approvedBy string, now int64) error {
	_, err := q.ExecContext(ctx, `
		UPDATE devices SET status = 'approved', user_id = ?, family_id = ?, role = ?,
			approved_at = ?, approved_by = ?, last_seen_at = ?
		WHERE id = ? AND status = 'pending'`,
		userID, nullIfEmpty(familyID), nullIfEmpty(role), now, approvedBy, now, id)
	return err
}

func (d *DB) SetDeviceStatus(ctx context.Context, q Querier, id, status string, now int64) error {
	_, err := q.ExecContext(ctx, `UPDATE devices SET status = ? WHERE id = ?`, status, id)
	return err
}

func (d *DB) TouchDevice(ctx context.Context, q Querier, id string, now int64) {
	_, _ = q.ExecContext(ctx, `UPDATE devices SET last_seen_at = ? WHERE id = ?`, now, id)
}

// ExpireStaleDevices 把超时未批准的配对请求标记为 expired
func (d *DB) ExpireStaleDevices(ctx context.Context, q Querier, now int64) (int64, error) {
	res, err := q.ExecContext(ctx,
		`UPDATE devices SET status = 'expired' WHERE status = 'pending' AND expires_at < ?`, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CountRecentPendingByIP 同一 IP 最近的待授权数量（防刷）
func (d *DB) CountRecentPendingByIP(ctx context.Context, q Querier, ip string, since int64) (int, error) {
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM devices WHERE ip = ? AND created_at >= ?`, ip, since).Scan(&n)
	return n, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// LastApprovedByKey 找同一台实体设备上一次被批准的记录（用于提示"已知设备"）
func (d *DB) LastApprovedByKey(ctx context.Context, q Querier, deviceKey, excludeID string) (*domain.Device, error) {
	if deviceKey == "" {
		return nil, domain.ErrNotFound
	}
	return scanDevice(q.QueryRowContext(ctx, `SELECT `+deviceColumns+`
		FROM devices WHERE device_key = ? AND status = 'approved' AND id <> ?
		ORDER BY approved_at DESC LIMIT 1`, deviceKey, excludeID))
}

// SupersedeDevicesByKey 同一台设备重新配对成功后，把旧记录标记为 superseded 并作废其令牌，
// 避免一台手机在系统里留下多条"已授权"记录、绑到不同账号。
func (d *DB) SupersedeDevicesByKey(ctx context.Context, q Querier, deviceKey, keepID string, now int64) ([]string, error) {
	if deviceKey == "" {
		return nil, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT id FROM devices
		WHERE device_key = ? AND status = 'approved' AND id <> ?`, deviceKey, keepID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err := q.ExecContext(ctx, `UPDATE devices SET status = 'superseded' WHERE id = ?`, id); err != nil {
			return nil, err
		}
	}
	return ids, nil
}
