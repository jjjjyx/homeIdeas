package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"family-ledger/server/internal/domain"
)

type envelope struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Data      any    `json:"data,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func writeData(w http.ResponseWriter, r *http.Request, data any) {
	writeEnvelope(w, r, http.StatusOK, 0, "ok", data)
}

func writeCreated(w http.ResponseWriter, r *http.Request, data any) {
	writeEnvelope(w, r, http.StatusCreated, 0, "ok", data)
}

func writeMessage(w http.ResponseWriter, r *http.Request, message string) {
	writeEnvelope(w, r, http.StatusOK, 0, message, nil)
}

func writeEnvelope(w http.ResponseWriter, r *http.Request, status, code int, message string, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{
		Code:      code,
		Message:   message,
		Data:      data,
		RequestID: requestIDFrom(r.Context()),
	})
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		slog.Error("未处理的错误", "err", err, "path", r.URL.Path, "request_id", requestIDFrom(r.Context()))
		appErr = domain.ErrInternal
	}
	writeEnvelope(w, r, appErr.Status, appErr.Code, appErr.Message, nil)
}

// decodeJSON 限制请求体大小，避免超大 payload。
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return domain.ErrBadRequest.WithMessage("请求体不是合法的 JSON")
	}
	return nil
}

func parseFloat(s string, fallback float64) float64 {
	if s == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fallback
	}
	return v
}
