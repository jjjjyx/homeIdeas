package service

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"family-ledger/server/internal/config"
	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/importer"
	"family-ledger/server/internal/llm"
	"family-ledger/server/internal/pkg/id"
	"family-ledger/server/internal/store"
)

// DraftRow 预览阶段的一条记录（已经映射成分类 id）
type DraftRow struct {
	OccurredAt   int64  `json:"occurred_at"`
	Type         int    `json:"type"`
	AmountCents  int64  `json:"amount_cents"`
	CategoryID   string `json:"category_id"`
	CategoryName string `json:"category_name"`
	Scope        string `json:"scope"`
	OwnerID      string `json:"owner_id"`
	Note         string `json:"note"`
}

type DraftResult struct {
	Rows     []DraftRow `json:"rows"`
	Source   string     `json:"source"` // template | ai
	Headers  []string   `json:"headers,omitempty"`
	Warnings []string   `json:"warnings,omitempty"`
}

// SetLLM 注入模型配置，供「任意表格 → 记账字段」的映射使用
func (s *LedgerService) SetLLM(cfg *config.Config) { s.llmCfg = cfg }

// PreviewImport 解析「AI 生成的 CSV」但不入库，返回给 App 预览。
// 先用通用记账模板解析；认不出表头时退回让模型做列映射。
func (s *LedgerService) PreviewImport(ctx context.Context, familyID, userID, fileName string, data []byte) (*DraftResult, error) {
	if _, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID); err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, domain.ErrBadRequest.WithMessage("文件内容为空")
	}
	if len(data) > 2<<20 {
		return nil, domain.ErrBadRequest.WithMessage("文件超过 2MB")
	}

	categories, err := s.db.CategoriesOfFamily(ctx, s.db.SQL(), familyID)
	if err != nil {
		return nil, err
	}

	records, headers, parseErr := importer.ParseTemplate(data)
	if parseErr != nil {
		names := make([]string, 0, len(categories))
		for _, c := range categories {
			if c.Type == domain.TypeExpense {
				names = append(names, c.Name)
			}
		}
		return nil, domain.ErrBadRequest.WithMessage(fmt.Sprintf(
			"表格格式不对：%v。\n\nApp 只接受下面这一种表头（第一行）：\n%s\n\n分类只能用：%s",
			parseErr, importer.TemplateHeader, strings.Join(names, " ")))
	}
	return &DraftResult{
		Source:  "template",
		Headers: headers,
		Rows:    toDraftRows(records, categories),
	}, nil
}

func toDraftRows(records []importer.TemplateRecord, categories []store.Category) []DraftRow {
	byName := map[string]store.Category{}
	for _, c := range categories {
		byName[c.Name] = c
	}
	out := make([]DraftRow, 0, len(records))
	for _, r := range records {
		cat, ok := byName[r.CategoryName]
		if !ok {
			cat = byName["其他"]
		}
		scope := r.Scope
		if scope == "" {
			scope = domain.ScopeUnknown
		}
		out = append(out, DraftRow{
			OccurredAt:   r.OccurredAt.UnixMilli(),
			Type:         r.Direction,
			AmountCents:  r.AmountCents,
			CategoryID:   cat.ID,
			CategoryName: cat.Name,
			Scope:        scope,
			Note:         r.Note,
		})
	}
	return out
}

type llmMappedRow struct {
	Date     string  `json:"date"`
	Type     string  `json:"type"`
	Amount   float64 `json:"amount"`
	Category string  `json:"category"`
	Scope    string  `json:"scope"`
	Note     string  `json:"note"`
}

type llmMapped struct {
	Rows []llmMappedRow `json:"rows"`
}

const mappingPrompt = `你是记账数据整理器。用户会给你一个从截图识别出来的表格（CSV 文本），
你要把它转换成记账记录。

规则：
- 同一个订单的多行商品，合并成一条，金额用总额
- 日期统一 YYYY-MM-DD；没有日期就用今天
- 金额是实付金额，两位小数
- 分类只能从给定列表选一个
- scope 只能是 family（家庭共同）/ personal（个人）/ transfer（资金往来）/ unknown
- 备注写商户+商品，20 字以内

只输出 JSON：{"rows":[{"date":"2026-11-02","type":"支出","amount":28.5,"category":"餐饮","scope":"family","note":"..."}]}`

