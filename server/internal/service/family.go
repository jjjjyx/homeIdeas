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
	"family-ledger/server/internal/store"
)

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

func (s *FamilyService) Create(ctx context.Context, userID, name, ip string) (*store.FamilyCard, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 20 {
		return nil, domain.ErrBadRequest.WithMessage("家庭名称需为 1-20 个字")
	}

	now := store.NowMS()
	familyID := id.New()
	var rev int64

	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		family := &domain.Family{
			ID:        familyID,
			Name:      name,
			OwnerID:   userID,
			Currency:  "CNY",
			Rev:       0,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := s.db.CreateFamily(ctx, tx, family); err != nil {
			return err
		}
		if err := s.db.AddMember(ctx, tx, familyID, userID, domain.RoleOwner, now); err != nil {
			return err
		}
		r, err := s.db.BumpRev(ctx, tx, familyID, now)
		if err != nil {
			return err
		}
		rev = r
		// 新家庭自动种入默认分类和账户，客户端首次同步即可拿到
		if err := s.db.SeedDefaultCategories(ctx, tx, familyID, rev, now); err != nil {
			return err
		}
		if err := s.db.SeedDefaultAccounts(ctx, tx, familyID, rev, now); err != nil {
			return err
		}
		return s.db.AddAudit(ctx, tx, &domain.AuditLog{
			FamilyID:   familyID,
			UserID:     userID,
			Action:     "family.create",
			TargetType: "family",
			TargetID:   familyID,
			Detail:     name,
			IP:         ip,
		}, now)
	})
	if err != nil {
		return nil, err
	}

	return &store.FamilyCard{
		Family: domain.Family{
			ID: familyID, Name: name, OwnerID: userID, Currency: "CNY",
			Rev: rev, CreatedAt: now, UpdatedAt: now,
		},
		Role:        domain.RoleOwner,
		MemberCount: 1,
	}, nil
}

func (s *FamilyService) List(ctx context.Context, userID string) ([]store.FamilyCard, error) {
	return s.db.FamiliesOfUser(ctx, s.db.SQL(), userID)
}

func (s *FamilyService) Detail(ctx context.Context, userID, familyID string) (*FamilyDetail, error) {
	m, err := s.mustMember(ctx, s.db.SQL(), familyID, userID)
	if err != nil {
		return nil, err
	}
	family, err := s.db.FamilyByID(ctx, s.db.SQL(), familyID)
	if err != nil {
		return nil, err
	}
	members, err := s.db.MembersOf(ctx, s.db.SQL(), familyID)
	if err != nil {
		return nil, err
	}
	return &FamilyDetail{Family: family, MyRole: m.Role, Members: members}, nil
}

func (s *FamilyService) Rename(ctx context.Context, userID, familyID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 20 {
		return domain.ErrBadRequest.WithMessage("家庭名称需为 1-20 个字")
	}
	m, err := s.mustMember(ctx, s.db.SQL(), familyID, userID)
	if err != nil {
		return err
	}
	if !isAdminRole(m.Role) {
		return domain.ErrForbidden.WithMessage("只有户主或管理员可以修改家庭信息")
	}
	now := store.NowMS()
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.db.RenameFamily(ctx, tx, familyID, name, now); err != nil {
			return err
		}
		if _, err := s.db.BumpRev(ctx, tx, familyID, now); err != nil {
			return err
		}
		return s.db.AddAudit(ctx, tx, &domain.AuditLog{
			FamilyID: familyID, UserID: userID, Action: "family.rename",
			TargetType: "family", TargetID: familyID, Detail: name,
		}, now)
	})
}

func (s *FamilyService) CreateInvite(ctx context.Context, userID, familyID, role string, ttlHours int, ip string) (*domain.Invite, error) {
	m, err := s.mustMember(ctx, s.db.SQL(), familyID, userID)
	if err != nil {
		return nil, err
	}
	if !isAdminRole(m.Role) {
		return nil, domain.ErrForbidden.WithMessage("只有户主或管理员可以生成邀请码")
	}
	if role != domain.RoleAdmin && role != domain.RoleMember {
		role = domain.RoleMember
	}
	if role == domain.RoleAdmin && m.Role != domain.RoleOwner {
		return nil, domain.ErrForbidden.WithMessage("只有户主可以邀请管理员")
	}
	if ttlHours <= 0 || ttlHours > 24*30 {
		ttlHours = 24 * 7
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
				FamilyID:  familyID,
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
			FamilyID: familyID, UserID: userID, Action: "family.invite",
			TargetType: "invite", TargetID: invite.ID, Detail: role, IP: ip,
		}, now)
	})
	if err != nil {
		return nil, err
	}
	return invite, nil
}

