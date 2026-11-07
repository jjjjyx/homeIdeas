package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"family-ledger/server/internal/domain"
)

const txColumns = `id, family_id, type, amount_cents, category_id, account_id, member_id,
	occurred_at, note, visibility, tags, rev, created_at, updated_at, deleted_at, source, external_id,
	scope, owner_id, category_source, ai_confidence, ai_reason, reviewed, parent_id, is_split`

func scanTx(row rowScanner) (*domain.Transaction, error) {
	var t domain.Transaction
	var accountID sql.NullString
	var deletedAt sql.NullInt64
	err := row.Scan(&t.ID, &t.FamilyID, &t.Type, &t.AmountCents, &t.CategoryID,
		&accountID, &t.MemberID, &t.OccurredAt, &t.Note, &t.Visibility, &t.Tags,
		&t.Rev, &t.CreatedAt, &t.UpdatedAt, &deletedAt, &t.Source, &t.ExternalID,
		&t.Scope, &t.OwnerID, &t.CategorySource, &t.AIConfidence, &t.AIReason, &t.Reviewed,
		&t.ParentID, &t.IsSplit)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	t.AccountID = accountID.String
	if deletedAt.Valid {
		v := deletedAt.Int64
		t.DeletedAt = &v
	}
	return &t, nil
}

func (d *DB) InsertTransaction(ctx context.Context, q Querier, t *domain.Transaction) error {
	var accountID any
	if t.AccountID != "" {
		accountID = t.AccountID
	}
	_, err := d.insertTransaction(ctx, q, t, accountID, false)
	return err
}

// InsertTransactionIfNew 账单导入专用：靠 (family_id, external_id) 唯一索引去重，
// 返回 true 表示真的插入了，false 表示这条已经导入过。
func (d *DB) InsertTransactionIfNew(ctx context.Context, q Querier, t *domain.Transaction) (bool, error) {
	var accountID any
	if t.AccountID != "" {
		accountID = t.AccountID
	}
	return d.insertTransaction(ctx, q, t, accountID, true)
}

