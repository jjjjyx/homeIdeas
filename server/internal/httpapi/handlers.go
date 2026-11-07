package httpapi

import (
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

type inviteResp struct {
	Code      string `json:"code"`
	Role      string `json:"role"`
	ExpiresAt *int64 `json:"expires_at"`
}

func toUserResp(u *domain.User) userResp {
	return userResp{
		ID:              u.ID,
		Account:         u.Account,
		Nickname:        u.Nickname,
		AvatarURL:       u.AvatarURL,
		IsPlatformAdmin: u.IsPlatformAdmin,
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
			IsPlatformAdmin: s.IsAdmin,
		},
	}
}

func toFamilyResp(c store.FamilyCard) familyResp {
	return familyResp{
		ID:          c.ID,
		Name:        c.Name,
		OwnerID:     c.OwnerID,
		MyRole:      c.Role,
		MemberCount: c.MemberCount,
		Rev:         c.Rev,
		CreatedAt:   c.CreatedAt,
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
	Device     string `json:"device"`
}

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

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r.Context())
	user, err := h.auth.UserByID(r.Context(), uid)
	if err != nil {
		writeError(w, r, err)
		return
	}
	families, err := h.families.List(r.Context(), uid)
	if err != nil {
		writeError(w, r, err)
		return
	}
	cards := make([]familyResp, 0, len(families))
	for _, c := range families {
		cards = append(cards, toFamilyResp(c))
	}
	writeData(w, r, map[string]any{"user": toUserResp(user), "families": cards})
}

// ---------- 家庭 ----------

type createFamilyReq struct {
	Name string `json:"name"`
}

func (h *Handler) createFamily(w http.ResponseWriter, r *http.Request) {
	var req createFamilyReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	card, err := h.families.Create(r.Context(), userIDFrom(r.Context()), req.Name, clientIP(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeCreated(w, r, toFamilyResp(*card))
}

func (h *Handler) listFamilies(w http.ResponseWriter, r *http.Request) {
	cards, err := h.families.List(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]familyResp, 0, len(cards))
	for _, c := range cards {
		out = append(out, toFamilyResp(c))
	}
	writeData(w, r, map[string]any{"items": out})
}

func (h *Handler) getFamily(w http.ResponseWriter, r *http.Request) {
	detail, err := h.families.Detail(r.Context(), userIDFrom(r.Context()), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	members := make([]memberResp, 0, len(detail.Members))
	for _, m := range detail.Members {
		members = append(members, toMemberResp(m))
	}
	writeData(w, r, map[string]any{
		"family": familyResp{
			ID:          detail.Family.ID,
			Name:        detail.Family.Name,
			OwnerID:     detail.Family.OwnerID,
			MyRole:      detail.MyRole,
			MemberCount: len(detail.Members),
			Rev:         detail.Family.Rev,
			CreatedAt:   detail.Family.CreatedAt,
		},
		"members": members,
	})
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
	if err := h.families.Rename(r.Context(), userIDFrom(r.Context()), r.PathValue("id"), req.Name); err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已修改")
}

type createInviteReq struct {
	Role           string `json:"role"`
	ExpiresInHours int    `json:"expires_in_hours"`
}

func (h *Handler) createInvite(w http.ResponseWriter, r *http.Request) {
	var req createInviteReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	invite, err := h.families.CreateInvite(r.Context(), userIDFrom(r.Context()),
		r.PathValue("id"), req.Role, req.ExpiresInHours, clientIP(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeCreated(w, r, inviteResp{Code: invite.Code, Role: invite.Role, ExpiresAt: invite.ExpiresAt})
}

type joinFamilyReq struct {
	Code string `json:"code"`
}

func (h *Handler) joinFamily(w http.ResponseWriter, r *http.Request) {
	var req joinFamilyReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	card, err := h.families.Join(r.Context(), userIDFrom(r.Context()), req.Code, clientIP(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, toFamilyResp(*card))
}

func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) {
	members, err := h.families.Members(r.Context(), userIDFrom(r.Context()), r.PathValue("id"))
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
		r.PathValue("id"), r.PathValue("uid"), req.Role); err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已更新角色")
}

func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) {
	if err := h.families.RemoveMember(r.Context(), userIDFrom(r.Context()),
		r.PathValue("id"), r.PathValue("uid")); err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已移除成员")
}

func (h *Handler) leaveFamily(w http.ResponseWriter, r *http.Request) {
	if err := h.families.Leave(r.Context(), userIDFrom(r.Context()), r.PathValue("id")); err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已退出家庭")
}
