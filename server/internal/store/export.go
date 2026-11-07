package store

import (
	"context"
)

// ExportRow 导出一行账目（已 join 分类名、账户名、记账人）
type ExportRow struct {
	OccurredAt   int64
	Type         int
	AmountCents  int64
	CategoryName string
	AccountName  string
	Note         string
	MemberName   string
	Visibility   string
}

// ExportTransactions 导出某家庭在时间区间内的账目（不含已删除）。
// viewerID 用于私密账过滤：别人看不到的账，导出也不能带出去。
func (d *DB) ExportTransactions(ctx context.Context, q Querier, familyID, viewerID string, fromMS, toMS int64) ([]ExportRow, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT t.occurred_at, t.type, t.amount_cents,
		       COALESCE(c.name, '已删除分类'),
		       COALESCE(a.name, ''),
		       t.note,
		       COALESCE(u.nickname, ''),
		       t.visibility
		FROM transactions t
		LEFT JOIN categories c ON c.id = t.category_id
		LEFT JOIN accounts a ON a.id = t.account_id
		LEFT JOIN users u ON u.id = t.member_id
		WHERE t.family_id = ? AND t.deleted_at IS NULL AND t.is_split = 0
		  AND t.occurred_at >= ? AND t.occurred_at < ?
		  AND (t.visibility <> 'private' OR t.member_id = ?)
		ORDER BY t.occurred_at ASC`, familyID, fromMS, toMS, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ExportRow, 0, 256)
	for rows.Next() {
		var r ExportRow
		if err := rows.Scan(&r.OccurredAt, &r.Type, &r.AmountCents, &r.CategoryName,
			&r.AccountName, &r.Note, &r.MemberName, &r.Visibility); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
