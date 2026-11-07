// Package classifier 用大模型把账目分成「家庭/个人」并给出细分类。
package classifier

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/llm"
	"family-ledger/server/internal/store"
)

type Service struct {
	db     *store.DB
	client *llm.Client
	batch  int
	now    func() int64
}

func New(db *store.DB, client *llm.Client, batch int) *Service {
	if batch <= 0 || batch > 60 {
		batch = 30
	}
	return &Service{db: db, client: client, batch: batch, now: store.NowMS}
}

type Result struct {
	Total            int      `json:"total"`
	Classified       int      `json:"classified"`
	Unknown          int      `json:"unknown"`
	Batches          int      `json:"batches"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	Errors           []string `json:"errors,omitempty"`
}

// RunFamily 对某个家庭里「还没分类」的支出账目跑一遍 AI 分类。
func (s *Service) RunFamily(ctx context.Context, familyID string, limit int) (*Result, error) {
	if !s.client.Enabled() {
		return nil, fmt.Errorf("未配置 LLM_API_KEY，无法调用模型")
	}

	pending, err := s.db.UnclassifiedTransactions(ctx, s.db.SQL(), familyID, limit)
	if err != nil {
		return nil, err
	}
	result := &Result{Total: len(pending)}
	if len(pending) == 0 {
		return result, nil
	}

	// 分类名 → 分类 ID，模型只返回名字
	categories, err := s.db.CategoriesOfFamily(ctx, s.db.SQL(), familyID)
	if err != nil {
		return nil, err
	}
	categoryID := map[string]string{}
	categoryNames := make([]string, 0, len(categories))
	for _, c := range categories {
		if c.Type == domain.TypeExpense && len(categoryNames) < 20 {
			categoryID[c.Name] = c.ID
			categoryNames = append(categoryNames, c.Name)
		}
	}
	systemPrompt := systemPromptFor(categoryNames)

	for start := 0; start < len(pending); start += s.batch {
		end := start + s.batch
		if end > len(pending) {
			end = len(pending)
		}
		batch := pending[start:end]

		decisions, usage, err := s.classifyBatch(ctx, batch, systemPrompt)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		result.Batches++
		if usage != nil {
			result.PromptTokens += usage.PromptTokens
			result.CompletionTokens += usage.CompletionTokens
		}

		if err := s.apply(ctx, familyID, batch, decisions, categoryID, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Service) classifyBatch(ctx context.Context, rows []store.PendingRow, systemPrompt string) (map[string]Decision, *llm.Usage, error) {
	items := make([]Item, 0, len(rows))
	for _, r := range rows {
		merchant, detail := splitNote(r.Note)
		t := time.UnixMilli(r.OccurredAt)
		items = append(items, Item{
			ID:       r.ID,
			Merchant: merchant,
			Item:     detail,
			Amount:   r.AmountCents / 100,
			Hour:     t.Hour(),
			Weekday:  weekdayCN(t.Weekday()),
		})
	}
	payload, err := buildUserPayload(items)
	if err != nil {
		return nil, nil, err
	}

	var resp response
	usage, err := s.client.ChatJSON(ctx, systemPrompt, payload, &resp)
	if err != nil {
		return nil, nil, err
	}

	out := make(map[string]Decision, len(resp.Items))
	for _, d := range resp.Items {
		if d.ID == "" {
			continue
		}
		out[d.ID] = validate(d)
	}
	return out, usage, nil
}

func (s *Service) apply(ctx context.Context, familyID string, rows []store.PendingRow,
	decisions map[string]Decision, categoryID map[string]string, result *Result) error {

	now := s.now()
	return s.db.WithTx(ctx, func(dbtx *sql.Tx) error {
		for _, r := range rows {
			d, ok := decisions[r.ID]
			if !ok {
				continue // 模型漏了这条，留着下次再跑
			}
			if d.Scope == domain.ScopeUnknown {
				result.Unknown++
			}

			ownerID := ""
			if d.Scope == domain.ScopePersonal {
				// 归到记账人名下；账单导入时记账人就是导入者
				ownerID = r.MemberID
			}

			catID := categoryID[d.Category]
			if catID == "" {
				catID = categoryID["其他"]
			}
			if catID == "" {
				result.Unknown++
				continue
			}

			rev, err := s.db.BumpRev(ctx, dbtx, familyID, now)
			if err != nil {
				return err
			}
			if err := s.db.ApplyClassification(ctx, dbtx, familyID, r.ID,
				d.Scope, ownerID, catID, d.Confidence, d.Reason,
				domain.CategorySourceAI, rev, now); err != nil {
				return err
			}
			result.Classified++
		}
		return nil
	})
}

// splitNote 把 "商户 · 商品" 拆开
func splitNote(note string) (merchant, detail string) {
	parts := strings.SplitN(note, " · ", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(note), ""
}

func weekdayCN(d time.Weekday) string {
	return [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[d]
}
