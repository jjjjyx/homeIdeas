package store

import (
	"context"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/pkg/id"
)

func (d *DB) AddAudit(ctx context.Context, q Querier, a *domain.AuditLog, now int64) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO audit_logs (id, family_id, user_id, action, target_type, target_id, detail, ip, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		id.New(), a.FamilyID, a.UserID, a.Action, a.TargetType, a.TargetID, a.Detail, a.IP, now)
	return err
}
