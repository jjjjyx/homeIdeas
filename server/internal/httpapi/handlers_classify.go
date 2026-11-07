package httpapi

import (
	"net/http"

	"family-ledger/server/internal/service"
)

// runClassify 触发一次 AI 分类
func (h *Handler) runClassify(w http.ResponseWriter, r *http.Request) {
	result, err := h.classify.Run(r.Context(), familyIDFrom(r.Context()), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, result)
}

// classifyPending 待确认列表（按商户聚合）
func (h *Handler) classifyPending(w http.ResponseWriter, r *http.Request) {
	data, err := h.classify.Pending(r.Context(), familyIDFrom(r.Context()), userIDFrom(r.Context()),
		parseFloat(r.URL.Query().Get("min_confidence"), 0.7))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, data)
}

type reviewReq struct {
	Scope      string `json:"scope"`
	OwnerID    string `json:"owner_id"`
	CategoryID string `json:"category_id"`
	Remember   bool   `json:"remember"`
}

// reviewTransaction 人工修正一条账目的归属与分类
func (h *Handler) reviewTransaction(w http.ResponseWriter, r *http.Request) {
	var req reviewReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	err := h.classify.Review(r.Context(), familyIDFrom(r.Context()), userIDFrom(r.Context()),
		r.PathValue("tid"), service.ReviewInput{
			Scope: req.Scope, OwnerID: req.OwnerID, CategoryID: req.CategoryID, Remember: req.Remember,
		})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已更新")
}
