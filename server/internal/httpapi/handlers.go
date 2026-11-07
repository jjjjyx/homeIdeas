package httpapi

import (
	"errors"
	"net/http"

	"family-ledger/server/internal/config"
	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/service"
	"family-ledger/server/internal/store"
)

type Handler struct {
	cfg      *config.Config
	db       *store.DB
	auth     *service.AuthService
	families *service.FamilyService
	ledger   *service.LedgerService
	admin    *service.AdminService
	mail     *service.MailService
	classify *service.ClassifyService
	devices  *service.DeviceService
}

func NewHandler(cfg *config.Config, db *store.DB, auth *service.AuthService,
	families *service.FamilyService, ledger *service.LedgerService,
	admin *service.AdminService, mail *service.MailService, classify *service.ClassifyService,
	devices *service.DeviceService) *Handler {
	return &Handler{cfg: cfg, db: db, auth: auth, families: families,
		ledger: ledger, admin: admin, mail: mail, classify: classify, devices: devices}
}

// ---------- DTO ----------

type userResp struct {
	ID              string `json:"id"`
	Account         string `json:"account"`
	Nickname        string `json:"nickname"`
	AvatarURL       string `json:"avatar_url"`
	Role            string `json:"role"`
	IsPlatformAdmin bool   `json:"is_platform_admin"`
}

type sessionResp struct {
	AccessToken  string   `json:"access_token"`
	ExpiresIn    int64    `json:"expires_in"`
	RefreshToken string   `json:"refresh_token"`
	User         userResp `json:"user"`
}

type familyResp struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	OwnerID     string `json:"owner_id"`
	MyRole      string `json:"my_role"`
	MemberCount int    `json:"member_count"`
	Rev         int64  `json:"rev"`
	CreatedAt   int64  `json:"created_at"`
}

type memberResp struct {
	UserID   string `json:"user_id"`
	Nickname string `json:"nickname"`
	Account  string `json:"account"`
	Role     string `json:"role"`
	JoinedAt int64  `json:"joined_at"`
}

func toUserResp(u *domain.User, role string) userResp {
	return userResp{
		ID:              u.ID,
		Account:         u.Account,
		Nickname:        u.Nickname,
		AvatarURL:       u.AvatarURL,
		Role:            role,
		IsPlatformAdmin: role == domain.RoleOwner || role == domain.RoleAdmin,
	}
}

func toSessionResp(s *service.Session) sessionResp {
	return sessionResp{
		AccessToken:  s.AccessToken,
		ExpiresIn:    s.ExpiresIn,
		RefreshToken: s.RefreshToken,
		User: userResp{
			ID:              s.UserID,
			Account:         s.Account,
			Nickname:        s.Nickname,
			Role:            s.Role,
			IsPlatformAdmin: s.IsAdmin,
		},
	}
}

func toFamilyResp(f *domain.Family, myRole string, memberCount int) familyResp {
	return familyResp{
		ID:          f.ID,
		Name:        f.Name,
		OwnerID:     f.OwnerID,
		MyRole:      myRole,
		MemberCount: memberCount,
		Rev:         f.Rev,
		CreatedAt:   f.CreatedAt,
	}
}

func toMemberResp(m domain.Member) memberResp {
	return memberResp{
		UserID:   m.UserID,
		Nickname: m.Nickname,
		Account:  m.Account,
		Role:     m.Role,
		JoinedAt: m.JoinedAt,
	}
}

// ---------- 健康检查 ----------

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeData(w, r, map[string]any{"status": "ok"})
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	if err := h.db.Ping(r.Context()); err != nil {
		writeError(w, r, domain.New(5001, http.StatusServiceUnavailable, "数据库不可用"))
		return
	}
	writeData(w, r, map[string]any{"status": "ready"})
}

// ---------- 认证 ----------

type registerReq struct {
	Account    string `json:"account"`
	Password   string `json:"password"`
	Nickname   string `json:"nickname"`
	InviteCode string `json:"invite_code"`
	FamilyName string `json:"family_name"`
	Device     string `json:"device"`
}