func (d *DB) insertTransaction(ctx context.Context, q Querier, t *domain.Transaction, accountID any, ignoreDup bool) (bool, error) {
	verb := "INSERT"
	if ignoreDup {
		verb = "INSERT OR IGNORE"
	}
	sqlStr := verb + ` INTO transactions (` + txColumns + `) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,NULL,?,?,?,?,?,?,?,?,?,?)`
	res, err := q.ExecContext(ctx, sqlStr,
		t.ID, t.FamilyID, t.Type, t.AmountCents, t.CategoryID, accountID, t.MemberID,
		t.OccurredAt, t.Note, t.Visibility, t.Tags, t.Rev, t.CreatedAt, t.UpdatedAt,
		t.Source, t.ExternalID,
		defaultScope(t.Scope), t.OwnerID, t.CategorySource, t.AIConfidence, t.AIReason, boolInt(t.Reviewed),
		t.ParentID, boolInt(t.IsSplit))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (d *DB) UpdateTransaction(ctx context.Context, q Querier, t *domain.Transaction) error {
	var accountID any
	if t.AccountID != "" {
		accountID = t.AccountID
	}
	_, err := q.ExecContext(ctx, `
		UPDATE transactions SET type = ?, amount_cents = ?, category_id = ?, account_id = ?,
			occurred_at = ?, note = ?, visibility = ?, tags = ?, rev = ?, updated_at = ?,
			scope = ?, owner_id = ?, category_source = ?, ai_confidence = ?, ai_reason = ?, reviewed = ?,
			parent_id = ?, is_split = ?
		WHERE id = ? AND family_id = ?`,
		t.Type, t.AmountCents, t.CategoryID, accountID, t.OccurredAt, t.Note,
		t.Visibility, t.Tags, t.Rev, t.UpdatedAt,
		defaultScope(t.Scope), t.OwnerID, t.CategorySource, t.AIConfidence, t.AIReason, boolInt(t.Reviewed),
		t.ParentID, boolInt(t.IsSplit),
		t.ID, t.FamilyID)
	return err
}

// SoftDeleteTransaction 写墓碑并抬 rev，离线客户端才能感知到删除。
func (d *DB) SoftDeleteTransaction(ctx context.Context, q Querier, id, familyID string, rev, now int64) error {
	_, err := q.ExecContext(ctx, `
		UPDATE transactions SET deleted_at = ?, rev = ?, updated_at = ?
		WHERE id = ? AND family_id = ? AND deleted_at IS NULL`,
		now, rev, now, id, familyID)
	return err
}

func (d *DB) TransactionByID(ctx context.Context, q Querier, id, familyID string) (*domain.Transaction, error) {
	return scanTx(q.QueryRowContext(ctx, `SELECT `+txColumns+` FROM transactions WHERE id = ? AND family_id = ?`, id, familyID))
}

// TxFilter 列表查询条件，所有字段可选。
type TxFilter struct {
	FamilyID string
	FromMS   int64
	ToMS     int64
	Type     *int
	Category string
	MemberID string
	// ViewerID 用于私密账过滤：只返回 family 可见 + 自己创建的
	ViewerID string
	Limit    int
	Offset   int
}

func (d *DB) ListTransactions(ctx context.Context, q Querier, f TxFilter) ([]domain.Transaction, error) {
	var sb strings.Builder
	sb.WriteString(`SELECT ` + txColumns + ` FROM transactions WHERE family_id = ? AND deleted_at IS NULL AND is_split = 0`)
	args := []any{f.FamilyID}

	if f.FromMS > 0 {
		sb.WriteString(` AND occurred_at >= ?`)
		args = append(args, f.FromMS)
	}
	if f.ToMS > 0 {
		sb.WriteString(` AND occurred_at < ?`)
		args = append(args, f.ToMS)
	}
	if f.Type != nil {
		sb.WriteString(` AND type = ?`)
		args = append(args, *f.Type)
	}
	if f.Category != "" {
		sb.WriteString(` AND category_id = ?`)
		args = append(args, f.Category)
	}
	if f.MemberID != "" {
		sb.WriteString(` AND member_id = ?`)
		args = append(args, f.MemberID)
	}
	if f.ViewerID != "" {
		sb.WriteString(` AND (visibility <> 'private' OR member_id = ?)`)
		args = append(args, f.ViewerID)
	}
	sb.WriteString(` ORDER BY occurred_at DESC, id DESC LIMIT ? OFFSET ?`)
	args = append(args, f.Limit, f.Offset)

	rows, err := q.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Transaction, 0, f.Limit)
	for rows.Next() {
		t, err := scanTx(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// TransactionsChangedSince 增量同步：返回 rev > since 的行（含墓碑）。
func (d *DB) TransactionsChangedSince(ctx context.Context, q Querier, familyID, viewerID string, since int64, limit int) ([]domain.Transaction, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT `+txColumns+` FROM transactions
		WHERE family_id = ? AND rev > ? AND (visibility <> 'private' OR member_id = ?)
		ORDER BY rev ASC LIMIT ?`, familyID, since, viewerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Transaction, 0, limit)
	for rows.Next() {
		t, err := scanTx(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// SumByType 某时间区间内的收入/支出合计。
type TypeSum struct {
	Type   int
	Amount int64
	Count  int
}

func (d *DB) SumByType(ctx context.Context, q Querier, familyID, viewerID string, fromMS, toMS int64) ([]TypeSum, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT type, COALESCE(SUM(amount_cents),0), COUNT(*)
		FROM transactions
		WHERE family_id = ? AND deleted_at IS NULL AND is_split = 0 AND occurred_at >= ? AND occurred_at < ?
		  AND (visibility <> 'private' OR member_id = ?)
		  AND scope <> 'transfer'
		GROUP BY type`, familyID, fromMS, toMS, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]TypeSum, 0, 2)
	for rows.Next() {
		var s TypeSum
		if err := rows.Scan(&s.Type, &s.Amount, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type CategorySum struct {
	CategoryID string
	Amount     int64
	Count      int
}

func (d *DB) SumByCategory(ctx context.Context, q Querier, familyID, viewerID string, fromMS, toMS int64, typ int) ([]CategorySum, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT category_id, COALESCE(SUM(amount_cents),0), COUNT(*)
		FROM transactions
		WHERE family_id = ? AND deleted_at IS NULL AND is_split = 0 AND occurred_at >= ? AND occurred_at < ?
		  AND type = ? AND (visibility <> 'private' OR member_id = ?)
		  AND scope <> 'transfer'
		GROUP BY category_id
		ORDER BY 2 DESC`, familyID, fromMS, toMS, typ, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]CategorySum, 0, 8)
	for rows.Next() {
		var s CategorySum
		if err := rows.Scan(&s.CategoryID, &s.Amount, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func defaultScope(s string) string {
	if s == "" {
		return domain.ScopeUnknown
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ChildrenOf 列出某个父账目拆出来的子账目
func (d *DB) ChildrenOf(ctx context.Context, q Querier, familyID, parentID string) ([]domain.Transaction, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT `+txColumns+` FROM transactions
		 WHERE family_id = ? AND parent_id = ? AND deleted_at IS NULL
		 ORDER BY amount_cents DESC`, familyID, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Transaction, 0, 4)
	for rows.Next() {
		t, err := scanTx(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// MarkSplit 把父账目标记为「已拆分」并打墓碑：它从账本里消失，但数据仍在，
// 需要撤销拆分或审计时还能找回来。
func (d *DB) MarkSplit(ctx context.Context, q Querier, id, familyID string, now int64) error {
	rev, err := d.BumpRev(ctx, q, familyID, now)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `
		UPDATE transactions SET is_split = 1, deleted_at = ?, rev = ?, updated_at = ?
		WHERE id = ? AND family_id = ?`, now, rev, now, id, familyID)
	return err
}

// UnmarkSplit 撤销拆分时把父账目恢复回来
func (d *DB) UnmarkSplit(ctx context.Context, q Querier, id, familyID string, now int64) error {
	rev, err := d.BumpRev(ctx, q, familyID, now)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `
		UPDATE transactions SET is_split = 0, deleted_at = NULL, rev = ?, updated_at = ?
		WHERE id = ? AND family_id = ?`, rev, now, id, familyID)
	return err
}
