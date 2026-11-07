package service

import (
	"context"
	"database/sql"
	"errors"
	"family-ledger/server/internal/config"
	"strings"
	"time"
	"unicode/utf8"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/pkg/id"
	"family-ledger/server/internal/store"
)

const (
	maxAmountCents = 1_000_000_000 // 1000 万元，防手滑输入
	maxNoteRunes   = 200
)

type LedgerService struct {
	db *store.DB
	// llmCfg 用于「任意表格 → 记账字段」的映射，可能为 nil
	llmCfg *config.Config
}

func NewLedgerService(db *store.DB) *LedgerService {
	return &LedgerService{db: db}
}

// TxInput 新建/修改一笔账的入参。ID 允许客户端预生成（离线建账）。
type TxInput struct {
	ID          string
	Type        int
	AmountCents int64
	CategoryID  string
	AccountID   string
	OccurredAt  int64
	Note        string
	Visibility  string
	Tags        string
}

func (in *TxInput) normalize() {
	in.ID = strings.TrimSpace(in.ID)
	in.CategoryID = strings.TrimSpace(in.CategoryID)
	in.AccountID = strings.TrimSpace(in.AccountID)
	in.Note = strings.TrimSpace(in.Note)
	in.Tags = strings.TrimSpace(in.Tags)
	if in.Visibility == "" {
		in.Visibility = domain.VisibilityFamily
	}
	if in.ID == "" {
		in.ID = id.New()
	}
}

func (in *TxInput) validate() error {
	if in.Type != domain.TypeExpense && in.Type != domain.TypeIncome {
		return domain.ErrBadRequest.WithMessage("type 只能是 0(支出) 或 1(收入)")
	}
	if in.AmountCents <= 0 || in.AmountCents > maxAmountCents {
		return domain.ErrBadRequest.WithMessage("金额需大于 0 且不超过 1000 万元")
	}
	if in.CategoryID == "" {
		return domain.ErrBadRequest.WithMessage("缺少分类")
	}
	if in.Visibility != domain.VisibilityFamily && in.Visibility != domain.VisibilityPrivate {
		return domain.ErrBadRequest.WithMessage("visibility 只能是 family 或 private")
	}
	if utf8.RuneCountInString(in.Note) > maxNoteRunes {
		return domain.ErrBadRequest.WithMessage("备注最多 200 字")
	}
	now := time.Now()
	minTS := time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local).UnixMilli()
	maxTS := now.AddDate(1, 0, 0).UnixMilli()
	if in.OccurredAt <= 0 {
		in.OccurredAt = now.UnixMilli()
	}
	if in.OccurredAt < minTS || in.OccurredAt > maxTS {
		return domain.ErrBadRequest.WithMessage("日期超出合理范围")
	}
	return nil
}

// Create 新建一笔账：校验成员身份、分类归属，分配 rev。
func (s *LedgerService) Create(ctx context.Context, familyID, userID string, in TxInput) (*domain.Transaction, error) {
	if _, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID); err != nil {
		return nil, err
	}
	in.normalize()
	if err := s.checkInput(ctx, familyID, &in); err != nil {
		return nil, err
	}

	now := store.NowMS()
	tx := &domain.Transaction{
		ID:          in.ID,
		FamilyID:    familyID,
		Type:        in.Type,
		AmountCents: in.AmountCents,
		CategoryID:  in.CategoryID,
		AccountID:   in.AccountID,
		MemberID:    userID,
		OccurredAt:  in.OccurredAt,
		Note:        in.Note,
		Visibility:  in.Visibility,
		Tags:        in.Tags,
	}
	if _, err := s.db.TransactionByID(ctx, s.db.SQL(), tx.ID, familyID); err == nil {
		return nil, domain.ErrConflict.WithMessage("该账单已存在")
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	err := s.db.WithTx(ctx, func(dbtx *sql.Tx) error {
		rev, err := s.db.BumpRev(ctx, dbtx, familyID, now)
		if err != nil {
			return err
		}
		tx.Rev = rev
		tx.CreatedAt = now
		tx.UpdatedAt = now
		return s.db.InsertTransaction(ctx, dbtx, tx)
	})
	if err != nil {
		return nil, err
	}
	return tx, nil
}

