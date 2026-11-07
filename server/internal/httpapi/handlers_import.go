package httpapi

import (
	"io"
	"net/http"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/service"
)

// importBill 接收账单文件（multipart/form-data）：file + password
func (h *Handler) importBill(w http.ResponseWriter, r *http.Request) {
	// 32MB 文件 + 一些表单开销
	r.Body = http.MaxBytesReader(w, r.Body, 34<<20)
	if err := r.ParseMultipartForm(34 << 20); err != nil {
		writeError(w, r, domain.ErrBadRequest.WithMessage("上传内容无法解析（可能是文件过大）"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, domain.ErrBadRequest.WithMessage("缺少 file 字段"))
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, r, domain.ErrBadRequest.WithMessage("读取上传文件失败"))
		return
	}

	summary, err := h.ledger.ImportBill(
		r.Context(),
		familyIDFrom(r.Context()),
		userIDFrom(r.Context()),
		header.Filename,
		data,
		r.FormValue("password"),
	)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeCreated(w, r, summary)
}

func (h *Handler) listImports(w http.ResponseWriter, r *http.Request) {
	batches, err := h.ledger.ListImports(r.Context(), familyIDFrom(r.Context()), userIDFrom(r.Context()),
		parseInt(r.URL.Query().Get("limit"), 20))
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(batches))
	for _, b := range batches {
		items = append(items, map[string]any{
			"id":         b.ID,
			"source":     b.Source,
			"file_name":  b.FileName,
			"total":      b.Total,
			"imported":   b.Imported,
			"skipped":    b.Skipped,
			"failed":     b.Failed,
			"created_at": b.CreatedAt,
		})
	}
	writeData(w, r, map[string]any{"items": items})
}

type saveRuleReq struct {
	Keyword    string `json:"keyword"`
	CategoryID string `json:"category_id"`
}

func (h *Handler) saveCategoryRule(w http.ResponseWriter, r *http.Request) {
	var req saveRuleReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.ledger.SaveCategoryRule(r.Context(), familyIDFrom(r.Context()), userIDFrom(r.Context()),
		req.Keyword, req.CategoryID); err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "规则已保存")
}

// previewImport 解析分享进来的 CSV，返回预览（不入库）
func (h *Handler) previewImport(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 3<<20)
	if err := r.ParseMultipartForm(3 << 20); err != nil {
		writeError(w, r, domain.ErrBadRequest.WithMessage("上传内容无法解析"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, domain.ErrBadRequest.WithMessage("缺少 file 字段"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, r, domain.ErrBadRequest.WithMessage("读取文件失败"))
		return
	}
	result, err := h.ledger.PreviewImport(r.Context(), familyIDFrom(r.Context()), userIDFrom(r.Context()),
		header.Filename, data)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, result)
}

type confirmImportReq struct {
	Rows []service.DraftRow `json:"rows"`
}

// confirmImport 把预览记录写库
func (h *Handler) confirmImport(w http.ResponseWriter, r *http.Request) {
	var req confirmImportReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	summary, err := h.ledger.ConfirmImport(r.Context(), familyIDFrom(r.Context()), userIDFrom(r.Context()), req.Rows)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeCreated(w, r, summary)
}
