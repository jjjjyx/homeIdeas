package store

import (
	"context"
	"database/sql"
)

// ScopeSum 按「开支归属」汇总
type ScopeSum struct {
	Scope   string
	OwnerID string
	Amount  int64
	Count   int
}

// scopeFilter 生成可选的 scope / owner 过滤条件
func scopeFilter(scope, ownerID string) (string, []any) {
	where := ""
	args := make([]any, 0, 4)
	if scope != "" {
		where += " AND scope = ?"
		args = append(args, scope)
	}
	if ownerID != "" {
		where += " AND owner_id = ?"
		args = append(args, ownerID)
	}
	return where, args
}

// SumByTypeScoped 同 SumByType，但可以按 scope / owner 过滤
func (d *DB) SumByTypeScoped(ctx context.Context, q Querier, familyID, viewerID string, fromMS, toMS int64, scope, ownerID string) ([]TypeSum, error) {
	extra, extraArgs := scopeFilter(scope, ownerID)
	if scope == "" {
		extra += " AND scope <> 'transfer'"
	}

	sqlStr := `
		SELECT type, COALESCE(SUM(amount_cents),0), COUNT(*)
		FROM transactions
		WHERE family_id = ? AND deleted_at IS NULL AND occurred_at >= ? AND occurred_at < ?
		  AND (visibility <> 'private' OR member_id = ?)` + extra + `
		GROUP BY type`

	args := []any{familyID, fromMS, toMS, viewerID}
	args = append(args, extraArgs...)

	rows, err := q.QueryContext(ctx, sqlStr, args...)
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

// SumByCategoryScoped 同 SumByCategory，但可以按 scope / owner 过滤
func (d *DB) SumByCategoryScoped(ctx context.Context, q Querier, familyID, viewerID string, fromMS, toMS int64, typ int, scope, ownerID string) ([]CategorySum, error) {
	extra, extraArgs := scopeFilter(scope, ownerID)
	if scope == "" {
		extra += " AND scope <> 'transfer'"
	}

	sqlStr := `
		SELECT category_id, COALESCE(SUM(amount_cents),0), COUNT(*)
		FROM transactions
		WHERE family_id = ? AND deleted_at IS NULL AND occurred_at >= ? AND occurred_at < ?
		  AND type = ? AND (visibility <> 'private' OR member_id = ?)` + extra + `
		GROUP BY category_id
		ORDER BY 2 DESC`

	args := []any{familyID, fromMS, toMS, typ, viewerID}
	args = append(args, extraArgs...)

	rows, err := q.QueryContext(ctx, sqlStr, args...)
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

// SumByScope 按「家庭/个人 + 归属人」拆分支出
func (d *DB) SumByScope(ctx context.Context, q Querier, familyID, viewerID string, fromMS, toMS int64) ([]ScopeSum, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT scope, owner_id, COALESCE(SUM(amount_cents),0), COUNT(*)
		FROM transactions
		WHERE family_id = ? AND deleted_at IS NULL AND is_split = 0 AND occurred_at >= ? AND occurred_at < ?
		  AND type = 0 AND (visibility <> 'private' OR member_id = ?)
		GROUP BY scope, owner_id
		ORDER BY 3 DESC`, familyID, fromMS, toMS, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ScopeSum, 0, 6)
	for rows.Next() {
		var s ScopeSum
		if err := rows.Scan(&s.Scope, &s.OwnerID, &s.Amount, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// PendingReview 待人工确认的账目（AI 置信度低或还没审过）
type PendingRow struct {
	ID          string
	Note        string
	AmountCents int64
	OccurredAt  int64
	Scope       string
	OwnerID     string
	CategoryID  string
	MemberID    string
	Confidence  float64
	Reason      string
}

// PendingReview 列出需要人工确认的账目
func (d *DB) PendingReview(ctx context.Context, q Querier, familyID, viewerID string, minConfidence float64, limit int) ([]PendingRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := q.QueryContext(ctx, `
		SELECT id, note, amount_cents, occurred_at, scope, owner_id, category_id, member_id, ai_confidence, ai_reason
		FROM transactions
		WHERE family_id = ? AND deleted_at IS NULL AND is_split = 0 AND reviewed = 0 AND type = 0
		  AND (visibility <> 'private' OR member_id = ?)
		  AND (scope = 'unknown' OR ai_confidence < ?)
		ORDER BY occurred_at DESC
		LIMIT ?`, familyID, viewerID, minConfidence, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]PendingRow, 0, limit)
	for rows.Next() {
		var p PendingRow
		var ownerID sql.NullString
		if err := rows.Scan(&p.ID, &p.Note, &p.AmountCents, &p.OccurredAt, &p.Scope,
			&ownerID, &p.CategoryID, &p.MemberID, &p.Confidence, &p.Reason); err != nil {
			return nil, err
		}
		p.OwnerID = ownerID.String
		out = append(out, p)
	}
	return out, rows.Err()
}

// UnclassifiedMerchant 待归类的商户聚合
type UnclassifiedMerchant struct {
	Merchant    string
	Count       int
	AmountCents int64
	SampleNote  string
}

// MerchantsOfUnclassified 把「待确认」的账目按商户聚合，供批量归类使用
func (d *DB) MerchantsOfUnclassified(ctx context.Context, q Querier, familyID, viewerID string, limit int) ([]UnclassifiedMerchant, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := q.QueryContext(ctx, `
		SELECT merchant, COUNT(*) AS c, COALESCE(SUM(amount_cents),0), MIN(note)
		FROM (
			SELECT amount_cents, note,
			       CASE WHEN instr(note, ' · ') > 0
			            THEN substr(note, 1, instr(note, ' · ') - 1)
			            ELSE note END AS merchant
			FROM transactions
			WHERE family_id = ? AND deleted_at IS NULL AND is_split = 0 AND type = 0 AND reviewed = 0
			  AND (visibility <> 'private' OR member_id = ?)
			  AND (scope = 'unknown' OR ai_confidence < 0.7)
		)
		GROUP BY merchant
		ORDER BY c DESC
		LIMIT ?`, familyID, viewerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]UnclassifiedMerchant, 0, limit)
	for rows.Next() {
		var m UnclassifiedMerchant
		if err := rows.Scan(&m.Merchant, &m.Count, &m.AmountCents, &m.SampleNote); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UnclassifiedTransactions 取出待 AI 分类的账目（家庭数据，不做可见性过滤——分类是系统行为）
func (d *DB) UnclassifiedTransactions(ctx context.Context, q Querier, familyID string, limit int) ([]PendingRow, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := q.QueryContext(ctx, `
		SELECT id, note, amount_cents, occurred_at, scope, owner_id, category_id, member_id, ai_confidence, ai_reason
		FROM transactions
		WHERE family_id = ? AND deleted_at IS NULL AND type = 0 AND reviewed = 0
		  AND (scope = 'unknown' OR ai_confidence = 0)
		ORDER BY occurred_at DESC
		LIMIT ?`, familyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]PendingRow, 0, limit)
	for rows.Next() {
		var p PendingRow
		var ownerID sql.NullString
		if err := rows.Scan(&p.ID, &p.Note, &p.AmountCents, &p.OccurredAt, &p.Scope,
			&ownerID, &p.CategoryID, &p.MemberID, &p.Confidence, &p.Reason); err != nil {
			return nil, err
		}
		p.OwnerID = ownerID.String
		out = append(out, p)
	}
	return out, rows.Err()
}

// ApplyClassification 写回 AI 判断结果
func (d *DB) ApplyClassification(ctx context.Context, q Querier, familyID, id, scope, ownerID, categoryID string,
	confidence float64, reason, source string, rev, now int64) error {
	_, err := q.ExecContext(ctx, `
		UPDATE transactions
		SET scope = ?, owner_id = ?, category_id = ?, category_source = ?,
		    ai_confidence = ?, ai_reason = ?, rev = ?, updated_at = ?
		WHERE id = ? AND family_id = ?`,
		scope, ownerID, categoryID, source, confidence, reason, rev, now, id, familyID)
	return err
}
