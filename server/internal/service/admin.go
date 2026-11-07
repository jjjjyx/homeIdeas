package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"family-ledger/server/internal/config"
	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/pkg/authtoken"
	"family-ledger/server/internal/pkg/pw"
	"family-ledger/server/internal/store"
)

// AdminTTL 管理后台令牌有效期：内部工具，给 8 小时，过期重新登录。
const AdminTTL = 8 * time.Hour

type AdminService struct {
	db  *store.DB
	cfg *config.Config
}

func NewAdminService(db *store.DB, cfg *config.Config) *AdminService {
	return &AdminService{db: db, cfg: cfg}
}

type AdminSession struct {
	UserID      string
	Account     string
	Nickname    string
	AccessToken string
	ExpiresIn   int64
}

// Login 只允许平台管理员登录；会话不与 App 共用（scope=admin）。
func (s *AdminService) Login(ctx context.Context, account, password, ip string) (*AdminSession, error) {
	account = strings.ToLower(strings.TrimSpace(account))
	user, err := s.db.UserByAccount(ctx, s.db.SQL(), account)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrCredInvalid
		}
		return nil, err
	}
	if !pw.Verify(user.PasswordHash, password) {
		return nil, domain.ErrCredInvalid
	}
	if !user.IsPlatformAdmin {
		return nil, domain.ErrForbidden.WithMessage("该账号不是平台管理员")
	}
	if user.Status != 1 {
		return nil, domain.ErrUserDisabled
	}

	token, err := authtoken.Sign(s.cfg.JWTSecret, user.ID, authtoken.ScopeAdmin, AdminTTL)
	if err != nil {
		return nil, err
	}
	now := store.NowMS()
	_ = s.db.AddAudit(ctx, s.db.SQL(), &domain.AuditLog{
		UserID: user.ID, Action: "admin.login", IP: ip,
	}, now)

	return &AdminSession{
		UserID: user.ID, Account: user.Account, Nickname: user.Nickname,
		AccessToken: token, ExpiresIn: int64(AdminTTL.Seconds()),
	}, nil
}

// VerifyAdmin 二次校验：令牌有效不代表账号仍有管理员权限（可能已被降权/封禁）。
func (s *AdminService) VerifyAdmin(ctx context.Context, userID string) (*domain.User, error) {
	user, err := s.db.UserByID(ctx, s.db.SQL(), userID)
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	if !user.IsPlatformAdmin || user.Status != 1 {
		return nil, domain.ErrForbidden.WithMessage("没有平台管理员权限")
	}
	return user, nil
}

func (s *AdminService) Overview(ctx context.Context) (*store.AdminOverview, error) {
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).UnixMilli()
	return s.db.AdminOverview(ctx, s.db.SQL(), dayStart)
}

func (s *AdminService) Users(ctx context.Context, keyword string, page, pageSize int) ([]store.AdminUserRow, int, error) {
	page, pageSize = normalizePage(page, pageSize)
	rows, err := s.db.AdminListUsers(ctx, s.db.SQL(), strings.TrimSpace(keyword), pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.CountUsers(ctx, s.db.SQL())
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (s *AdminService) Families(ctx context.Context, keyword string, page, pageSize int) ([]store.AdminFamilyRow, int, error) {
	page, pageSize = normalizePage(page, pageSize)
	rows, err := s.db.AdminListFamilies(ctx, s.db.SQL(), strings.TrimSpace(keyword), pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.AdminCountFamilies(ctx, s.db.SQL(), strings.TrimSpace(keyword))
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (s *AdminService) FamilyTransactions(ctx context.Context, familyID string, page, pageSize int) ([]store.AdminTxRow, error) {
	page, pageSize = normalizePage(page, pageSize)
	return s.db.AdminFamilyTransactions(ctx, s.db.SQL(), familyID, pageSize, (page-1)*pageSize)
}

func (s *AdminService) AuditLogs(ctx context.Context, action string, page, pageSize int) ([]store.AuditRow, int, error) {
	page, pageSize = normalizePage(page, pageSize)
	rows, err := s.db.AdminAuditLogs(ctx, s.db.SQL(), strings.TrimSpace(action), pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.AdminCountAuditLogs(ctx, s.db.SQL(), strings.TrimSpace(action))
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (s *AdminService) SetUserStatus(ctx context.Context, adminID, userID string, status int, ip string) error {
	if status != 0 && status != 1 {
		return domain.ErrBadRequest.WithMessage("status 只能是 0 或 1")
	}
	if adminID == userID && status != 1 {
		return domain.ErrBadRequest.WithMessage("不能封禁自己")
	}
	now := store.NowMS()
	if err := s.db.AdminSetUserStatus(ctx, userID, status, now); err != nil {
		return err
	}
	action := "admin.user.enable"
	if status != 1 {
		action = "admin.user.disable"
	}
	_ = s.db.AddAudit(ctx, s.db.SQL(), &domain.AuditLog{
		UserID: adminID, Action: action, TargetType: "user", TargetID: userID, IP: ip,
	}, now)
	return nil
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}
	return page, pageSize
}
