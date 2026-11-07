package store

import (
	"context"
	"database/sql"
	"errors"

	"family-ledger/server/internal/domain"
)

type MailAccount struct {
	ID                 string
	FamilyID           string
	Host               string
	Port               int
	Username           string
	PasswordEnc        string
	ExtractPasswordEnc string
	UseTLS             bool
	LastUID            int64
	LastSyncAt         *int64
	Enabled            bool
	CreatedAt          int64
	UpdatedAt          int64
}

func (d *DB) UpsertMailAccount(ctx context.Context, q Querier, a *MailAccount) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO mail_accounts (id, family_id, host, port, username, password_enc, extract_password_enc,
			use_tls, last_uid, last_sync_at, enabled, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(family_id) DO UPDATE SET
			host = excluded.host, port = excluded.port, username = excluded.username,
			password_enc = excluded.password_enc,
			extract_password_enc = CASE WHEN excluded.extract_password_enc <> '' THEN excluded.extract_password_enc ELSE mail_accounts.extract_password_enc END,
			use_tls = excluded.use_tls, enabled = excluded.enabled,
			last_uid = excluded.last_uid, updated_at = excluded.updated_at`,
		a.ID, a.FamilyID, a.Host, a.Port, a.Username, a.PasswordEnc, a.ExtractPasswordEnc,
		boolToInt(a.UseTLS), a.LastUID, a.LastSyncAt, boolToInt(a.Enabled), a.CreatedAt, a.UpdatedAt)
	return err
}

func (d *DB) MailAccountOfFamily(ctx context.Context, q Querier, familyID string) (*MailAccount, error) {
	var a MailAccount
	var useTLS, enabled int
	var lastSync sql.NullInt64
	err := q.QueryRowContext(ctx, `
		SELECT id, family_id, host, port, username, password_enc, extract_password_enc,
		       use_tls, last_uid, last_sync_at, enabled, created_at, updated_at
		FROM mail_accounts WHERE family_id = ?`, familyID).
		Scan(&a.ID, &a.FamilyID, &a.Host, &a.Port, &a.Username, &a.PasswordEnc, &a.ExtractPasswordEnc,
			&useTLS, &a.LastUID, &lastSync, &enabled, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	a.UseTLS = useTLS == 1
	a.Enabled = enabled == 1
	if lastSync.Valid {
		v := lastSync.Int64
		a.LastSyncAt = &v
	}
	return &a, nil
}

func (d *DB) ListEnabledMailAccounts(ctx context.Context, q Querier) ([]MailAccount, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, family_id, host, port, username, password_enc, extract_password_enc,
		       use_tls, last_uid, last_sync_at, enabled, created_at, updated_at
		FROM mail_accounts WHERE enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]MailAccount, 0, 8)
	for rows.Next() {
		var a MailAccount
		var useTLS, enabled int
		var lastSync sql.NullInt64
		if err := rows.Scan(&a.ID, &a.FamilyID, &a.Host, &a.Port, &a.Username, &a.PasswordEnc,
			&a.ExtractPasswordEnc, &useTLS, &a.LastUID, &lastSync, &enabled, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.UseTLS = useTLS == 1
		a.Enabled = enabled == 1
		if lastSync.Valid {
			v := lastSync.Int64
			a.LastSyncAt = &v
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (d *DB) UpdateMailProgress(ctx context.Context, q Querier, familyID string, lastUID int64, now int64) error {
	_, err := q.ExecContext(ctx,
		`UPDATE mail_accounts SET last_uid = ?, last_sync_at = ?, updated_at = ? WHERE family_id = ?`,
		lastUID, now, now, familyID)
	return err
}

// MailMessage 处理过的邮件（按 UID 去重，避免重复导入）
type MailMessage struct {
	ID         string
	FamilyID   string
	MessageUID int64
	Subject    string
	FromAddr   string
	Status     string // done / need_password / failed / skipped
	Detail     string
	CreatedAt  int64
	// PasswordEnc 这封邮件账单的解压密码密文（每份账单密码不同，所以按邮件存）
	PasswordEnc string
}

func (d *DB) InsertMailMessage(ctx context.Context, q Querier, m *MailMessage) (bool, error) {
	res, err := q.ExecContext(ctx, `
		INSERT OR IGNORE INTO mail_messages (id, family_id, message_uid, subject, from_addr, status, detail, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		m.ID, m.FamilyID, m.MessageUID, m.Subject, m.FromAddr, m.Status, m.Detail, m.CreatedAt)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (d *DB) UpdateMailMessage(ctx context.Context, q Querier, id, status, detail string) error {
	_, err := q.ExecContext(ctx, `UPDATE mail_messages SET status = ?, detail = ? WHERE id = ?`, status, detail, id)
	return err
}

func (d *DB) ListMailMessages(ctx context.Context, q Querier, familyID string, limit int) ([]MailMessage, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, family_id, message_uid, subject, from_addr, status, detail, created_at, password_enc
		FROM mail_messages WHERE family_id = ? ORDER BY created_at DESC LIMIT ?`, familyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]MailMessage, 0, limit)
	for rows.Next() {
		var m MailMessage
		if err := rows.Scan(&m.ID, &m.FamilyID, &m.MessageUID, &m.Subject, &m.FromAddr, &m.Status, &m.Detail, &m.CreatedAt, &m.PasswordEnc); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) MailMessagesByStatus(ctx context.Context, q Querier, familyID, status string) ([]MailMessage, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, family_id, message_uid, subject, from_addr, status, detail, created_at, password_enc
		FROM mail_messages WHERE family_id = ? AND status = ? ORDER BY created_at DESC`, familyID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]MailMessage, 0, 8)
	for rows.Next() {
		var m MailMessage
		if err := rows.Scan(&m.ID, &m.FamilyID, &m.MessageUID, &m.Subject, &m.FromAddr, &m.Status, &m.Detail, &m.CreatedAt, &m.PasswordEnc); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// DeleteMailMessages 清空某家庭的邮件处理记录，用于「重新扫描」
func (d *DB) DeleteMailMessages(ctx context.Context, q Querier, familyID string) error {
	_, err := q.ExecContext(ctx, `DELETE FROM mail_messages WHERE family_id = ?`, familyID)
	return err
}

// MailMessageByID 取单封邮件的处理记录（含解压密码密文）
func (d *DB) MailMessageByID(ctx context.Context, q Querier, familyID, id string) (*MailMessage, error) {
	var m MailMessage
	err := q.QueryRowContext(ctx, `
		SELECT id, family_id, message_uid, subject, from_addr, status, detail, created_at, password_enc
		FROM mail_messages WHERE id = ? AND family_id = ?`, id, familyID).
		Scan(&m.ID, &m.FamilyID, &m.MessageUID, &m.Subject, &m.FromAddr, &m.Status, &m.Detail, &m.CreatedAt, &m.PasswordEnc)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &m, nil
}

// SetMailMessagePassword 保存某封邮件的解压密码密文
func (d *DB) SetMailMessagePassword(ctx context.Context, q Querier, id, passwordEnc string) error {
	_, err := q.ExecContext(ctx, `UPDATE mail_messages SET password_enc = ? WHERE id = ?`, passwordEnc, id)
	return err
}
