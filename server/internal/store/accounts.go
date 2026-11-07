package store

import (
	"context"
	"database/sql"
	"errors"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/pkg/id"
)

const accountColumns = `id, family_id, name, kind, sort_order, rev, created_at, updated_at, deleted_at`

func scanAccount(row rowScanner) (*domain.Account, error) {
	var a domain.Account
	var deletedAt sql.NullInt64
	if err := row.Scan(&a.ID, &a.FamilyID, &a.Name, &a.Kind, &a.SortOrder,
		&a.Rev, &a.CreatedAt, &a.UpdatedAt, &deletedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	if deletedAt.Valid {
		v := deletedAt.Int64
		a.DeletedAt = &v
	}
	return &a, nil
}

func (d *DB) InsertAccount(ctx context.Context, q Querier, a *domain.Account) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO accounts (`+accountColumns+`) VALUES (?,?,?,?,?,?,?,?,NULL)`,
		a.ID, a.FamilyID, a.Name, a.Kind, a.SortOrder, a.Rev, a.CreatedAt, a.UpdatedAt)
	return err
}

func (d *DB) AccountsOfFamily(ctx context.Context, q Querier, familyID string) ([]domain.Account, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+accountColumns+` FROM accounts WHERE family_id = ? AND deleted_at IS NULL ORDER BY sort_order ASC`, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Account, 0, 8)
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (d *DB) AccountsChangedSince(ctx context.Context, q Querier, familyID string, since int64, limit int) ([]domain.Account, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+accountColumns+` FROM accounts WHERE family_id = ? AND rev > ? ORDER BY rev ASC LIMIT ?`,
		familyID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Account, 0, limit)
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

type seedAccount struct {
	Name string
	Kind string
}

var defaultAccounts = []seedAccount{
	{"现金", "cash"},
	{"支付宝", "alipay"},
	{"微信", "wechat"},
	{"银行卡", "bank"},
}

func (d *DB) SeedDefaultAccounts(ctx context.Context, q Querier, familyID string, rev int64, now int64) error {
	for i, a := range defaultAccounts {
		_, err := q.ExecContext(ctx, `
			INSERT INTO accounts (id, family_id, name, kind, sort_order, rev, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?)`,
			id.New(), familyID, a.Name, a.Kind, i, rev, now, now)
		if err != nil {
			return err
		}
	}
	return nil
}
