package httpapi

import (
	"net/http"

	"family-ledger/server/internal/domain"
)

type adminSessionResp struct {
	AccessToken string   `json:"access_token"`
	ExpiresIn   int64    `json:"expires_in"`
	User        userResp `json:"user"`
}

type adminLoginReq struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

func (h *Handler) adminLogin(w http.ResponseWriter, r *http.Request) {
	var req adminLoginReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	session, err := h.admin.Login(r.Context(), req.Account, req.Password, clientIP(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, adminSessionResp{
		AccessToken: session.AccessToken,
		ExpiresIn:   session.ExpiresIn,
		User: userResp{
			ID: session.UserID, Account: session.Account, Nickname: session.Nickname,
			Role: session.Role, IsPlatformAdmin: true,
		},
	})
}

func (h *Handler) adminMe(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r.Context())
	user, err := h.admin.VerifyAdmin(r.Context(), uid)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, map[string]any{"user": toUserResp(user, h.admin.RoleOf(r.Context(), uid))})
}

// adminOverview 单家庭概览：家庭信息 + 成员/账目/支出汇总。
func (h *Handler) adminOverview(w http.ResponseWriter, r *http.Request) {
	o, err := h.admin.Overview(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	var family any
	if o.Family != nil {
		family = toFamilyResp(o.Family, h.admin.RoleOf(r.Context(), userIDFrom(r.Context())), o.Members)
	}
	writeData(w, r, map[string]any{
		"family":              family,
		"members":             o.Members,
		"transactions":        o.Txs,
		"expense_total_cents": o.Amount,
		"new_tx_today":        o.NewTx,
		"new_users_today":     o.NewUsers,
	})
}

type adminMemberResp struct {
	ID        string `json:"id"`
	Account   string `json:"account"`
	Nickname  string `json:"nickname"`
	Role      string `json:"role"`
	Status    int    `json:"status"`
	JoinedAt  int64  `json:"joined_at"`
	CreatedAt int64  `json:"created_at"`
}

func (h *Handler) adminMembers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, total, err := h.admin.Members(r.Context(), q.Get("q"),
		parseInt(q.Get("page"), 1), parseInt(q.Get("page_size"), 20))
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]adminMemberResp, 0, len(rows))
	for _, m := range rows {
		items = append(items, adminMemberResp{
			ID: m.UserID, Account: m.Account, Nickname: m.Nickname, Role: m.Role,
			Status: m.Status, JoinedAt: m.JoinedAt, CreatedAt: m.CreatedAt,
		})
	}
	writeData(w, r, map[string]any{"items": items, "total": total})
}

type adminUpdateMemberReq struct {
	Role     *string `json:"role"`
	Status   *int    `json:"status"`
	Password string  `json:"password"`
}

// adminUpdateMember 一个接口处理改角色、封禁/解封、重置密码。
func (h *Handler) adminUpdateMember(w http.ResponseWriter, r *http.Request) {
	var req adminUpdateMemberReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if req.Role == nil && req.Status == nil && req.Password == "" {
		writeError(w, r, domain.ErrBadRequest.WithMessage("没有要修改的内容"))
		return
	}
	uid := r.PathValue("uid")
	actor := userIDFrom(r.Context())
	if req.Role != nil {
		if err := h.families.SetRole(r.Context(), actor, uid, *req.Role); err != nil {
			writeError(w, r, err)
			return
		}
	}
	if req.Status != nil {
		if err := h.admin.SetUserStatus(r.Context(), actor, uid, *req.Status, clientIP(r)); err != nil {
			writeError(w, r, err)
			return
		}
	}
	if req.Password != "" {
		if err := h.admin.ResetPassword(r.Context(), actor, uid, req.Password, clientIP(r)); err != nil {
			writeError(w, r, err)
			return
		}
	}
	writeMessage(w, r, "已更新")
}

type adminTxResp struct {
	txResp
	CategoryName string `json:"category_name"`
	MemberName   string `json:"member_name"`
}

func (h *Handler) adminTransactions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, total, err := h.admin.Transactions(r.Context(),
		parseInt(q.Get("page"), 1), parseInt(q.Get("page_size"), 50))
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]adminTxResp, 0, len(rows))
	for _, t := range rows {
		items = append(items, adminTxResp{
			txResp:       toTxResp(t.Transaction),
			CategoryName: t.CategoryName,
			MemberName:   t.MemberName,
		})
	}
	writeData(w, r, map[string]any{"items": items, "total": total})
}

type adminAuditResp struct {
	ID         string `json:"id"`
	Action     string `json:"action"`
	UserID     string `json:"user_id"`
	Nickname   string `json:"nickname"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Detail     string `json:"detail"`
	IP         string `json:"ip"`
	CreatedAt  int64  `json:"created_at"`
}

func (h *Handler) adminAuditLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, total, err := h.admin.AuditLogs(r.Context(), q.Get("action"),
		parseInt(q.Get("page"), 1), parseInt(q.Get("page_size"), 30))
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]adminAuditResp, 0, len(rows))
	for _, a := range rows {
		items = append(items, adminAuditResp{
			ID: a.ID, Action: a.Action, UserID: a.UserID, Nickname: a.Nickname,
			TargetType: a.TargetType, TargetID: a.TargetID,
			Detail: a.Detail, IP: a.IP, CreatedAt: a.CreatedAt,
		})
	}
	writeData(w, r, map[string]any{"items": items, "total": total})
}
