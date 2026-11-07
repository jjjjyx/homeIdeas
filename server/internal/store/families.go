package store

import (
	"context"
	"database/sql"
	"errors"

	"family-ledger/server/internal/domain"
)

const familyColumns = `id, name, owner_id, currency, rev, created_at, updated_at`

func scanFamily(s rowScanner) (*domain.Family, error) {
	var f domain.Family
	if err := s.Scan(&f.ID, &f.Name, &f.OwnerID, &f.Currency, &f.Rev, &f.CreatedAt, &f.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &f, nil
}

func (d *DB) CreateFamily(ctx context.Context, q Querier, f *domain.Family) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO families (`+familyColumns+`) VALUES (?,?,?,?,?,?,?)`,
		f.ID, f.Name, f.OwnerID, f.Currency, f.Rev, f.CreatedAt, f.UpdatedAt)
	return err
}

func (d *DB) FamilyByID(ctx context.Context, q Querier, id string) (*domain.Family, error) {
	return scanFamily(q.QueryRowContext(ctx,
		`SELECT `+familyColumns+` FROM families WHERE id = ? AND deleted_at IS NULL`, id))
}

// FamilyCard 家庭列表项：带当前用户角色和成员数。
type FamilyCard struct {
	domain.Family
	Role        string
	MemberCount int
}

func (d *DB) FamiliesOfUser(ctx context.Context, q Querier, userID string) ([]FamilyCard, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT f.id, f.name, f.owner_id, f.currency, f.rev, f.created_at, f.updated_at,
		       m.role,
		       (SELECT COUNT(*) FROM family_members mm WHERE mm.family_id = f.id)
		FROM families f
		JOIN family_members m ON m.family_id = f.id
		WHERE m.user_id = ? AND f.deleted_at IS NULL
		ORDER BY f.created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]FamilyCard, 0, 4)
	for rows.Next() {
		var c FamilyCard
		if err := rows.Scan(&c.ID, &c.Name, &c.OwnerID, &c.Currency, &c.Rev,
			&c.CreatedAt, &c.UpdatedAt, &c.Role, &c.MemberCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddMemberIfAbsent 幂等加入：已经是成员时什么都不做（配对重试、重复批准都不报错）
func (d *DB) AddMemberIfAbsent(ctx context.Context, q Querier, familyID, userID, role string, now int64) error {
	_, err := q.ExecContext(ctx,
		`INSERT OR IGNORE INTO family_members (family_id, user_id, role, joined_at) VALUES (?,?,?,?)`,
		familyID, userID, role, now)
	return err
}

func (d *DB) AddMember(ctx context.Context, q Querier, familyID, userID, role string, now int64) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO family_members (family_id, user_id, role, joined_at) VALUES (?,?,?,?)`,
		familyID, userID, role, now)
	return err
}

func (d *DB) MemberOf(ctx context.Context, q Querier, familyID, userID string) (*domain.Member, error) {
	var m domain.Member
	err := q.QueryRowContext(ctx, `
		SELECT m.family_id, m.user_id, m.role, m.joined_at, u.nickname, u.account
		FROM family_members m JOIN users u ON u.id = m.user_id
		WHERE m.family_id = ? AND m.user_id = ?`, familyID, userID).
		Scan(&m.FamilyID, &m.UserID, &m.Role, &m.JoinedAt, &m.Nickname, &m.Account)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &m, nil
}

func (d *DB) MembersOf(ctx context.Context, q Querier, familyID string) ([]domain.Member, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT m.family_id, m.user_id, m.role, m.joined_at, u.nickname, u.account
		FROM family_members m JOIN users u ON u.id = m.user_id
		WHERE m.family_id = ?
		ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, m.joined_at ASC`, familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Member, 0, 4)
	for rows.Next() {
		var m domain.Member
		if err := rows.Scan(&m.FamilyID, &m.UserID, &m.Role, &m.JoinedAt, &m.Nickname, &m.Account); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) UpdateMemberRole(ctx context.Context, q Querier, familyID, userID, role string) error {
	_, err := q.ExecContext(ctx,
		`UPDATE family_members SET role = ? WHERE family_id = ? AND user_id = ?`, role, familyID, userID)
	return err
}

func (d *DB) RemoveMember(ctx context.Context, q Querier, familyID, userID string) error {
	_, err := q.ExecContext(ctx,
		`DELETE FROM family_members WHERE family_id = ? AND user_id = ?`, familyID, userID)
	return err
}

func (d *DB) CountMembers(ctx context.Context, q Querier, familyID string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM family_members WHERE family_id = ?`, familyID).Scan(&n)
	return n, err
}

func (d *DB) CountOwners(ctx context.Context, q Querier, familyID string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM family_members WHERE family_id = ? AND role = 'owner'`, familyID).Scan(&n)
	return n, err
}

// BumpRev 递增家庭同步版本号，任何同步实体变更都要调用。
func (d *DB) BumpRev(ctx context.Context, q Querier, familyID string, now int64) (int64, error) {
	var rev int64
	err := q.QueryRowContext(ctx,
		`UPDATE families SET rev = rev + 1, updated_at = ? WHERE id = ? RETURNING rev`, now, familyID).Scan(&rev)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, domain.ErrNotFound
		}
		return 0, err
	}
	return rev, nil
}

func (d *DB) RenameFamily(ctx context.Context, q Querier, familyID, name string, now int64) error {
	_, err := q.ExecContext(ctx,
		`UPDATE families SET name = ?, updated_at = ? WHERE id = ?`, name, now, familyID)
	return err
}
