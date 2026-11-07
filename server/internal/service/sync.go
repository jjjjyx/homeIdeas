package service

import (
	"context"
	"database/sql"
	"errors"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/store"
)

// SyncPullResult 一次增量拉取的结果。成员表很小，每次返回全量，客户端整体替换。
type SyncPullResult struct {
	Cursor       int64
	HasMore      bool
	Categories   []store.Category
	Accounts     []domain.Account
	Transactions []domain.Transaction
	Members      []domain.Member
}

func (s *LedgerService) Pull(ctx context.Context, familyID, userID string, since int64, limit int) (*SyncPullResult, error) {
	if since < 0 {
		since = 0
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	if _, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID); err != nil {
		return nil, err
	}

	categories, err := s.db.CategoriesChangedSince(ctx, s.db.SQL(), familyID, since, limit)
	if err != nil {
		return nil, err
	}
	accounts, err := s.db.AccountsChangedSince(ctx, s.db.SQL(), familyID, since, limit)
	if err != nil {
		return nil, err
	}
	transactions, err := s.db.TransactionsChangedSince(ctx, s.db.SQL(), familyID, userID, since, limit)
	if err != nil {
		return nil, err
	}
	members, err := s.db.MembersOf(ctx, s.db.SQL(), familyID)
	if err != nil {
		return nil, err
	}

	result := &SyncPullResult{
		Cursor:       since,
		Categories:   categories,
		Accounts:     accounts,
		Transactions: transactions,
		Members:      members,
	}
	if len(categories) >= limit || len(accounts) >= limit || len(transactions) >= limit {
		result.HasMore = true
	}
	for _, c := range categories {
		if c.Rev > result.Cursor {
			result.Cursor = c.Rev
		}
	}
	for _, a := range accounts {
		if a.Rev > result.Cursor {
			result.Cursor = a.Rev
		}
	}
	for _, t := range transactions {
		if t.Rev > result.Cursor {
			result.Cursor = t.Rev
		}
	}
	return result, nil
}

// SyncOpInput 客户端 outbox 里的一条待同步操作。
type SyncOpInput struct {
	ClientOpID string
	Entity     string
	Op         string
	BaseRev    int64
	Payload    TxInput
}

type SyncOpResult struct {
	ClientOpID string
	Status     string // applied / conflict / rejected
	Rev        int64
	Reason     string
	Server     *domain.Transaction
}

const (
	syncStatusApplied  = "applied"
	syncStatusConflict = "conflict"
	syncStatusRejected = "rejected"
)

// Push 逐条应用客户端的离线操作。单条失败不影响其它条，结果逐条返回。
func (s *LedgerService) Push(ctx context.Context, familyID, userID string, ops []SyncOpInput) ([]SyncOpResult, error) {
	if len(ops) == 0 {
		return []SyncOpResult{}, nil
	}
	if len(ops) > 200 {
		return nil, domain.ErrBadRequest.WithMessage("单次最多推送 200 条操作")
	}
	if _, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID); err != nil {
		return nil, err
	}

	results := make([]SyncOpResult, 0, len(ops))
	for _, op := range ops {
		switch {
		case op.Entity != "transaction":
			results = append(results, SyncOpResult{
				ClientOpID: op.ClientOpID, Status: syncStatusRejected,
				Reason: "不支持的实体: " + op.Entity,
			})
		case op.Op == "upsert":
			results = append(results, s.syncUpsert(ctx, familyID, userID, op))
		case op.Op == "delete":
			results = append(results, s.syncDelete(ctx, familyID, userID, op))
		default:
			results = append(results, SyncOpResult{
				ClientOpID: op.ClientOpID, Status: syncStatusRejected,
				Reason: "不支持的操作: " + op.Op,
			})
		}
	}
	return results, nil
}

func (s *LedgerService) syncUpsert(ctx context.Context, familyID, userID string, op SyncOpInput) SyncOpResult {
	res := SyncOpResult{ClientOpID: op.ClientOpID}
	in := op.Payload
	if err := s.checkInput(ctx, familyID, &in); err != nil {
		res.Status = syncStatusRejected
		res.Reason = err.Error()
		if appErr, ok := err.(*domain.AppError); ok {
			res.Reason = appErr.Message
		}
		return res
	}

	member, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID)
	if err != nil {
		res.Status = syncStatusRejected
		res.Reason = "无权访问该家庭"
		return res
	}

	existing, err := s.db.TransactionByID(ctx, s.db.SQL(), in.ID, familyID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		res.Status = syncStatusRejected
		res.Reason = "读取失败"
		return res
	}

	now := store.NowMS()

	// 已存在：版本冲突检测 → 权限校验 → 覆盖
	if existing != nil {
		if op.BaseRev > 0 && existing.Rev != op.BaseRev {
			res.Status = syncStatusConflict
			res.Server = existing
			return res
		}
		if member.Role == domain.RoleMember && existing.MemberID != userID {
			res.Status = syncStatusRejected
			res.Reason = "只能修改自己记的账"
			return res
		}
		if existing.Visibility == domain.VisibilityPrivate && existing.MemberID != userID {
			res.Status = syncStatusRejected
			res.Reason = "没有权限修改这笔私密账"
			return res
		}

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
			res.Status = syncStatusRejected
			res.Reason = "写入失败"
			return res
		}
		res.Status = syncStatusApplied
		res.Rev = updated.Rev
		res.Server = &updated
		return res
	}

	// 不存在：新建
	created := &domain.Transaction{
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
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	err = s.db.WithTx(ctx, func(dbtx *sql.Tx) error {
		rev, err := s.db.BumpRev(ctx, dbtx, familyID, now)
		if err != nil {
			return err
		}
		created.Rev = rev
		return s.db.InsertTransaction(ctx, dbtx, created)
	})
	if err != nil {
		res.Status = syncStatusRejected
		res.Reason = "写入失败"
		return res
	}
	res.Status = syncStatusApplied
	res.Rev = created.Rev
	res.Server = created
	return res
}

func (s *LedgerService) syncDelete(ctx context.Context, familyID, userID string, op SyncOpInput) SyncOpResult {
	res := SyncOpResult{ClientOpID: op.ClientOpID}
	existing, err := s.db.TransactionByID(ctx, s.db.SQL(), op.Payload.ID, familyID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			res.Status = syncStatusApplied // 幂等：已经不在了
			return res
		}
		res.Status = syncStatusRejected
		res.Reason = "读取失败"
		return res
	}
	if existing.DeletedAt != nil {
		res.Status = syncStatusApplied
		res.Rev = existing.Rev
		return res
	}
	member, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID)
	if err != nil {
		res.Status = syncStatusRejected
		res.Reason = "无权访问该家庭"
		return res
	}
	if member.Role == domain.RoleMember && existing.MemberID != userID {
		res.Status = syncStatusRejected
		res.Reason = "只能删除自己记的账"
		return res
	}
	if existing.Visibility == domain.VisibilityPrivate && existing.MemberID != userID {
		res.Status = syncStatusRejected
		res.Reason = "没有权限删除这笔私密账"
		return res
	}

	now := store.NowMS()
	var rev int64
	err = s.db.WithTx(ctx, func(dbtx *sql.Tx) error {
		r, err := s.db.BumpRev(ctx, dbtx, familyID, now)
		if err != nil {
			return err
		}
		rev = r
		return s.db.SoftDeleteTransaction(ctx, dbtx, existing.ID, familyID, rev, now)
	})
	if err != nil {
		res.Status = syncStatusRejected
		res.Reason = "写入失败"
		return res
	}
	res.Status = syncStatusApplied
	res.Rev = rev
	return res
}
