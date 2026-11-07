package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/pkg/id"
	"family-ledger/server/internal/store"
)

// SplitLine 拆单后的一行
type SplitLine struct {
	AmountCents int64
	Scope       string // family | personal
	OwnerID     string // scope=personal 时有效，空则用原子账目的记账人
	CategoryID  string
	Note        string
}

type SplitInput struct {
	Lines []SplitLine
}

// SplitTransaction 把一笔账目拆成多行（超市小票里同时有家庭用品和个人用品）。
//
// 做法：原账目标记 is_split=1 只做留档、不进统计；
// 每一行生成一条普通子账目（parent_id 指向原账目），统计/列表/导出自动生效。
func (s *LedgerService) SplitTransaction(ctx context.Context, familyID, userID, txID string, in SplitInput) (*SplitResult, error) {
	member, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID)
	if err != nil {
		return nil, err
	}
	parent, err := s.db.TransactionByID(ctx, s.db.SQL(), txID, familyID)
	if err != nil {
		return nil, err
	}
	if parent.Visibility == domain.VisibilityPrivate && parent.MemberID != member.UserID {
		return nil, domain.ErrNotFound
	}
	if parent.IsSplit {
		return nil, domain.ErrBadRequest.WithMessage("这笔账目已经拆分过了")
	}
	if parent.Type != domain.TypeExpense {
		return nil, domain.ErrBadRequest.WithMessage("只有支出可以拆分")
	}
	if len(in.Lines) < 2 {
		return nil, domain.ErrBadRequest.WithMessage("拆单至少要两行")
	}

	var sum int64
	lines := make([]SplitLine, 0, len(in.Lines))
	for i, line := range in.Lines {
		if line.AmountCents <= 0 {
			return nil, domain.ErrBadRequest.WithMessage(fmt.Sprintf("第 %d 行金额必须大于 0", i+1))
		}
		if line.Scope != domain.ScopeFamily && line.Scope != domain.ScopePersonal {
			return nil, domain.ErrBadRequest.WithMessage(fmt.Sprintf("第 %d 行归属只能是家庭或个人", i+1))
		}
		if line.CategoryID == "" {
			return nil, domain.ErrBadRequest.WithMessage(fmt.Sprintf("第 %d 行缺少分类", i+1))
		}
		ok, err := s.db.CategoryBelongsToFamily(ctx, s.db.SQL(), line.CategoryID, familyID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, domain.ErrBadRequest.WithMessage(fmt.Sprintf("第 %d 行的分类不属于该家庭", i+1))
		}
		if line.Scope == domain.ScopeFamily {
			line.OwnerID = ""
		} else if line.OwnerID == "" {
			line.OwnerID = parent.MemberID
		}
		sum += line.AmountCents
		lines = append(lines, line)
	}
	if sum != parent.AmountCents {
		return nil, domain.ErrBadRequest.WithMessage(
			fmt.Sprintf("各行合计 %d 分，与原账目 %d 分不一致", sum, parent.AmountCents))
	}

	now := store.NowMS()
	childIDs := make([]string, 0, len(lines))
	err = s.db.WithTx(ctx, func(dbtx *sql.Tx) error {
		// 父账目打墓碑软删除：统计、列表、导出、以及所有客户端的本地缓存
		// 都会自动隐藏它，不需要给 App 加字段
		if err := s.db.MarkSplit(ctx, dbtx, parent.ID, familyID, now); err != nil {
			return err
		}

		for _, line := range lines {
			rev, err := s.db.BumpRev(ctx, dbtx, familyID, now)
			if err != nil {
				return err
			}
			child := &domain.Transaction{
				ID:             id.New(),
				FamilyID:       familyID,
				Type:           domain.TypeExpense,
				AmountCents:    line.AmountCents,
				CategoryID:     line.CategoryID,
				AccountID:      parent.AccountID,
				MemberID:       parent.MemberID,
				OccurredAt:     parent.OccurredAt,
				Note:           splitNote(parent.Note, line.Note),
				Visibility:     parent.Visibility,
				Rev:            rev,
				CreatedAt:      now,
				UpdatedAt:      now,
				Source:         parent.Source,
				Scope:          line.Scope,
				OwnerID:        line.OwnerID,
				CategorySource: domain.CategorySourceManual,
				Reviewed:       true,
				ParentID:       parent.ID,
			}
			if err := s.db.InsertTransaction(ctx, dbtx, child); err != nil {
				return err
			}
			childIDs = append(childIDs, child.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &SplitResult{ParentID: parent.ID, ChildIDs: childIDs, Lines: len(lines)}, nil
}

type SplitResult struct {
	ParentID string   `json:"parent_id"`
	ChildIDs []string `json:"child_ids"`
	Lines    int      `json:"lines"`
}

// UndoSplit 撤销拆分：删掉子账目，恢复父账目
func (s *LedgerService) UndoSplit(ctx context.Context, familyID, userID, txID string) error {
	member, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID)
	if err != nil {
		return err
	}
	parent, err := s.db.TransactionByID(ctx, s.db.SQL(), txID, familyID)
	if err != nil {
		return err
	}
	if !parent.IsSplit {
		return domain.ErrBadRequest.WithMessage("这笔账目没有拆分过")
	}
	if parent.Visibility == domain.VisibilityPrivate && parent.MemberID != member.UserID {
		return domain.ErrNotFound
	}

	now := store.NowMS()
	return s.db.WithTx(ctx, func(dbtx *sql.Tx) error {
		if _, err := dbtx.ExecContext(ctx,
			`DELETE FROM transactions WHERE parent_id = ? AND family_id = ?`, txID, familyID); err != nil {
			return err
		}
		return s.db.UnmarkSplit(ctx, dbtx, parent.ID, familyID, now)
	})
}

// ChildTransactions 列出拆出来的子账目
func (s *LedgerService) ChildTransactions(ctx context.Context, familyID, userID, txID string) ([]domain.Transaction, error) {
	if _, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID); err != nil {
		return nil, err
	}
	return s.db.ChildrenOf(ctx, s.db.SQL(), familyID, txID)
}

// splitNote 子账目备注：原备注 + 商品名
func splitNote(parentNote, lineNote string) string {
	lineNote = strings.TrimSpace(lineNote)
	if lineNote == "" {
		return parentNote
	}
	base := parentNote
	if i := strings.Index(base, " · "); i > 0 {
		base = base[:i]
	}
	return base + " · " + lineNote
}