// register 只用于系统首次初始化：创建唯一家庭和户主账号。
func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	session, err := h.auth.Register(r.Context(), service.RegisterInput{
		Account:    req.Account,
		Password:   req.Password,
		Nickname:   req.Nickname,
		InviteCode: req.InviteCode,
		FamilyName: req.FamilyName,
		Device:     req.Device,
		IP:         clientIP(r),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeCreated(w, r, toSessionResp(session))
}

type loginReq struct {
	Account  string `json:"account"`
	Password string `json:"password"`
	Device   string `json:"device"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	session, err := h.auth.Login(r.Context(), service.LoginInput{
		Account:  req.Account,
		Password: req.Password,
		Device:   req.Device,
		IP:       clientIP(r),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, toSessionResp(session))
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	session, err := h.auth.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, toSessionResp(session))
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var req refreshReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.auth.Logout(r.Context(), req.RefreshToken); err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已退出登录")
}

// me 返回当前用户 + 唯一家庭。系统未初始化时 family 为 null。
func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r.Context())
	user, err := h.auth.UserByID(r.Context(), uid)
	if err != nil {
		writeError(w, r, err)
		return
	}
	detail, err := h.families.Detail(r.Context(), uid)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrForbidden) {
			writeData(w, r, map[string]any{"user": toUserResp(user, ""), "family": nil})
			return
		}
		writeError(w, r, err)
		return
	}
	members := make([]memberResp, 0, len(detail.Members))
	for _, m := range detail.Members {
		members = append(members, toMemberResp(m))
	}
	writeData(w, r, map[string]any{
		"user":    toUserResp(user, detail.MyRole),
		"family":  toFamilyResp(detail.Family, detail.MyRole, len(detail.Members)),
		"members": members,
	})
}

// ---------- 家庭信息与成员 ----------

func (h *Handler) getFamily(w http.ResponseWriter, r *http.Request) {
	detail, err := h.families.Detail(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	members := make([]memberResp, 0, len(detail.Members))
	for _, m := range detail.Members {
		members = append(members, toMemberResp(m))
	}
	writeData(w, r, map[string]any{
		"family":  toFamilyResp(detail.Family, detail.MyRole, len(detail.Members)),
		"members": members,
	})
}

type inviteResp struct {
	Code      string `json:"code"`
	Role      string `json:"role"`
	ExpiresAt *int64 `json:"expires_at"`
}

type createInviteReq struct {
	Role           string `json:"role"`
	ExpiresInHours int    `json:"expires_in_hours"`
}

// createInvite 生成加入唯一家庭的邀请码（户主/管理员）。
func (h *Handler) createInvite(w http.ResponseWriter, r *http.Request) {
	var req createInviteReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	invite, err := h.families.CreateInvite(r.Context(), userIDFrom(r.Context()),
		req.Role, req.ExpiresInHours, clientIP(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeCreated(w, r, inviteResp{Code: invite.Code, Role: invite.Role, ExpiresAt: invite.ExpiresAt})
}

type renameFamilyReq struct {
	Name string `json:"name"`
}

func (h *Handler) renameFamily(w http.ResponseWriter, r *http.Request) {
	var req renameFamilyReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.families.Rename(r.Context(), userIDFrom(r.Context()), req.Name); err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已修改")
}

func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) {
	members, err := h.families.Members(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]memberResp, 0, len(members))
	for _, m := range members {
		out = append(out, toMemberResp(m))
	}
	writeData(w, r, map[string]any{"items": out})
}

type addMemberReq struct {
	Account  string `json:"account"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
	Role     string `json:"role"`
}

// addMember 户主/管理员直接创建成员账号（单家庭模式不再需要邀请码）。
func (h *Handler) addMember(w http.ResponseWriter, r *http.Request) {
	var req addMemberReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	member, err := h.families.AddMember(r.Context(), userIDFrom(r.Context()),
		req.Account, req.Password, req.Nickname, req.Role)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeCreated(w, r, toMemberResp(*member))
}

type setRoleReq struct {
	Role string `json:"role"`
}

func (h *Handler) setRole(w http.ResponseWriter, r *http.Request) {
	var req setRoleReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.families.SetRole(r.Context(), userIDFrom(r.Context()),
		r.PathValue("uid"), req.Role); err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已更新角色")
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	if err := h.families.RemoveMember(r.Context(), userIDFrom(r.Context()),
		r.PathValue("uid")); err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已移除成员")
}