func (s *FamilyService) Join(ctx context.Context, userID, code, ip string) (*store.FamilyCard, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, domain.ErrBadRequest.WithMessage("请输入邀请码")
	}

	now := store.NowMS()
	var family *domain.Family

	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		inv, err := s.db.InviteByCode(ctx, tx, code)
		if err != nil {
			return domain.ErrInviteInvalid
		}
		if inv.UsedBy != "" {
			return domain.ErrInviteInvalid.WithMessage("邀请码已被使用")
		}
		if inv.ExpiresAt != nil && *inv.ExpiresAt < now {
			return domain.ErrInviteInvalid.WithMessage("邀请码已过期")
		}
		if _, err := s.db.MemberOf(ctx, tx, inv.FamilyID, userID); err == nil {
			return domain.ErrConflict.WithMessage("你已经在这个家庭里了")
		}

		f, err := s.db.FamilyByID(ctx, tx, inv.FamilyID)
		if err != nil {
			return domain.ErrInviteInvalid
		}
		family = f

		role := inv.Role
		if role != domain.RoleAdmin && role != domain.RoleMember {
			role = domain.RoleMember
		}
		// 先原子核销，失败说明被别人抢先用了
		used, err := s.db.MarkInviteUsed(ctx, tx, inv.ID, userID, now)
		if err != nil {
			return err
		}
		if !used {
			return domain.ErrInviteInvalid.WithMessage("邀请码已被使用")
		}
		if err := s.db.AddMember(ctx, tx, inv.FamilyID, userID, role, now); err != nil {
			return err
		}
		if _, err := s.db.BumpRev(ctx, tx, inv.FamilyID, now); err != nil {
			return err
		}
		return s.db.AddAudit(ctx, tx, &domain.AuditLog{
			FamilyID: inv.FamilyID, UserID: userID, Action: "family.join",
			TargetType: "family", TargetID: inv.FamilyID, IP: ip,
		}, now)
	})
	if err != nil {
		return nil, err
	}

	count, _ := s.db.CountMembers(ctx, s.db.SQL(), family.ID)
	return &store.FamilyCard{Family: *family, Role: domain.RoleMember, MemberCount: count}, nil
}

func (s *FamilyService) Members(ctx context.Context, userID, familyID string) ([]domain.Member, error) {
	if _, err := s.mustMember(ctx, s.db.SQL(), familyID, userID); err != nil {
		return nil, err
	}
	return s.db.MembersOf(ctx, s.db.SQL(), familyID)
}

func (s *FamilyService) SetRole(ctx context.Context, actorID, familyID, targetID, role string) error {
	if role != domain.RoleAdmin && role != domain.RoleMember {
		return domain.ErrBadRequest.WithMessage("角色只能是 admin 或 member")
	}
	actor, err := s.mustMember(ctx, s.db.SQL(), familyID, actorID)
	if err != nil {
		return err
	}
	if actor.Role != domain.RoleOwner {
		return domain.ErrForbidden.WithMessage("只有户主可以调整成员角色")
	}
	if actorID == targetID {
		return domain.ErrBadRequest.WithMessage("不能修改自己的角色")
	}
	if _, err := s.mustMember(ctx, s.db.SQL(), familyID, targetID); err != nil {
		return err
	}

	now := store.NowMS()
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.db.UpdateMemberRole(ctx, tx, familyID, targetID, role); err != nil {
			return err
		}
		if _, err := s.db.BumpRev(ctx, tx, familyID, now); err != nil {
			return err
		}
		return s.db.AddAudit(ctx, tx, &domain.AuditLog{
			FamilyID: familyID, UserID: actorID, Action: "member.set_role",
			TargetType: "user", TargetID: targetID, Detail: role,
		}, now)
	})
}

func (s *FamilyService) RemoveMember(ctx context.Context, actorID, familyID, targetID string) error {
	actor, err := s.mustMember(ctx, s.db.SQL(), familyID, actorID)
	if err != nil {
		return err
	}
	target, err := s.mustMember(ctx, s.db.SQL(), familyID, targetID)
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
		if err := s.db.RemoveMember(ctx, tx, familyID, targetID); err != nil {
			return err
		}
		if _, err := s.db.BumpRev(ctx, tx, familyID, now); err != nil {
			return err
		}
		return s.db.AddAudit(ctx, tx, &domain.AuditLog{
			FamilyID: familyID, UserID: actorID, Action: "member.remove",
			TargetType: "user", TargetID: targetID,
		}, now)
	})
}

func (s *FamilyService) Leave(ctx context.Context, userID, familyID string) error {
	actor, err := s.mustMember(ctx, s.db.SQL(), familyID, userID)
	if err != nil {
		return err
	}
	if actor.Role == domain.RoleOwner {
		return domain.ErrForbidden.WithMessage("户主不能退出家庭，请先转让或删除家庭")
	}

	now := store.NowMS()
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.db.RemoveMember(ctx, tx, familyID, userID); err != nil {
			return err
		}
		if _, err := s.db.BumpRev(ctx, tx, familyID, now); err != nil {
			return err
		}
		return s.db.AddAudit(ctx, tx, &domain.AuditLog{
			FamilyID: familyID, UserID: userID, Action: "family.leave",
			TargetType: "family", TargetID: familyID,
		}, now)
	})
}

// mustMember 是数据域校验的唯一入口：不属于该家庭一律 403，避免暴露家庭是否存在。
func (s *FamilyService) mustMember(ctx context.Context, q store.Querier, familyID, userID string) (*domain.Member, error) {
	m, err := s.db.MemberOf(ctx, q, familyID, userID)
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
