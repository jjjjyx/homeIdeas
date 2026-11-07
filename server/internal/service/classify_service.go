package service

import (
	"context"
	"database/sql"
	"strings"

	"family-ledger/server/internal/classifier"
	"family-ledger/server/internal/config"
	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/llm"
	"family-ledger/server/internal/pkg/id"
	"family-ledger/server/internal/store"
)

// ClassifyService 负责 AI 分类的调度与人工修正的落库。
type ClassifyService struct {
	db         *store.DB
	cfg        *config.Config
	classifier *classifier.Service
	enabled    bool
}

func NewClassifyService(db *store.DB, cfg *config.Config) *ClassifyService {
	client := llm.New(llm.Config{
		Provider: cfg.LLMProvider,
		APIKey:   cfg.LLMAPIKey,
		BaseURL:  cfg.LLMBaseURL,
		Model:    cfg.LLMModel,
	})
	return &ClassifyService{
		db:         db,
		cfg:        cfg,
		classifier: classifier.New(db, client, cfg.LLMBatchSize),
		enabled:    cfg.LLMEnabled && client.Enabled(),
	}
}

func (s *ClassifyService) Enabled() bool { return s.enabled }

// Run 对家庭里待分类的账单跑一遍 AI
func (s *ClassifyService) Run(ctx context.Context, familyID, userID string) (*classifier.Result, error) {
	if err := s.requireAdmin(ctx, familyID, userID); err != nil {
		return nil, err
	}
	if !s.enabled {
		return nil, domain.ErrBadRequest.WithMessage("服务端未配置模型 API Key，暂不能用 AI 分类")
	}
	return s.classifier.RunFamily(ctx, familyID, 1000)
}

// Pending 待确认的账目按商户聚合
func (s *ClassifyService) Pending(ctx context.Context, familyID, userID string, minConfidence float64) (map[string]any, error) {
	if _, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID); err != nil {
		return nil, err
	}
	merchants, err := s.db.MerchantsOfUnclassified(ctx, s.db.SQL(), familyID, userID, 100)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.PendingReview(ctx, s.db.SQL(), familyID, userID, minConfidence, 200)
	if err != nil {
		return nil, err
	}
	categories, err := s.db.CategoriesOfFamily(ctx, s.db.SQL(), familyID)
	if err != nil {
		return nil, err
	}
	byID := map[string]store.Category{}
	for _, c := range categories {
		byID[c.ID] = c
	}

	items := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		name := "其他"
		if c, ok := byID[r.CategoryID]; ok {
			name = c.Name
		}
		items = append(items, map[string]any{
			"id": r.ID, "note": r.Note, "amount_cents": r.AmountCents,
			"occurred_at": r.OccurredAt, "scope": r.Scope, "owner_id": r.OwnerID,
			"category_id": r.CategoryID, "category_name": name,
			"confidence": r.Confidence, "reason": r.Reason,
		})
	}
	groups := make([]map[string]any, 0, len(merchants))
	for _, m := range merchants {
		groups = append(groups, map[string]any{
			"merchant": m.Merchant, "count": m.Count, "amount_cents": m.AmountCents,
			"sample": m.SampleNote,
		})
	}
	return map[string]any{
		"pending_count": len(rows),
		"items":         items,
		"merchants":     groups,
	}, nil
}

type ReviewInput struct {
	Scope      string // family | personal
	OwnerID    string // scope=personal 时必填
	CategoryID string
	// Remember 是否把「商户 → 分类」记成规则，下次导入自动套用
	Remember bool
}

// Review 人工修正一条账目，并（可选）把它固化成规则。
func (s *ClassifyService) Review(ctx context.Context, familyID, userID, txID string, in ReviewInput) error {
	member, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID)
	if err != nil {
		return err
	}

	tx, err := s.db.TransactionByID(ctx, s.db.SQL(), txID, familyID)
	if err != nil {
		return err
	}
	if tx.Visibility == domain.VisibilityPrivate && tx.MemberID != member.UserID {
		return domain.ErrNotFound
	}

	scope := strings.TrimSpace(in.Scope)
	if scope != domain.ScopeFamily && scope != domain.ScopePersonal {
		return domain.ErrBadRequest.WithMessage("scope 只能是 family 或 personal")
	}
	ownerID := strings.TrimSpace(in.OwnerID)
	if scope == domain.ScopePersonal && ownerID == "" {
		ownerID = tx.MemberID
	}
	if scope == domain.ScopeFamily {
		ownerID = ""
	}

	categoryID := in.CategoryID
	if categoryID != "" {
		ok, err := s.db.CategoryBelongsToFamily(ctx, s.db.SQL(), categoryID, familyID)
		if err != nil {
			return err
		}
		if !ok {
			return domain.ErrBadRequest.WithMessage("分类不属于该家庭")
		}
	} else {
		categoryID = tx.CategoryID
	}

	now := store.NowMS()
	err = s.db.WithTx(ctx, func(dbtx *sql.Tx) error {
		rev, err := s.db.BumpRev(ctx, dbtx, familyID, now)
		if err != nil {
			return err
		}
		if err := s.db.ApplyClassification(ctx, dbtx, familyID, txID, scope, ownerID,
			categoryID, 1, "人工确认", domain.CategorySourceManual, rev, now); err != nil {
			return err
		}
		_, err = dbtx.ExecContext(ctx,
			`UPDATE transactions SET reviewed = 1 WHERE id = ? AND family_id = ?`, txID, familyID)
		return err
	})
	if err != nil {
		return err
	}

	if in.Remember {
		merchant := merchantOf(tx.Note)
		if merchant != "" && categoryID != "" && member.Role != domain.RoleMember {
			_ = s.db.UpsertRule(ctx, s.db.SQL(), id.New(), familyID, merchant, categoryID, now)
		}
	}
	return nil
}

// merchantOf 从 "商户 · 商品" 里取商户名
func merchantOf(note string) string {
	if i := strings.Index(note, " · "); i > 0 {
		return strings.TrimSpace(note[:i])
	}
	return strings.TrimSpace(note)
}

func (s *ClassifyService) requireAdmin(ctx context.Context, familyID, userID string) error {
	member, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID)
	if err != nil {
		return err
	}
	if member.Role == domain.RoleMember {
		return domain.ErrForbidden.WithMessage("只有户主或管理员可以触发 AI 分类")
	}
	return nil
}
