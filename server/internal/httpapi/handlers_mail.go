package httpapi

import (
	"net/http"

	"family-ledger/server/internal/service"
)

type mailConfigReq struct {
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	UseTLS          bool   `json:"use_tls"`
	Enabled         bool   `json:"enabled"`
	ExtractPassword string `json:"extract_password"`
	ResetCursor     bool   `json:"reset_cursor"`
}

func (r mailConfigReq) toInput() service.MailConfigInput {
	return service.MailConfigInput{
		Host: r.Host, Port: r.Port, Username: r.Username, Password: r.Password,
		UseTLS: r.UseTLS, Enabled: r.Enabled, ExtractPassword: r.ExtractPassword,
		ResetCursor: r.ResetCursor,
	}
}

func (h *Handler) getMailConfig(w http.ResponseWriter, r *http.Request) {
	status, err := h.mail.Status(r.Context(), familyIDFrom(r.Context()), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, status)
}

func (h *Handler) saveMailConfig(w http.ResponseWriter, r *http.Request) {
	var req mailConfigReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.mail.SaveConfig(r.Context(), familyIDFrom(r.Context()), userIDFrom(r.Context()), req.toInput()); err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已保存")
}

func (h *Handler) testMailConfig(w http.ResponseWriter, r *http.Request) {
	var req mailConfigReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := h.mail.TestConnection(r.Context(), familyIDFrom(r.Context()), userIDFrom(r.Context()), req.toInput()); err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "连接成功")
}

func (h *Handler) syncMail(w http.ResponseWriter, r *http.Request) {
	result, err := h.mail.SyncFamily(r.Context(), familyIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, result)
}

type mailPasswordReq struct {
	// MessageID 为空时作为默认密码重试所有待填邮件；指定时只重试这一封
	MessageID string `json:"message_id"`
	Password  string `json:"password"`
}

func (h *Handler) submitMailPassword(w http.ResponseWriter, r *http.Request) {
	var req mailPasswordReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := h.mail.SubmitPassword(r.Context(), familyIDFrom(r.Context()), userIDFrom(r.Context()), req.MessageID, req.Password)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, result)
}
