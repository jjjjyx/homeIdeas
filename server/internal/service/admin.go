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
	Role        string
	AccessToken string
	ExpiresIn   int64
}

// Login 只允许唯一家庭的户主/管理员登录；会话与 App 共用 users 表，但 scope=admin。
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
	if user.Status != 1 {
		return nil, domain.ErrUserDisabled
	}
	role, err := s.adminRoleOf(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if role == "" {
		return nil, domain.ErrForbidden.WithMessage("该账号不是家庭管理员")
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
		UserID: user.ID, Account: user.Account, Nickname: user.Nickname, Role: role,
		AccessToken: token, ExpiresIn: int64(AdminTTL.Seconds()),
	}, nil
}

// VerifyAdmin 二次校验：令牌有效不代表账号仍有管理员权限（可能已被降权/封禁）。
func (s *AdminService) VerifyAdmin(ctx context.Context, userID string) (*domain.User, error) {
	user, err := s.db.UserByID(ctx, s.db.SQL(), userID)
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	if user.Status != 1 {
		return nil, domain.ErrForbidden.WithMessage("没有家庭管理员权限")
	}
	if _, err := s.adminRoleOf(ctx, userID); err != nil {
		return nil, err
	}
	return user, nil
}

// adminRoleOf 返回管理员角色（owner/admin），不是管理员时返回 ErrForbidden。
func (s *AdminService) adminRoleOf(ctx context.Context, userID string) (string, error) {
	familyID, err := s.db.PrimaryFamilyID(ctx, s.db.SQL())
	if err != nil {
		return "", domain.ErrForbidden.WithMessage("系统还没有初始化家庭")
	}
	m, err := s.db.MemberOf(ctx, s.db.SQL(), familyID, userID)
	if err != nil {
		return "", domain.ErrForbidden.WithMessage("该账号不是家庭管理员")
	}
	if m.Role != domain.RoleOwner && m.Role != domain.RoleAdmin {
		return "", domain.ErrForbidden.WithMessage("该账号不是家庭管理员")
	}
	return m.Role, nil
}

func (s *AdminService) RoleOf(ctx context.Context, userID string) string {
	role, _ := s.adminRoleOf(ctx, userID)
	return role
}

// OverviewData 管理后台概览：唯一家庭 + 汇总指标。
type OverviewData struct {
	Family   *domain.Family
	Members  int
	Txs      int
	Amount   int64
	NewTx    int
	NewUsers int
}

func (s *AdminService) Overview(ctx context.Context) (*OverviewData, error) {
	family, err := s.db.PrimaryFamily(ctx, s.db.SQL())
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return &OverviewData{}, nil
		}
		return nil, err
	}
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).UnixMilli()
	o, err := s.db.AdminOverview(ctx, s.db.SQL(), family.ID, dayStart)
	if err != nil {
		return nil, err
	}
	return &OverviewData{
		Family: family, Members: o.Members, Txs: o.Transactions,
		Amount: o.AmountCents, NewTx: o.NewTxToday, NewUsers: o.NewUsersToday,
	}, nil
}

func (s *AdminService) Members(ctx context.Context, keyword string, page, pageSize int) ([]store.AdminMemberRow, int, error) {
	familyID, err := s.db.PrimaryFamilyID(ctx, s.db.SQL())
	if err != nil {
		return nil, 0, err
	}
	page, pageSize = normalizePage(page, pageSize)
	keyword = strings.TrimSpace(keyword)
	rows, err := s.db.AdminListMembers(ctx, s.db.SQL(), familyID, keyword, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.AdminCountMembers(ctx, s.db.SQL(), familyID, keyword)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (s *AdminService) Transactions(ctx context.Context, page, pageSize int) ([]store.AdminTxRow, int, error) {
	familyID, err := s.db.PrimaryFamilyID(ctx, s.db.SQL())
	if err != nil {
		return nil, 0, err
	}
	page, pageSize = normalizePage(page, pageSize)
	rows, err := s.db.AdminTransactions(ctx, s.db.SQL(), familyID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.db.AdminCountTransactions(ctx, s.db.SQL(), familyID)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
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
	if status != 1 {
		if err := s.checkCanManageMember(ctx, adminID, userID); err != nil {
			return err
		}
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

// ResetPassword 管理员重置成员密码。管理员只能重置普通成员，户主可以重置任何人（自己除外）。
func (s *AdminService) ResetPassword(ctx context.Context, adminID, userID, password, ip string) error {
	if len(password) < 8 || len(password) > 72 {
		return domain.ErrBadRequest.WithMessage("密码长度需在 8-72 位之间")
	}
	if adminID == userID {
		return domain.ErrBadRequest.WithMessage("请在个人设置里修改自己的密码")
	}
	if err := s.checkCanManageMember(ctx, adminID, userID); err != nil {
		return err
	}
	hash, err := pw.Hash(password)
	if err != nil {
		return err
	}
	now := store.NowMS()
	if err := s.db.UpdateUserPassword(ctx, s.db.SQL(), userID, hash, now); err != nil {
		return err
	}
	if err := s.db.RevokeTokensOfUser(ctx, s.db.SQL(), userID, now); err != nil {
		return err
	}
	_ = s.db.AddAudit(ctx, s.db.SQL(), &domain.AuditLog{
		UserID: adminID, Action: "admin.user.reset_password", TargetType: "user", TargetID: userID, IP: ip,
	}, now)
	return nil
}

// checkCanManageMember 户主可以管理除自己外的所有人；管理员只能管理普通成员。
// 户主账号不允许被封禁或重置密码（避免管理员接管户主）。
func (s *AdminService) checkCanManageMember(ctx context.Context, actorID, targetID string) error {
	actorRole, err := s.adminRoleOf(ctx, actorID)
	if err != nil {
		return err
	}
	familyID, err := s.db.PrimaryFamilyID(ctx, s.db.SQL())
	if err != nil {
		return err
	}
	target, err := s.db.MemberOf(ctx, s.db.SQL(), familyID, targetID)
	if err != nil {
		return domain.ErrNotFound.WithMessage("成员不存在")
	}
	if target.Role == domain.RoleOwner {
		return domain.ErrForbidden.WithMessage("不能封禁或重置户主")
	}
	if actorRole == domain.RoleAdmin && target.Role != domain.RoleMember {
		return domain.ErrForbidden.WithMessage("管理员只能管理普通成员")
	}
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
