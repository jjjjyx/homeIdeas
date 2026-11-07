package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/pkg/id"
	"family-ledger/server/internal/pkg/pw"
	"family-ledger/server/internal/store"
)

// FamilyService 单家庭模式下的家庭信息与成员管理。
//
// 系统只有一个家庭：初始化时创建，之后不存在「创建/加入/切换家庭」，
// 成员由户主或管理员在管理后台直接创建。
type FamilyService struct {
	db *store.DB
}

func NewFamilyService(db *store.DB) *FamilyService {
	return &FamilyService{db: db}
}

type FamilyDetail struct {
	Family  *domain.Family
	MyRole  string
	Members []domain.Member
}

// PrimaryID 返回唯一家庭 ID；系统未初始化时返回 ErrNotFound。
func (s *FamilyService) PrimaryID(ctx context.Context) (string, error) {
	return s.db.PrimaryFamilyID(ctx, s.db.SQL())
}

func (s *FamilyService) Detail(ctx context.Context, userID string) (*FamilyDetail, error) {
	family, err := s.db.PrimaryFamily(ctx, s.db.SQL())
	if err != nil {
		return nil, err
	}
	m, err := s.mustMember(ctx, family.ID, userID)
	if err != nil {
		return nil, err
	}
	members, err := s.db.MembersOf(ctx, s.db.SQL(), family.ID)
	if err != nil {
		return nil, err
	}
	return &FamilyDetail{Family: family, MyRole: m.Role, Members: members}, nil
}

func (s *FamilyService) Rename(ctx context.Context, userID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 20 {
		return domain.ErrBadRequest.WithMessage("家庭名称需为 1-20 个字")
	}
	family, err := s.db.PrimaryFamily(ctx, s.db.SQL())
	if err != nil {
		return err
	}
	m, err := s.mustMember(ctx, family.ID, userID)
	if err != nil {
		return err
	}
	if !isAdminRole(m.Role) {
		return domain.ErrForbidden.WithMessage("只有户主或管理员可以修改家庭信息")
	}
	now := store.NowMS()
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.db.RenameFamily(ctx, tx, family.ID, name, now); err != nil {
			return err
		}
		if _, err := s.db.BumpRev(ctx, tx, family.ID, now); err != nil {
			return err
		}
		return s.db.AddAudit(ctx, tx, &domain.AuditLog{
			FamilyID: family.ID, UserID: userID, Action: "family.rename",
			TargetType: "family", TargetID: family.ID, Detail: name,
		}, now)
	})
}

func (s *FamilyService) Members(ctx context.Context, userID string) ([]domain.Member, error) {
	family, err := s.db.PrimaryFamily(ctx, s.db.SQL())
	if err != nil {
		return nil, err
	}
	if _, err := s.mustMember(ctx, family.ID, userID); err != nil {
		return nil, err
	}
	return s.db.MembersOf(ctx, s.db.SQL(), family.ID)
}

// AddMember 由户主/管理员直接创建家庭成员账号。
func (s *FamilyService) AddMember(ctx context.Context, actorID, account, password, nickname, role string) (*domain.Member, error) {
	family, err := s.db.PrimaryFamily(ctx, s.db.SQL())
	if err != nil {
		return nil, err
	}
	actor, err := s.mustMember(ctx, family.ID, actorID)
	if err != nil {
		return nil, err
	}
	if !isAdminRole(actor.Role) {
		return nil, domain.ErrForbidden.WithMessage("只有户主或管理员可以添加成员")
	}

	account = strings.ToLower(strings.TrimSpace(account))
	if !accountPattern.MatchString(account) {
		return nil, domain.ErrBadRequest.WithMessage("账号需为 3-32 位小写字母、数字、点、下划线或短横线")
	}
	if len(password) < 8 || len(password) > 72 {
		return nil, domain.ErrBadRequest.WithMessage("密码长度需在 8-72 位之间")
	}
	nickname = strings.TrimSpace(nickname)
	if nickname == "" {
		nickname = account
	}
	if utf8.RuneCountInString(nickname) > 20 {
		return nil, domain.ErrBadRequest.WithMessage("昵称最多 20 个字")
	}
	role = strings.TrimSpace(role)
	if role == "" {
		role = domain.RoleMember
	}
	if role != domain.RoleAdmin && role != domain.RoleMember {
		return nil, domain.ErrBadRequest.WithMessage("新成员角色只能是 admin 或 member")
	}
	if role == domain.RoleAdmin && actor.Role != domain.RoleOwner {
		return nil, domain.ErrForbidden.WithMessage("只有户主可以添加管理员")
	}

	hash, err := pw.Hash(password)
	if err != nil {
		return nil, err
	}

	now := store.NowMS()
	userID := id.New()
	err = s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := s.db.UserByAccount(ctx, tx, account); err == nil {
			return domain.ErrConflict.WithMessage("账号已被占用")
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		user := &domain.User{
			ID: userID, Account: account, PasswordHash: hash, Nickname: nickname,
			Status: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.db.CreateUser(ctx, tx, user); err != nil {
			return err
		}
		if err := s.db.AddMember(ctx, tx, family.ID, userID, role, now); err != nil {
			return err
		}
		if _, err := s.db.BumpRev(ctx, tx, family.ID, now); err != nil {
			return err
		}
		return s.db.AddAudit(ctx, tx, &domain.AuditLog{
			FamilyID: family.ID, UserID: actorID, Action: "member.add",
			TargetType: "user", TargetID: userID, Detail: role,
		}, now)
	})
	if err != nil {
		return nil, err
	}
	return &domain.Member{
		FamilyID: family.ID, UserID: userID, Role: role, JoinedAt: now,
		Nickname: nickname, Account: account,
	}, nil
}

