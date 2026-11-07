package httpapi

import (
	"net/http"

	"family-ledger/server/internal/service"
)

type splitLineReq struct {
	AmountCents int64  `json:"amount_cents"`
	Scope       string `json:"scope"`
	OwnerID     string `json:"owner_id"`
	CategoryID  string `json:"category_id"`
	Note        string `json:"note"`
}

type splitReq struct {
	Lines []splitLineReq `json:"lines"`
}

// splitTransaction 把一笔账目拆成多行
func (h *Handler) splitTransaction(w http.ResponseWriter, r *http.Request) {
	var req splitReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	lines := make([]service.SplitLine, 0, len(req.Lines))
	for _, l := range req.Lines {
		lines = append(lines, service.SplitLine{
			AmountCents: l.AmountCents, Scope: l.Scope, OwnerID: l.OwnerID,
			CategoryID: l.CategoryID, Note: l.Note,
		})
	}
	result, err := h.ledger.SplitTransaction(r.Context(), familyIDFrom(r.Context()), userIDFrom(r.Context()),
		r.PathValue("tid"), service.SplitInput{Lines: lines})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeCreated(w, r, result)
}

func (h *Handler) undoSplit(w http.ResponseWriter, r *http.Request) {
	err := h.ledger.UndoSplit(r.Context(), familyIDFrom(r.Context()), userIDFrom(r.Context()), r.PathValue("tid"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已撤销拆分")
}

func (h *Handler) listSplitChildren(w http.ResponseWriter, r *http.Request) {
	children, err := h.ledger.ChildTransactions(r.Context(), familyIDFrom(r.Context()), userIDFrom(r.Context()), r.PathValue("tid"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]txResp, 0, len(children))
	for _, t := range children {
		items = append(items, toTxResp(t))
	}
	writeData(w, r, map[string]any{"items": items})
}
