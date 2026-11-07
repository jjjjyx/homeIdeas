package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"family-ledger/server/internal/config"
	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/pkg/authtoken"
	"family-ledger/server/internal/pkg/id"
	"family-ledger/server/internal/pkg/pw"
	"family-ledger/server/internal/store"
)

// DeviceService 设备配对授权。
//
// 新设备首次打开 App 时发起配对（6 位配对码 + 32 字节配对密钥），
// 管理员在后台确认设备和身份后，App 轮询即可拿到令牌完成登录。
// 这样家人不用记账号密码，重装后也能重新授权到同一个已有身份。
type DeviceService struct {
	db    *store.DB
	cfg   *config.Config
	auth  *AuthService
	nowFn func() int64
}

func NewDeviceService(db *store.DB, cfg *config.Config, auth *AuthService) *DeviceService {
	return &DeviceService{db: db, cfg: cfg, auth: auth, nowFn: store.NowMS}
}

// 配对有效期
const devicePairTTL = 10 * time.Minute

// 同一 IP 在窗口内最多发起几次配对（防刷）
const (
	deviceRateWindow = 5 * time.Minute
	deviceRateMax    = 5
)

type StartDeviceInput struct {
	DeviceUID  string
	DeviceKey  string
	Model      string
	OSVersion  string
	AppVersion string
	IP         string
}

type StartDeviceResult struct {
	PairCode   string `json:"pair_code"`
	PairSecret string `json:"pair_secret"`
	ExpiresIn  int64  `json:"expires_in"`
}

// StartDevice 发起一次配对请求
func (s *DeviceService) StartDevice(ctx context.Context, in StartDeviceInput) (*StartDeviceResult, error) {
	if strings.TrimSpace(in.DeviceUID) == "" {
		return nil, domain.ErrBadRequest.WithMessage("缺少设备标识")
	}
	now := s.nowFn()
	// 顺手清掉过期记录
	_, _ = s.db.ExpireStaleDevices(ctx, s.db.SQL(), now)

	if in.IP != "" {
		n, err := s.db.CountRecentPendingByIP(ctx, s.db.SQL(), in.IP, now-deviceRateWindow.Milliseconds())
		if err != nil {
			return nil, err
		}
		if n >= deviceRateMax {
			return nil, domain.New(4006, 429, "配对请求过于频繁，请 5 分钟后再试")
		}
	}

	code, err := randomDigits(6)
	if err != nil {
		return nil, err
	}
	secret, err := randomToken()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(secret))

	dev := &domain.Device{
		ID:         id.New(),
		DeviceUID:  truncate(in.DeviceUID, 64),
		DeviceKey:  truncate(in.DeviceKey, 80),
		Model:      truncate(in.Model, 64),
		OSVersion:  truncate(in.OSVersion, 32),
		AppVersion: truncate(in.AppVersion, 32),
		IP:         truncate(in.IP, 64),
		Status:     domain.DevicePending,
		PairCode:   code,
		CreatedAt:  now,
		ExpiresAt:  now + devicePairTTL.Milliseconds(),
	}
	if err := s.db.CreateDevice(ctx, s.db.SQL(), dev, hex.EncodeToString(sum[:])); err != nil {
		return nil, err
	}
	return &StartDeviceResult{
		PairCode:   code,
		PairSecret: secret,
		ExpiresIn:  int64(devicePairTTL.Seconds()),
	}, nil
}