// CreateInvite 生成加入唯一家庭的邀请码。户主/管理员可发；只有户主能发 admin 邀请。
func (s *FamilyService) CreateInvite(ctx context.Context, userID, role string, ttlHours int, ip string) (*domain.Invite, error) {
	family, err := s.db.PrimaryFamily(ctx, s.db.SQL())
	if err != nil {
		return nil, err
	}
	actor, err := s.mustMember(ctx, family.ID, userID)
	if err != nil {
		return nil, err
	}
	if !isAdminRole(actor.Role) {
		return nil, domain.ErrForbidden.WithMessage("只有户主或管理员可以生成邀请码")
	}
	role = strings.TrimSpace(role)
	if role != domain.RoleAdmin && role != domain.RoleMember {
		role = domain.RoleMember
	}
	if role == domain.RoleAdmin && actor.Role != domain.RoleOwner {
		return nil, domain.ErrForbidden.WithMessage("只有户主可以邀请管理员")
	}
	if ttlHours <= 0 {
		ttlHours = 24 * 7
	}
	if ttlHours > 24*30 {
		ttlHours = 24 * 30
	}

	now := store.NowMS()
	expires := now + int64(ttlHours)*3600_000
	var invite *domain.Invite

	err = s.db.WithTx(ctx, func(tx *sql.Tx) error {
		for attempt := 0; attempt < 5; attempt++ {
			code, genErr := generateInviteCode(6)
			if genErr != nil {
				return genErr
			}
			inv := &domain.Invite{
				ID:        id.New(),
				FamilyID:  family.ID,
				Code:      code,
				Role:      role,
				CreatedBy: userID,
				ExpiresAt: &expires,
				CreatedAt: now,
			}
			if err := s.db.CreateInvite(ctx, tx, inv); err != nil {
				if strings.Contains(err.Error(), "UNIQUE") {
					continue // 撞码，重试
				}
				return err
			}
			invite = inv
			break
		}
		if invite == nil {
			return domain.ErrInternal.WithMessage("生成邀请码失败，请重试")
		}
		return s.db.AddAudit(ctx, tx, &domain.AuditLog{
			FamilyID: family.ID, UserID: userID, Action: "family.invite",
			TargetType: "invite", TargetID: invite.ID, Detail: role, IP: ip,
		}, now)
	})
	if err != nil {
		return nil, err
	}
	return invite, nil
}

func (s *FamilyService) SetRole(ctx context.Context, actorID, targetID, role string) error {
	if role != domain.RoleAdmin && role != domain.RoleMember {
		return domain.ErrBadRequest.WithMessage("角色只能是 admin 或 member")
	}
	family, err := s.db.PrimaryFamily(ctx, s.db.SQL())
	if err != nil {
		return err
	}
	actor, err := s.mustMember(ctx, family.ID, actorID)
	if err != nil {
		return err
	}
	if actor.Role != domain.RoleOwner {
		return domain.ErrForbidden.WithMessage("只有户主可以调整成员角色")
	}
	if actorID == targetID {
		return domain.ErrBadRequest.WithMessage("不能修改自己的角色")
	}
	if _, err := s.mustMember(ctx, family.ID, targetID); err != nil {
		return err
	}

	now := store.NowMS()
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.db.UpdateMemberRole(ctx, tx, family.ID, targetID, role); err != nil {
			return err
		}
		if _, err := s.db.BumpRev(ctx, tx, family.ID, now); err != nil {
			return err
		}
		return s.db.AddAudit(ctx, tx, &domain.AuditLog{
			FamilyID: family.ID, UserID: actorID, Action: "member.set_role",
			TargetType: "user", TargetID: targetID, Detail: role,
		}, now)
	})
}

func (s *FamilyService) RemoveMember(ctx context.Context, actorID, targetID string) error {
	family, err := s.db.PrimaryFamily(ctx, s.db.SQL())
	if err != nil {
		return err
	}
	actor, err := s.mustMember(ctx, family.ID, actorID)
	if err != nil {
		return err
	}
	target, err := s.mustMember(ctx, family.ID, targetID)
	if err != nil {
		return err
	}
	if target.Role == domain.RoleOwner {
		return domain.ErrLastOwner.WithMessage("不能移除户主")
	}
	if actor.Role == domain.RoleMember {
		return domain.ErrForbidden.WithMessage("只有户主或管理员可以移除成员")
	}
	if actor.Role == domain.RoleAdmin && target.Role != domain.RoleMember {
		return domain.ErrForbidden.WithMessage("管理员只能移除普通成员")
	}

	now := store.NowMS()
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.db.RemoveMember(ctx, tx, family.ID, targetID); err != nil {
			return err
		}
		// 移除后立即让该成员的设备令牌失效
		if err := s.db.RevokeTokensOfUser(ctx, tx, targetID, now); err != nil {
			return err
		}
		if _, err := s.db.BumpRev(ctx, tx, family.ID, now); err != nil {
			return err
		}
		return s.db.AddAudit(ctx, tx, &domain.AuditLog{
			FamilyID: family.ID, UserID: actorID, Action: "member.remove",
			TargetType: "user", TargetID: targetID,
		}, now)
	})
}

func (s *FamilyService) mustMember(ctx context.Context, familyID, userID string) (*domain.Member, error) {
	m, err := s.db.MemberOf(ctx, s.db.SQL(), familyID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrForbidden
		}
		return nil, err
	}
	return m, nil
}

func isAdminRole(role string) bool {
	return role == domain.RoleOwner || role == domain.RoleAdmin
}

// generateInviteCode 去掉 0/O/1/I 等易混字符。
const inviteAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func generateInviteCode(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = inviteAlphabet[int(buf[i])%len(inviteAlphabet)]
	}
	return string(buf), nil
}