func (s *LedgerService) mapWithLLM(ctx context.Context, data []byte, categories []store.Category) ([]DraftRow, []string, error) {
	if s.llmCfg == nil {
		return nil, nil, fmt.Errorf("服务端未配置模型")
	}
	client := llm.New(llm.Config{
		Provider: s.llmCfg.LLMProvider,
		APIKey:   s.llmCfg.LLMAPIKey,
		BaseURL:  s.llmCfg.LLMBaseURL,
		Model:    s.llmCfg.LLMModel,
	})
	if !client.Enabled() {
		return nil, nil, fmt.Errorf("服务端未配置模型")
	}

	text, err := importer.DecodeText(data, 6000)
	if err != nil {
		return nil, nil, err
	}
	names := make([]string, 0, len(categories))
	for _, c := range categories {
		if c.Type == domain.TypeExpense {
			names = append(names, c.Name)
		}
	}
	user := fmt.Sprintf("分类列表：%s\n\n表格内容：\n%s", strings.Join(names, " "), text)

	var out llmMapped
	if _, err := client.ChatJSON(ctx, mappingPrompt, user, &out); err != nil {
		return nil, nil, err
	}

	byName := map[string]store.Category{}
	for _, c := range categories {
		byName[c.Name] = c
	}
	rows := make([]DraftRow, 0, len(out.Rows))
	warnings := make([]string, 0, 2)
	for _, r := range out.Rows {
		t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(r.Date), time.Local)
		if err != nil {
			t = time.Now()
			warnings = append(warnings, fmt.Sprintf("日期 %q 无法识别，已用今天", r.Date))
		}
		cents := int64(r.Amount*100 + 0.5)
		if cents == 0 {
			continue
		}
		cat, ok := byName[r.Category]
		if !ok {
			cat = byName["其他"]
		}
		dir := domain.TypeExpense
		if strings.Contains(r.Type, "收入") {
			dir = domain.TypeIncome
		}
		scope := r.Scope
		switch scope {
		case domain.ScopeFamily, domain.ScopePersonal, domain.ScopeTransfer:
		default:
			scope = domain.ScopeUnknown
		}
		rows = append(rows, DraftRow{
			OccurredAt:   t.UnixMilli(),
			Type:         dir,
			AmountCents:  cents,
			CategoryID:   cat.ID,
			CategoryName: cat.Name,
			Scope:        scope,
			Note:         r.Note,
		})
	}
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("模型没有识别出任何记录")
	}
	return rows, warnings, nil
}

// ConfirmImport 把预览过的记录真正写库。
func (s *LedgerService) ConfirmImport(ctx context.Context, familyID, userID string, rows []DraftRow) (*ImportSummary, error) {
	member, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID)
	if err != nil {
		return nil, err
	}
	if member.Role == domain.RoleMember {
		return nil, domain.ErrForbidden.WithMessage("只有户主或管理员可以导入账单")
	}
	if len(rows) == 0 {
		return nil, domain.ErrBadRequest.WithMessage("没有要导入的记录")
	}
	if len(rows) > 500 {
		return nil, domain.ErrBadRequest.WithMessage("一次最多导入 500 条")
	}

	categories, err := s.db.CategoriesOfFamily(ctx, s.db.SQL(), familyID)
	if err != nil {
		return nil, err
	}
	valid := map[string]store.Category{}
	for _, c := range categories {
		valid[c.ID] = c
	}

	now := store.NowMS()
	summary := &ImportSummary{
		BatchID:    id.New(),
		Source:     "ai_csv",
		SourceName: "AI 账单",
		Total:      len(rows),
	}
	err = s.db.WithTx(ctx, func(dbtx *sql.Tx) error {
		for _, r := range rows {
			cat, ok := valid[r.CategoryID]
			if !ok {
				summary.Failed++
				continue
			}
			if r.AmountCents <= 0 {
				summary.Skipped++
				continue
			}
			rev, err := s.db.BumpRev(ctx, dbtx, familyID, now)
			if err != nil {
				return err
			}
			note := strings.TrimSpace(r.Note)
			if note == "" {
				note = cat.Name
			}
			item := &domain.Transaction{
				ID:             id.New(),
				FamilyID:       familyID,
				Type:           r.Type,
				AmountCents:    r.AmountCents,
				CategoryID:     cat.ID,
				MemberID:       userID,
				OccurredAt:     r.OccurredAt,
				Note:           note,
				Visibility:     domain.VisibilityFamily,
				Rev:            rev,
				CreatedAt:      now,
				UpdatedAt:      now,
				Source:         "ai_csv",
				ExternalID:     "aicsv-" + fingerprint(r),
				Scope:          r.Scope,
				OwnerID:        r.OwnerID,
				CategorySource: domain.CategorySourceAI,
				AIConfidence:   1,
				AIReason:       "AI 识别导入",
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
		}
		return s.db.InsertImportBatch(ctx, dbtx, &store.ImportBatch{
			ID: summary.BatchID, FamilyID: familyID, Source: summary.Source,
			FileName: "分享导入.csv", Total: summary.Total, Imported: summary.Imported,
			Skipped: summary.Skipped, Failed: summary.Failed, Status: "done",
			CreatedBy: userID, CreatedAt: now,
		})
	})
	if err != nil {
		return nil, err
	}
	return summary, nil
}

// fingerprint 给 AI 导入的数据造稳定去重键（这类数据没有交易单号）
func fingerprint(r DraftRow) string {
	raw := fmt.Sprintf("%d|%d|%s|%s", r.OccurredAt, r.AmountCents, r.CategoryID, r.Note)
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])[:16]
}
