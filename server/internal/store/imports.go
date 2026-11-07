package store

import (
	"context"
)

type ImportBatch struct {
	ID        string
	FamilyID  string
	Source    string
	FileName  string
	Total     int
	Imported  int
	Skipped   int
	Failed    int
	Status    string
	Message   string
	CreatedBy string
	CreatedAt int64
}

func (d *DB) InsertImportBatch(ctx context.Context, q Querier, b *ImportBatch) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO import_batches (id, family_id, source, file_name, total, imported, skipped, failed, status, message, created_by, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.FamilyID, b.Source, b.FileName, b.Total, b.Imported, b.Skipped,
		b.Failed, b.Status, b.Message, b.CreatedBy, b.CreatedAt)
	return err
}

func (d *DB) ListImportBatches(ctx context.Context, q Querier, familyID string, limit int) ([]ImportBatch, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, family_id, source, file_name, total, imported, skipped, failed, status, message, created_by, created_at
		FROM import_batches WHERE family_id = ? ORDER BY created_at DESC LIMIT ?`, familyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ImportBatch, 0, limit)
	for rows.Next() {
		var b ImportBatch
		if err := rows.Scan(&b.ID, &b.FamilyID, &b.Source, &b.FileName, &b.Total,
			&b.Imported, &b.Skipped, &b.Failed, &b.Status, &b.Message, &b.CreatedBy, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// CategoryRule 商户名/关键词 → 分类的映射，导入时自动套用
type CategoryRule struct {
	ID         string
	FamilyID   string
	Keyword    string
	CategoryID string
	Priority   int
	Hits       int
}

func (d *DB) RulesOfFamily(ctx context.Context, q Querier, familyID string) ([]CategoryRule, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, family_id, keyword, category_id, priority, hits
		FROM category_rules WHERE family_id = ? ORDER BY priority DESC, hits DESC, length(keyword) DESC`,
		familyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]CategoryRule, 0, 32)
	for rows.Next() {
		var r CategoryRule
		if err := rows.Scan(&r.ID, &r.FamilyID, &r.Keyword, &r.CategoryID, &r.Priority, &r.Hits); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) UpsertRule(ctx context.Context, q Querier, id, familyID, keyword, categoryID string, now int64) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO category_rules (id, family_id, keyword, category_id, priority, hits, created_at, updated_at)
		VALUES (?,?,?,?,10,0,?,?)
		ON CONFLICT(family_id, keyword) DO UPDATE SET category_id = excluded.category_id, updated_at = excluded.updated_at`,
		id, familyID, keyword, categoryID, now, now)
	return err
}

func (d *DB) BumpRuleHits(ctx context.Context, q Querier, familyID, keyword string) error {
	_, err := q.ExecContext(ctx,
		`UPDATE category_rules SET hits = hits + 1 WHERE family_id = ? AND keyword = ?`, familyID, keyword)
	return err
}
