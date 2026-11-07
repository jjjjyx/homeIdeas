package service

import (
	"context"
	"database/sql"
	"strings"
	"unicode/utf8"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/importer"
	"family-ledger/server/internal/pkg/id"
	"family-ledger/server/internal/store"
)

type ImportSummary struct {
	BatchID    string   `json:"batch_id"`
	Source     string   `json:"source"`
	SourceName string   `json:"source_name"`
	Total      int      `json:"total"`
	Imported   int      `json:"imported"`
	Duplicated int      `json:"duplicated"`
	Skipped    int      `json:"skipped"`
	Failed     int      `json:"failed"`
	Warnings   []string `json:"warnings,omitempty"`
}

// ImportBill 处理 App 上传的账单文件。
func (s *LedgerService) ImportBill(
	ctx context.Context,
	familyID, userID, fileName string,
	data []byte,
	password string,
) (*ImportSummary, error) {
	member, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID)
	if err != nil {
		return nil, err
	}
	if member.Role == domain.RoleMember {
		return nil, domain.ErrForbidden.WithMessage("只有户主或管理员可以导入账单")
	}
	if len(data) == 0 {
		return nil, domain.ErrBadRequest.WithMessage("文件内容为空")
	}
	if len(data) > 32<<20 {
		return nil, domain.ErrBadRequest.WithMessage("文件超过 32MB")
	}

	parsed, err := importer.ParseAny(fileName, data, password)
	if err != nil {
		return nil, domain.ErrBadRequest.WithMessage(err.Error())
	}
	return s.importRecords(ctx, familyID, userID, parsed, fileName)
}

// importRecords 把解析结果写库。
//
// 去重靠 (family_id, external_id) 唯一索引：同一个交易单号只会进来一次，
// 所以同一份账单重复导入、邮件重复拉取都不会产生重复账目。
func (s *LedgerService) importRecords(
	ctx context.Context,
	familyID, userID string,
	parsed *importer.Result,
	fileName string,
) (*ImportSummary, error) {
	categories, err := s.db.CategoriesOfFamily(ctx, s.db.SQL(), familyID)
	if err != nil {
		return nil, err
	}
	rules, err := s.db.RulesOfFamily(ctx, s.db.SQL(), familyID)
	if err != nil {
		return nil, err
	}

	summary := &ImportSummary{
		BatchID:    id.New(),
		Source:     string(parsed.Source),
		SourceName: parsed.Source.Label(),
		Total:      len(parsed.Records),
		Skipped:    parsed.Skipped,
		Warnings:   parsed.Warnings,
	}

	now := store.NowMS()
	err = s.db.WithTx(ctx, func(dbtx *sql.Tx) error {
		for _, rec := range parsed.Records {
			categoryID, matched := pickCategory(rec, categories, rules)
			if categoryID == "" {
				summary.Failed++
				continue
			}

			rev, err := s.db.BumpRev(ctx, dbtx, familyID, now)
			if err != nil {
				return err
			}

			item := &domain.Transaction{
				ID:          id.New(),
				FamilyID:    familyID,
				Type:        rec.Direction,
				AmountCents: rec.AmountCents,
				CategoryID:  categoryID,
				MemberID:    userID,
				OccurredAt:  rec.OccurredAt.UnixMilli(),
				Note:        buildNote(rec),
				Visibility:  domain.VisibilityFamily,
				Rev:         rev,
				CreatedAt:   now,
				UpdatedAt:   now,
				Source:      string(parsed.Source),
				ExternalID:  rec.ExternalID,
			}

			inserted, err := s.db.InsertTransactionIfNew(ctx, dbtx, item)
			if err != nil {
				return err
			}
			if !inserted {
				summary.Duplicated++
				continue
			}
			summary.Imported++
			if matched != "" {
				_ = s.db.BumpRuleHits(ctx, dbtx, familyID, matched)
			}
		}

		return s.db.InsertImportBatch(ctx, dbtx, &store.ImportBatch{
			ID:        summary.BatchID,
			FamilyID:  familyID,
			Source:    summary.Source,
			FileName:  fileName,
			Total:     summary.Total,
			Imported:  summary.Imported,
			Skipped:   summary.Skipped,
			Failed:    summary.Failed,
			Status:    "done",
			CreatedBy: userID,
			CreatedAt: now,
		})
	})
	if err != nil {
		return nil, err
	}
	return summary, nil
}

// pickCategory 先套用学习到的规则（商户名/商品关键词），命中不了再按类型找默认分类。
func pickCategory(rec importer.Record, categories []store.Category, rules []store.CategoryRule) (categoryID, matchedKeyword string) {
	byID := make(map[string]store.Category, len(categories))
	for _, c := range categories {
		byID[c.ID] = c
	}

	haystack := strings.ToLower(rec.Counterparty + " " + rec.Item)
	for _, rule := range rules {
		keyword := strings.ToLower(strings.TrimSpace(rule.Keyword))
		if keyword == "" {
			continue
		}
		c, ok := byID[rule.CategoryID]
		if !ok || c.Type != rec.Direction {
			continue // 规则指向的分类被删了或类型不匹配
		}
		if strings.Contains(haystack, keyword) {
			return rule.CategoryID, rule.Keyword
		}
	}

	defaultName := "其他"
	if rec.Direction == domain.TypeIncome {
		defaultName = "其他收入"
	}
	for _, c := range categories {
		if c.Name == defaultName && c.Type == rec.Direction {
			return c.ID, ""
		}
	}
	for _, c := range categories {
		if c.Type == rec.Direction {
			return c.ID, ""
		}
	}
	return "", ""
}

func buildNote(rec importer.Record) string {
	parts := make([]string, 0, 2)
	if v := strings.TrimSpace(rec.Counterparty); v != "" && v != "/" {
		parts = append(parts, v)
	}
	if v := strings.TrimSpace(rec.Item); v != "" && v != "/" && v != rec.Counterparty {
		parts = append(parts, v)
	}
	note := strings.Join(parts, " · ")
	if utf8.RuneCountInString(note) > 200 {
		note = string([]rune(note)[:200])
	}
	return note
}

// ListImports 最近的导入记录
func (s *LedgerService) ListImports(ctx context.Context, familyID, userID string, limit int) ([]store.ImportBatch, error) {
	if _, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.db.ListImportBatches(ctx, s.db.SQL(), familyID, limit)
}

// SaveCategoryRule 记住「这个商户属于哪个分类」，下次导入自动套用。
func (s *LedgerService) SaveCategoryRule(ctx context.Context, familyID, userID, keyword, categoryID string) error {
	member, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID)
	if err != nil {
		return err
	}
	if member.Role == domain.RoleMember {
		return domain.ErrForbidden.WithMessage("只有户主或管理员可以维护分类规则")
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" || utf8.RuneCountInString(keyword) > 40 {
		return domain.ErrBadRequest.WithMessage("关键词需为 1-40 个字")
	}
	ok, err := s.db.CategoryBelongsToFamily(ctx, s.db.SQL(), categoryID, familyID)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrBadRequest.WithMessage("分类不属于该家庭")
	}
	return s.db.UpsertRule(ctx, s.db.SQL(), id.New(), familyID, keyword, categoryID, store.NowMS())
}
