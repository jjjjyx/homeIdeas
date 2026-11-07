package service

import (
	"context"
	"errors"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/store"
)

// mustMember 数据域校验的唯一入口：不属于该家庭一律 403，不暴露家庭是否存在。
func mustMember(ctx context.Context, db *store.DB, q store.Querier, familyID, userID string) (*domain.Member, error) {
	m, err := db.MemberOf(ctx, q, familyID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrForbidden
		}
		return nil, err
	}
	return m, nil
}
