package httpapi

import (
	"fmt"
	"net/http"
	"time"
)

// exportCSV 直接返回文件流，不走 JSON 信封。
func (h *Handler) exportCSV(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	data, err := h.ledger.ExportCSV(
		r.Context(),
		familyIDFrom(r.Context()),
		userIDFrom(r.Context()),
		parseMS(q.Get("from")),
		parseMS(q.Get("to")),
	)
	if err != nil {
		writeError(w, r, err)
		return
	}

	filename := fmt.Sprintf("family-ledger-%s.csv", time.Now().Format("20060102"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
