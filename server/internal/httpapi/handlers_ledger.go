package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/service"
)

type txResp struct {
	ID          string `json:"id"`
	FamilyID    string `json:"family_id"`
	Type        int    `json:"type"`
	AmountCents int64  `json:"amount_cents"`
	CategoryID  string `json:"category_id"`
	AccountID   string `json:"account_id,omitempty"`
	MemberID    string `json:"member_id"`
	OccurredAt  int64  `json:"occurred_at"`
	Note        string `json:"note"`
	Visibility  string `json:"visibility"`
	Tags        string `json:"tags,omitempty"`
	Rev         int64  `json:"rev"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	DeletedAt   *int64 `json:"deleted_at,omitempty"`
	Source      string `json:"source,omitempty"`
	ExternalID  string `json:"external_id,omitempty"`

	Scope          string  `json:"scope"`
	OwnerID        string  `json:"owner_id,omitempty"`
	CategorySource string  `json:"category_source,omitempty"`
	AIConfidence   float64 `json:"ai_confidence,omitempty"`
	AIReason       string  `json:"ai_reason,omitempty"`
	Reviewed       bool    `json:"reviewed"`

	ParentID string `json:"parent_id,omitempty"`
	IsSplit  bool   `json:"is_split,omitempty"`
}

func toTxResp(t domain.Transaction) txResp {
	return txResp{
		ID: t.ID, FamilyID: t.FamilyID, Type: t.Type, AmountCents: t.AmountCents,
		CategoryID: t.CategoryID, AccountID: t.AccountID, MemberID: t.MemberID,
		OccurredAt: t.OccurredAt, Note: t.Note, Visibility: t.Visibility,
		Tags: t.Tags, Rev: t.Rev, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
		DeletedAt: t.DeletedAt, Source: t.Source, ExternalID: t.ExternalID,
		Scope: t.Scope, OwnerID: t.OwnerID, CategorySource: t.CategorySource,
		AIConfidence: t.AIConfidence, AIReason: t.AIReason, Reviewed: t.Reviewed,
		ParentID: t.ParentID, IsSplit: t.IsSplit,
	}
}

type categorySyncResp struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      int    `json:"type"`
	IconKey   string `json:"icon_key"`
	Color     int64  `json:"color"`
	SortOrder int    `json:"sort_order"`
	Rev       int64  `json:"rev"`
	UpdatedAt int64  `json:"updated_at"`
	DeletedAt *int64 `json:"deleted_at,omitempty"`
}

type accountSyncResp struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	SortOrder int    `json:"sort_order"`
	Rev       int64  `json:"rev"`
	UpdatedAt int64  `json:"updated_at"`
	DeletedAt *int64 `json:"deleted_at,omitempty"`
}

type txInputReq struct {
	ID          string `json:"id"`
	Type        int    `json:"type"`
	AmountCents int64  `json:"amount_cents"`
	CategoryID  string `json:"category_id"`
	AccountID   string `json:"account_id"`
	OccurredAt  int64  `json:"occurred_at"`
	Note        string `json:"note"`
	Visibility  string `json:"visibility"`
	Tags        string `json:"tags"`
}

func (r txInputReq) toInput() service.TxInput {
	return service.TxInput{
		ID: r.ID, Type: r.Type, AmountCents: r.AmountCents, CategoryID: r.CategoryID,
		AccountID: r.AccountID, OccurredAt: r.OccurredAt, Note: r.Note,
		Visibility: r.Visibility, Tags: r.Tags,
	}
}

// ---------- 账目 CRUD ----------

func (h *Handler) createTransaction(w http.ResponseWriter, r *http.Request) {
	var req txInputReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	tx, err := h.ledger.Create(r.Context(), r.PathValue("id"), userIDFrom(r.Context()), req.toInput())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeCreated(w, r, toTxResp(*tx))
}

func (h *Handler) listTransactions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opt := service.ListOptions{
		FromMS:   parseMS(q.Get("from")),
		ToMS:     parseMS(q.Get("to")),
		Category: q.Get("category_id"),
		MemberID: q.Get("member_id"),
		Limit:    parseInt(q.Get("limit"), 0),
		Offset:   parseInt(q.Get("offset"), 0),
	}
	if v := q.Get("type"); v != "" {
		t := parseInt(v, -1)
		if t == domain.TypeExpense || t == domain.TypeIncome {
			opt.Type = &t
		}
	}
	list, err := h.ledger.List(r.Context(), r.PathValue("id"), userIDFrom(r.Context()), opt)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := make([]txResp, 0, len(list))
	for _, t := range list {
		out = append(out, toTxResp(t))
	}
	writeData(w, r, map[string]any{"items": out})
}

func (h *Handler) updateTransaction(w http.ResponseWriter, r *http.Request) {
	var req txInputReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	tx, err := h.ledger.Update(r.Context(), r.PathValue("id"), userIDFrom(r.Context()), r.PathValue("tid"), req.toInput())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, toTxResp(*tx))
}

func (h *Handler) deleteTransaction(w http.ResponseWriter, r *http.Request) {
	if err := h.ledger.Delete(r.Context(), r.PathValue("id"), userIDFrom(r.Context()), r.PathValue("tid")); err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已删除")
}

// ---------- 增量同步 ----------

func (h *Handler) syncPull(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	since := int64(parseInt(q.Get("since"), 0))
	limit := parseInt(q.Get("limit"), 500)

	res, err := h.ledger.Pull(r.Context(), r.PathValue("id"), userIDFrom(r.Context()), since, limit)
	if err != nil {
		writeError(w, r, err)
		return
	}

	categories := make([]categorySyncResp, 0, len(res.Categories))
	for _, c := range res.Categories {
		categories = append(categories, categorySyncResp{
			ID: c.ID, Name: c.Name, Type: c.Type, IconKey: c.IconKey, Color: c.Color,
			SortOrder: c.SortOrder, Rev: c.Rev, UpdatedAt: c.UpdatedAt, DeletedAt: c.DeletedAt,
		})
	}
	accounts := make([]accountSyncResp, 0, len(res.Accounts))
	for _, a := range res.Accounts {
		accounts = append(accounts, accountSyncResp{
			ID: a.ID, Name: a.Name, Kind: a.Kind, SortOrder: a.SortOrder,
			Rev: a.Rev, UpdatedAt: a.UpdatedAt, DeletedAt: a.DeletedAt,
		})
	}
	transactions := make([]txResp, 0, len(res.Transactions))
	for _, t := range res.Transactions {
		transactions = append(transactions, toTxResp(t))
	}
	members := make([]memberResp, 0, len(res.Members))
	for _, m := range res.Members {
		members = append(members, toMemberResp(m))
	}

	writeData(w, r, map[string]any{
		"cursor":   res.Cursor,
		"has_more": res.HasMore,
		"changes": map[string]any{
			"categories":   categories,
			"accounts":     accounts,
			"transactions": transactions,
			"members":      members,
		},
	})
}

type syncOpReq struct {
	ClientOpID string     `json:"client_op_id"`
	Entity     string     `json:"entity"`
	Op         string     `json:"op"`
	BaseRev    int64      `json:"base_rev"`
	Payload    txInputReq `json:"payload"`
}

type syncPushReq struct {
	Ops []syncOpReq `json:"ops"`
}

type syncResultResp struct {
	ClientOpID string  `json:"client_op_id"`
	Status     string  `json:"status"`
	Rev        int64   `json:"rev,omitempty"`
	Reason     string  `json:"reason,omitempty"`
	Server     *txResp `json:"server,omitempty"`
}

func (h *Handler) syncPush(w http.ResponseWriter, r *http.Request) {
	var req syncPushReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	ops := make([]service.SyncOpInput, 0, len(req.Ops))
	for _, op := range req.Ops {
		ops = append(ops, service.SyncOpInput{
			ClientOpID: op.ClientOpID,
			Entity:     op.Entity,
			Op:         op.Op,
			BaseRev:    op.BaseRev,
			Payload:    op.Payload.toInput(),
		})
	}
	results, err := h.ledger.Push(r.Context(), r.PathValue("id"), userIDFrom(r.Context()), ops)
	if err != nil {
		writeError(w, r, err)
		return
	}

	out := make([]syncResultResp, 0, len(results))
	for _, res := range results {
		item := syncResultResp{
			ClientOpID: res.ClientOpID,
			Status:     res.Status,
			Rev:        res.Rev,
			Reason:     res.Reason,
		}
		if res.Server != nil {
			s := toTxResp(*res.Server)
			item.Server = &s
		}
		out = append(out, item)
	}

	// 推送后直接给出最新游标，客户端可以省一次 pull
	pull, err := h.ledger.Pull(r.Context(), r.PathValue("id"), userIDFrom(r.Context()), 0, 1)
	cursor := int64(0)
	if err == nil {
		cursor = pull.Cursor
	}
	writeData(w, r, map[string]any{"cursor": cursor, "results": out})
}

// ---------- 统计 ----------

func (h *Handler) statsSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.ledger.Summary(r.Context(), r.PathValue("id"), userIDFrom(r.Context()), r.URL.Query().Get("month"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, map[string]any{
		"month":         summary.Month,
		"income_cents":  summary.IncomeCents,
		"expense_cents": summary.ExpenseCents,
		"balance_cents": summary.IncomeCents - summary.ExpenseCents,
		"count":         summary.Count,
	})
}

func (h *Handler) statsCategories(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	typ := parseInt(q.Get("type"), domain.TypeExpense)
	stats, err := h.ledger.CategoryStats(r.Context(), r.PathValue("id"), userIDFrom(r.Context()), q.Get("month"), typ)
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(stats))
	var total int64
	for _, s := range stats {
		total += s.AmountCents
		items = append(items, map[string]any{
			"category_id":  s.CategoryID,
			"name":         s.Name,
			"color":        s.Color,
			"icon_key":     s.IconKey,
			"amount_cents": s.AmountCents,
			"count":        s.Count,
			"ratio":        s.Ratio,
		})
	}
	writeData(w, r, map[string]any{"total_cents": total, "items": items})
}

func (h *Handler) statsTrend(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	months := parseInt(q.Get("months"), 6)
	typ := parseInt(q.Get("type"), domain.TypeExpense)
	points, err := h.ledger.Trend(r.Context(), r.PathValue("id"), userIDFrom(r.Context()), months, typ)
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(points))
	for _, p := range points {
		items = append(items, map[string]any{"month": p.Month, "amount_cents": p.AmountCents})
	}
	writeData(w, r, map[string]any{"items": items})
}

// ---------- 参数解析小工具 ----------

func parseInt(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

// parseMS 支持两种写法：Unix 毫秒数字，或 2006-01-02 / 2006-01 这样的日期。
func parseMS(s string) int64 {
	if s == "" {
		return 0
	}
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}
	for _, layout := range []string{"2006-01-02", "2006-01"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

func (h *Handler) statsScopes(w http.ResponseWriter, r *http.Request) {
	data, err := h.ledger.ScopeBreakdown(r.Context(), r.PathValue("id"), userIDFrom(r.Context()),
		r.URL.Query().Get("month"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, data)
}
