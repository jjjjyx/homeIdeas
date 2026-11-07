package httpapi

import (
	"net/http"
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
			ID: session.UserID, Account: session.Account,
			Nickname: session.Nickname, IsPlatformAdmin: true,
		},
	})
}

func (h *Handler) adminMe(w http.ResponseWriter, r *http.Request) {
	user, err := h.admin.VerifyAdmin(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, map[string]any{"user": toUserResp(user)})
}

func (h *Handler) adminOverview(w http.ResponseWriter, r *http.Request) {
	o, err := h.admin.Overview(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, map[string]any{
		"users":               o.Users,
		"families":            o.Families,
		"transactions":        o.Transactions,
		"expense_total_cents": o.AmountCents,
		"new_users_today":     o.NewUsersToday,
		"new_tx_today":        o.NewTxToday,
	})
}

type adminUserResp struct {
	ID          string `json:"id"`
	Account     string `json:"account"`
	Nickname    string `json:"nickname"`
	Status      int    `json:"status"`
	IsAdmin     bool   `json:"is_platform_admin"`
	FamilyCount int    `json:"family_count"`
	CreatedAt   int64  `json:"created_at"`
}

func (h *Handler) adminUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, total, err := h.admin.Users(r.Context(), q.Get("q"),
		parseInt(q.Get("page"), 1), parseInt(q.Get("page_size"), 20))
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]adminUserResp, 0, len(rows))
	for _, u := range rows {
		items = append(items, adminUserResp{
			ID: u.ID, Account: u.Account, Nickname: u.Nickname, Status: u.Status,
			IsAdmin: u.IsPlatformAdmin, FamilyCount: u.FamilyCount, CreatedAt: u.CreatedAt,
		})
	}
	writeData(w, r, map[string]any{"items": items, "total": total})
}

type adminSetStatusReq struct {
	Status int `json:"status"`
}

func (h *Handler) adminSetUserStatus(w http.ResponseWriter, r *http.Request) {
	var req adminSetStatusReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.admin.SetUserStatus(r.Context(), userIDFrom(r.Context()), r.PathValue("uid"), req.Status, clientIP(r)); err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已更新")
}

type adminFamilyResp struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	OwnerID          string `json:"owner_id"`
	OwnerNickname    string `json:"owner_nickname"`
	MemberCount      int    `json:"member_count"`
	TransactionCount int    `json:"transaction_count"`
	Rev              int64  `json:"rev"`
	CreatedAt        int64  `json:"created_at"`
}

func (h *Handler) adminFamilies(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, total, err := h.admin.Families(r.Context(), q.Get("q"),
		parseInt(q.Get("page"), 1), parseInt(q.Get("page_size"), 20))
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]adminFamilyResp, 0, len(rows))
	for _, f := range rows {
		items = append(items, adminFamilyResp{
			ID: f.ID, Name: f.Name, OwnerID: f.OwnerID, OwnerNickname: f.OwnerNickname,
			MemberCount: f.MemberCount, TransactionCount: f.TransactionCount,
			Rev: f.Rev, CreatedAt: f.CreatedAt,
		})
	}
	writeData(w, r, map[string]any{"items": items, "total": total})
}

type adminTxResp struct {
	txResp
	CategoryName string `json:"category_name"`
	MemberName   string `json:"member_name"`
}

func (h *Handler) adminFamilyTransactions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, err := h.admin.FamilyTransactions(r.Context(), r.PathValue("id"),
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
	writeData(w, r, map[string]any{"items": items})
}

type adminAuditResp struct {
	ID         string `json:"id"`
	Action     string `json:"action"`
	UserID     string `json:"user_id"`
	Nickname   string `json:"nickname"`
	FamilyID   string `json:"family_id"`
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
			FamilyID: a.FamilyID, TargetType: a.TargetType, TargetID: a.TargetID,
			Detail: a.Detail, IP: a.IP, CreatedAt: a.CreatedAt,
		})
	}
	writeData(w, r, map[string]any{"items": items, "total": total})
}