// Update 修改一笔账。普通成员只能改自己记的；私密账只有本人能改。
func (s *LedgerService) Update(ctx context.Context, familyID, userID, txID string, in TxInput) (*domain.Transaction, error) {
	member, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID)
	if err != nil {
		return nil, err
	}
	existing, err := s.db.TransactionByID(ctx, s.db.SQL(), txID, familyID)
	if err != nil {
		return nil, err
	}
	if existing.DeletedAt != nil {
		return nil, domain.ErrNotFound.WithMessage("账单已删除")
	}
	if member.Role == domain.RoleMember && existing.MemberID != userID {
		return nil, domain.ErrForbidden.WithMessage("只能修改自己记的账")
	}
	if existing.Visibility == domain.VisibilityPrivate && existing.MemberID != userID {
		return nil, domain.ErrForbidden.WithMessage("没有权限修改这笔私密账")
	}

	in.ID = txID
	in.normalize()
	if err := s.checkInput(ctx, familyID, &in); err != nil {
		return nil, err
	}

	now := store.NowMS()
	updated := *existing
	updated.Type = in.Type
	updated.AmountCents = in.AmountCents
	updated.CategoryID = in.CategoryID
	updated.AccountID = in.AccountID
	updated.OccurredAt = in.OccurredAt
	updated.Note = in.Note
	updated.Visibility = in.Visibility
	updated.Tags = in.Tags

	err = s.db.WithTx(ctx, func(dbtx *sql.Tx) error {
		rev, err := s.db.BumpRev(ctx, dbtx, familyID, now)
		if err != nil {
			return err
		}
		updated.Rev = rev
		updated.UpdatedAt = now
		return s.db.UpdateTransaction(ctx, dbtx, &updated)
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// Delete 软删除：写墓碑，客户端同步时能感知。
func (s *LedgerService) Delete(ctx context.Context, familyID, userID, txID string) error {
	member, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID)
	if err != nil {
		return err
	}
	existing, err := s.db.TransactionByID(ctx, s.db.SQL(), txID, familyID)
	if err != nil {
		return err
	}
	if existing.DeletedAt != nil {
		return nil // 幂等
	}
	if member.Role == domain.RoleMember && existing.MemberID != userID {
		return domain.ErrForbidden.WithMessage("只能删除自己记的账")
	}
	if existing.Visibility == domain.VisibilityPrivate && existing.MemberID != userID {
		return domain.ErrForbidden.WithMessage("没有权限删除这笔私密账")
	}

	now := store.NowMS()
	return s.db.WithTx(ctx, func(dbtx *sql.Tx) error {
		rev, err := s.db.BumpRev(ctx, dbtx, familyID, now)
		if err != nil {
			return err
		}
		if err := s.db.SoftDeleteTransaction(ctx, dbtx, txID, familyID, rev, now); err != nil {
			return err
		}
		return s.db.AddAudit(ctx, dbtx, &domain.AuditLog{
			FamilyID: familyID, UserID: userID, Action: "transaction.delete",
			TargetType: "transaction", TargetID: txID,
		}, now)
	})
}

// checkInput 归一化 + 校验 + 分类归属检查，Create/Update/同步推送共用。
func (s *LedgerService) checkInput(ctx context.Context, familyID string, in *TxInput) error {
	in.normalize()
	if err := in.validate(); err != nil {
		return err
	}
	ok, err := s.db.CategoryBelongsToFamily(ctx, s.db.SQL(), in.CategoryID, familyID)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrBadRequest.WithMessage("分类不属于该家庭")
	}
	return nil
}

type ListOptions struct {
	FromMS   int64
	ToMS     int64
	Type     *int
	Category string
	MemberID string
	Limit    int
	Offset   int
}

func (s *LedgerService) List(ctx context.Context, familyID, userID string, opt ListOptions) ([]domain.Transaction, error) {
	if _, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID); err != nil {
		return nil, err
	}
	if opt.Limit <= 0 || opt.Limit > 500 {
		opt.Limit = 100
	}
	return s.db.ListTransactions(ctx, s.db.SQL(), store.TxFilter{
		FamilyID: familyID,
		FromMS:   opt.FromMS,
		ToMS:     opt.ToMS,
		Type:     opt.Type,
		Category: opt.Category,
		MemberID: opt.MemberID,
		ViewerID: userID,
		Limit:    opt.Limit,
		Offset:   opt.Offset,
	})
}