type PollDeviceResult struct {
	Status       string `json:"status"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	DeviceName   string `json:"device_name,omitempty"`
}

// PollDevice App 轮询配对状态；批准后返回令牌
func (s *DeviceService) PollDevice(ctx context.Context, pairCode, pairSecret string) (*PollDeviceResult, error) {
	dev, secretHash, err := s.db.DeviceByPairCode(ctx, s.db.SQL(), pairCode)
	if err != nil {
		return nil, domain.ErrNotFound.WithMessage("配对请求不存在或已失效")
	}
	sum := sha256.Sum256([]byte(pairSecret))
	if hex.EncodeToString(sum[:]) != secretHash {
		return nil, domain.ErrNotFound.WithMessage("配对请求不存在或已失效")
	}

	now := s.nowFn()
	if dev.Status == domain.DevicePending && dev.ExpiresAt < now {
		_ = s.db.SetDeviceStatus(ctx, s.db.SQL(), dev.ID, domain.DeviceExpired, now)
		dev.Status = domain.DeviceExpired
	}

	out := &PollDeviceResult{Status: dev.Status, DeviceName: dev.Model}
	if dev.Status != domain.DeviceApproved {
		return out, nil
	}

	user, err := s.db.UserByID(ctx, s.db.SQL(), dev.UserID)
	if err != nil {
		return nil, err
	}

	// 每台设备只保留一个有效刷新令牌
	_ = s.db.RevokeByDevice(ctx, s.db.SQL(), dev.ID, now)
	session, err := s.issueForDevice(ctx, user, dev, now)
	if err != nil {
		return nil, err
	}
	s.db.TouchDevice(ctx, s.db.SQL(), dev.ID, now)

	out.AccessToken = session.AccessToken
	out.RefreshToken = session.RefreshToken
	out.ExpiresIn = session.ExpiresIn
	return out, nil
}

// issueForDevice 签发绑定到设备的令牌（吊销设备即可让令牌失效）
func (s *DeviceService) issueForDevice(ctx context.Context, user *domain.User, dev *domain.Device, now int64) (*Session, error) {
	access, err := authtoken.Sign(s.cfg.JWTSecret, user.ID, authtoken.ScopeApp, s.cfg.AccessTTL)
	if err != nil {
		return nil, err
	}
	refreshPlain, err := randomToken()
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(refreshPlain))
	label := dev.Model
	if label == "" {
		label = "配对设备"
	}
	token := &domain.RefreshToken{
		ID:        id.New(),
		UserID:    user.ID,
		TokenHash: hex.EncodeToString(sum[:]),
		Device:    truncate(label, 64),
		DeviceID:  dev.ID,
		ExpiresAt: now + int64(float64(s.cfg.RefreshTTL.Milliseconds())*1.0),
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

type ApproveInput struct {
	// 绑定到已有用户
	UserID string
	// 或者新建用户
	NewAccount  string
	NewNickname string
	NewPassword string

	FamilyID string
	Role     string
}

type ApproveResult struct {
	DeviceID string `json:"device_id"`
	UserID   string `json:"user_id"`
	Account  string `json:"account"`
	// 新建用户时返回自动生成的密码，供管理员转告本人（也可之后重置）
	GeneratedPassword string `json:"generated_password,omitempty"`
	// 被本次批准顶掉的旧设备记录（同一台手机的历史绑定）
	Superseded []string `json:"superseded,omitempty"`
}

// Approve 管理员批准配对：绑定已有用户或新建用户
func (s *DeviceService) Approve(ctx context.Context, adminUserID, deviceID string, in ApproveInput) (*ApproveResult, error) {
	admin, err := s.db.UserByID(ctx, s.db.SQL(), adminUserID)
	if err != nil {
		return nil, err
	}
	if !admin.IsPlatformAdmin {
		return nil, domain.ErrForbidden.WithMessage("只有平台管理员可以在后台批准设备")
	}

	dev, err := s.db.DeviceByID(ctx, s.db.SQL(), deviceID)
	if err != nil {
		return nil, err
	}
	if dev.Status != domain.DevicePending {
		return nil, domain.ErrBadRequest.WithMessage("这条配对请求已经处理过了")
	}

	now := s.nowFn()
	result := &ApproveResult{DeviceID: deviceID}
	userID := strings.TrimSpace(in.UserID)

	err = s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if userID == "" {
			account := strings.TrimSpace(in.NewAccount)
			nickname := strings.TrimSpace(in.NewNickname)
			if account == "" || nickname == "" {
				return domain.ErrBadRequest.WithMessage("请选择已有用户，或填写新用户的账号和昵称")
			}
			pwd := strings.TrimSpace(in.NewPassword)
			if pwd == "" {
				gen, err := randomPassword(12)
				if err != nil {
					return err
				}
				pwd = gen
				result.GeneratedPassword = gen
			}
			hash, err := pw.Hash(pwd)
			if err != nil {
				return err
			}
			u := &domain.User{
				ID: id.New(), Account: strings.ToLower(account), PasswordHash: hash,
				Nickname: nickname, Status: 1, CreatedAt: now, UpdatedAt: now,
			}
			if err := s.db.CreateUser(ctx, tx, u); err != nil {
				return domain.ErrBadRequest.WithMessage("账号已存在或格式不合法")
			}
			userID = u.ID
			result.Account = u.Account
		} else {
			u, err := s.db.UserByID(ctx, tx, userID)
			if err != nil {
				return domain.ErrBadRequest.WithMessage("用户不存在")
			}
			result.Account = u.Account
		}
		result.UserID = userID

		// 加入家庭（已在里面就跳过）
		if in.FamilyID != "" {
			role := strings.TrimSpace(in.Role)
			if role != domain.RoleOwner && role != domain.RoleAdmin && role != domain.RoleMember {
				role = domain.RoleMember
			}
			if err := s.db.AddMemberIfAbsent(ctx, tx, in.FamilyID, userID, role, now); err != nil {
				return err
			}
		}

		if err := s.db.ApproveDevice(ctx, tx, deviceID, userID, in.FamilyID, in.Role, adminUserID, now); err != nil {
			return err
		}
		// 同一台手机之前批准过？让旧记录失效，避免一台设备多条授权
		oldIDs, err := s.db.SupersedeDevicesByKey(ctx, tx, dev.DeviceKey, deviceID, now)
		if err != nil {
			return err
		}
		result.Superseded = oldIDs
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *DeviceService) Reject(ctx context.Context, adminUserID, deviceID string) error {
	if err := s.requireAdmin(ctx, adminUserID); err != nil {
		return err
	}
	return s.db.SetDeviceStatus(ctx, s.db.SQL(), deviceID, domain.DeviceRejected, s.nowFn())
}

// Revoke 吊销设备：同时作废它的所有刷新令牌
func (s *DeviceService) Revoke(ctx context.Context, adminUserID, deviceID string) error {
	if err := s.requireAdmin(ctx, adminUserID); err != nil {
		return err
	}
	now := s.nowFn()
	if err := s.db.SetDeviceStatus(ctx, s.db.SQL(), deviceID, domain.DeviceRevoked, now); err != nil {
		return err
	}
	return s.db.RevokeByDevice(ctx, s.db.SQL(), deviceID, now)
}

func (s *DeviceService) Pending(ctx context.Context, adminUserID string) ([]domain.Device, error) {
	if err := s.requireAdmin(ctx, adminUserID); err != nil {
		return nil, err
	}
	_, _ = s.db.ExpireStaleDevices(ctx, s.db.SQL(), s.nowFn())
	return s.db.PendingDevices(ctx, s.db.SQL(), 50)
}

func (s *DeviceService) List(ctx context.Context, adminUserID string, includeExpired bool) ([]domain.Device, error) {
	if err := s.requireAdmin(ctx, adminUserID); err != nil {
		return nil, err
	}
	return s.db.AllDevices(ctx, s.db.SQL(), 200, includeExpired)
}

func (s *DeviceService) requireAdmin(ctx context.Context, userID string) error {
	u, err := s.db.UserByID(ctx, s.db.SQL(), userID)
	if err != nil {
		return err
	}
	if !u.IsPlatformAdmin {
		return domain.ErrForbidden.WithMessage("只有平台管理员可以操作")
	}
	return nil
}

func randomDigits(n int) (string, error) {
	buf := make([]byte, n)
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		buf[i] = byte('0' + v.Int64())
	}
	return string(buf), nil
}

func randomPassword(n int) (string, error) {
	const alphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, n)
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		buf[i] = alphabet[v.Int64()]
	}
	return string(buf), nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

var _ = fmt.Sprintf

// PendingItem 待授权设备 + 这台手机之前的授权记录
type PendingItem struct {
	domain.Device
	LastDeviceID   string `json:"last_device_id,omitempty"`
	LastUserID     string `json:"last_user_id,omitempty"`
	LastAccount    string `json:"last_account,omitempty"`
	LastNickname   string `json:"last_nickname,omitempty"`
	LastFamilyID   string `json:"last_family_id,omitempty"`
	LastRole       string `json:"last_role,omitempty"`
	LastApprovedAt *int64 `json:"last_approved_at,omitempty"`
}

// PendingWithHistory 列出待授权设备，并附上同一台手机上次授权给了谁
func (s *DeviceService) PendingWithHistory(ctx context.Context, adminUserID string) ([]PendingItem, error) {
	list, err := s.Pending(ctx, adminUserID)
	if err != nil {
		return nil, err
	}
	out := make([]PendingItem, 0, len(list))
	for _, d := range list {
		item := PendingItem{Device: d}
		if d.DeviceKey != "" {
			if prev, err := s.db.LastApprovedByKey(ctx, s.db.SQL(), d.DeviceKey, d.ID); err == nil && prev != nil {
				item.LastDeviceID = prev.ID
				item.LastUserID = prev.UserID
				item.LastFamilyID = prev.FamilyID
				item.LastRole = prev.Role
				if prev.ApprovedAt != nil {
					item.LastApprovedAt = prev.ApprovedAt
				}
				if u, err := s.db.UserByID(ctx, s.db.SQL(), prev.UserID); err == nil {
					item.LastAccount = u.Account
					item.LastNickname = u.Nickname
				}
			}
		}
		out = append(out, item)
	}
	return out, nil
}
