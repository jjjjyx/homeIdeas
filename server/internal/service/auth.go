package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"family-ledger/server/internal/config"
	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/pkg/authtoken"
	"family-ledger/server/internal/pkg/id"
	"family-ledger/server/internal/pkg/pw"
	"family-ledger/server/internal/store"
)

var accountPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,31}$`)

type AuthService struct {
	db  *store.DB
	cfg *config.Config
}

func NewAuthService(db *store.DB, cfg *config.Config) *AuthService {
	return &AuthService{db: db, cfg: cfg}
}

type RegisterInput struct {
	Account    string
	Password   string
	Nickname   string
	InviteCode string
	Device     string
	IP         string
}

type LoginInput struct {
	Account  string
	Password string
	Device   string
	IP       string
}

// Session 登录结果：令牌 + 基础用户信息。
type Session struct {
	UserID       string
	Account      string
	Nickname     string
	IsAdmin      bool
	AccessToken  string
	ExpiresIn    int64
	RefreshToken string
}

func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*Session, error) {
	account := strings.ToLower(strings.TrimSpace(in.Account))
	if !accountPattern.MatchString(account) {
		return nil, domain.ErrBadRequest.WithMessage("账号需为 3-32 位小写字母、数字、点、下划线或短横线")
	}
	if len(in.Password) < 8 || len(in.Password) > 72 {
		return nil, domain.ErrBadRequest.WithMessage("密码长度需在 8-72 位之间")
	}
	nickname := strings.TrimSpace(in.Nickname)
	if nickname == "" {
		nickname = account
	}
	if utf8.RuneCountInString(nickname) > 20 {
		return nil, domain.ErrBadRequest.WithMessage("昵称最多 20 个字")
	}

	// 密码哈希放在事务外，避免 bcrypt 期间占住唯一的数据库连接。
	hash, err := pw.Hash(in.Password)
	if err != nil {
		return nil, err
	}

	now := store.NowMS()
	newUserID := id.New()
	var joinedFamilyID string
	var isFirstUser bool

	err = s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := s.db.UserByAccount(ctx, tx, account); err == nil {
			return domain.ErrConflict.WithMessage("账号已被占用")
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}

		total, err := s.db.CountUsers(ctx, tx)
		if err != nil {
			return err
		}
		// 库里第一个用户是引导账号：免邀请码，并自动成为平台管理员。
		isFirstUser = total == 0

		inviteCode := strings.TrimSpace(in.InviteCode)
		var invite *domain.Invite
		if !isFirstUser {
			if inviteCode == "" {
				if !s.cfg.AllowPublicRegister {
					return domain.ErrInviteRequired
				}
			} else {
				inv, err := s.db.InviteByCode(ctx, tx, inviteCode)
				if err != nil {
					return domain.ErrInviteInvalid
				}
				if inv.UsedBy != "" {
					return domain.ErrInviteInvalid.WithMessage("邀请码已被使用")
				}
				if inv.ExpiresAt != nil && *inv.ExpiresAt < now {
					return domain.ErrInviteInvalid.WithMessage("邀请码已过期")
				}
				invite = inv
			}
		}

		user := &domain.User{
			ID:              newUserID,
			Account:         account,
			PasswordHash:    hash,
			Nickname:        nickname,
			Status:          1,
			IsPlatformAdmin: isFirstUser,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := s.db.CreateUser(ctx, tx, user); err != nil {
			return err
		}

		if invite != nil {
			if err := s.joinFamilyTx(ctx, tx, invite, newUserID, now); err != nil {
				return err
			}
			joinedFamilyID = invite.FamilyID
		}

		return s.db.AddAudit(ctx, tx, &domain.AuditLog{
			FamilyID: joinedFamilyID,
			UserID:   newUserID,
			Action:   "auth.register",
			Detail:   "account=" + account,
			IP:       in.IP,
		}, now)
	})
	if err != nil {
		return nil, err
	}

	session, err := s.issueSession(ctx, &domain.User{
		ID:              newUserID,
		Account:         account,
		Nickname:        nickname,
		IsPlatformAdmin: isFirstUser,
	}, in.Device)
	if err != nil {
		return nil, err
	}
	return session, nil
}

// joinFamilyTx 把新用户加入邀请码对应的家庭，并核销邀请码。
// 先核销再加入：核销是带条件的原子更新，并发下只有一个能成功。
func (s *AuthService) joinFamilyTx(ctx context.Context, tx *sql.Tx, inv *domain.Invite, userID string, now int64) error {
	used, err := s.db.MarkInviteUsed(ctx, tx, inv.ID, userID, now)
	if err != nil {
		return err
	}
	if !used {
		return domain.ErrInviteInvalid.WithMessage("邀请码已被使用")
	}

	role := inv.Role
	if role != domain.RoleAdmin && role != domain.RoleMember {
		role = domain.RoleMember
	}
	if err := s.db.AddMember(ctx, tx, inv.FamilyID, userID, role, now); err != nil {
		return err
	}
	_, err = s.db.BumpRev(ctx, tx, inv.FamilyID, now)
	return err
}

func (s *AuthService) Login(ctx context.Context, in LoginInput) (*Session, error) {
	account := strings.ToLower(strings.TrimSpace(in.Account))
	user, err := s.db.UserByAccount(ctx, s.db.SQL(), account)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrCredInvalid
		}
		return nil, err
	}
	if !pw.Verify(user.PasswordHash, in.Password) {
		return nil, domain.ErrCredInvalid
	}
	if user.Status != 1 {
		return nil, domain.ErrUserDisabled
	}

	now := store.NowMS()
	_ = s.db.AddAudit(ctx, s.db.SQL(), &domain.AuditLog{
		UserID: user.ID,
		Action: "auth.login",
		IP:     in.IP,
	}, now)

	return s.issueSession(ctx, user, in.Device)
}

func (s *AuthService) issueSession(ctx context.Context, user *domain.User, device string) (*Session, error) {
	now := store.NowMS()
	access, err := authtoken.Sign(s.cfg.JWTSecret, user.ID, authtoken.ScopeApp, s.cfg.AccessTTL)
	if err != nil {
		return nil, err
	}
	refreshPlain, err := randomToken()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(refreshPlain))
	token := &domain.RefreshToken{
		ID:        id.New(),
		UserID:    user.ID,
		TokenHash: hex.EncodeToString(sum[:]),
		Device:    device,
		ExpiresAt: now + s.cfg.RefreshTTL.Milliseconds(),
		CreatedAt: now,
	}
	if err := s.db.CreateRefreshToken(ctx, s.db.SQL(), token); err != nil {
		return nil, err
	}
	return &Session{
		UserID:       user.ID,
		Account:      user.Account,
		Nickname:     user.Nickname,
		IsAdmin:      user.IsPlatformAdmin,
		AccessToken:  access,
		ExpiresIn:    int64(s.cfg.AccessTTL.Seconds()),
		RefreshToken: refreshPlain,
	}, nil
}

// Refresh 用长期令牌换新 access token。
func (s *AuthService) Refresh(ctx context.Context, refreshPlain string) (*Session, error) {
	sum := sha256.Sum256([]byte(refreshPlain))
	token, err := s.db.RefreshTokenByHash(ctx, s.db.SQL(), hex.EncodeToString(sum[:]))
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	now := store.NowMS()
	if token.RevokedAt != nil || token.ExpiresAt < now {
		return nil, domain.ErrUnauthorized
	}
	user, err := s.db.UserByID(ctx, s.db.SQL(), token.UserID)
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	if user.Status != 1 {
		return nil, domain.ErrUserDisabled
	}

	access, err := authtoken.Sign(s.cfg.JWTSecret, user.ID, authtoken.ScopeApp, s.cfg.AccessTTL)
	if err != nil {
		return nil, err
	}
	return &Session{
		UserID:       user.ID,
		Account:      user.Account,
		Nickname:     user.Nickname,
		IsAdmin:      user.IsPlatformAdmin,
		AccessToken:  access,
		ExpiresIn:    int64(s.cfg.AccessTTL.Seconds()),
		RefreshToken: refreshPlain,
	}, nil
}

func (s *AuthService) Logout(ctx context.Context, refreshPlain string) error {
	sum := sha256.Sum256([]byte(refreshPlain))
	token, err := s.db.RefreshTokenByHash(ctx, s.db.SQL(), hex.EncodeToString(sum[:]))
	if err != nil {
		return nil // 幂等：令牌不存在也当作成功
	}
	return s.db.RevokeRefreshToken(ctx, s.db.SQL(), token.ID, store.NowMS())
}

func (s *AuthService) UserByID(ctx context.Context, userID string) (*domain.User, error) {
	user, err := s.db.UserByID(ctx, s.db.SQL(), userID)
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	if user.Status != 1 {
		return nil, domain.ErrUserDisabled
	}
	return user, nil
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
