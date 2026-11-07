package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/pkg/authtoken"
	"family-ledger/server/internal/pkg/id"
)

type ctxKey string

const (
	ctxKeyRequestID ctxKey = "request_id"
	ctxKeyUserID    ctxKey = "user_id"
)

func requestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

func userIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyUserID).(string); ok {
		return v
	}
	return ""
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-Id")
		if rid == "" {
			rid = id.New()
		}
		w.Header().Set("X-Request-Id", rid)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID, rid)))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		slog.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"dur_ms", time.Since(start).Milliseconds(),
			"request_id", requestIDFrom(r.Context()),
		)
	})
}

func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic", "panic", rec, "path", r.URL.Path)
				writeError(w, r, domain.ErrInternal)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func withCORS(origins []string, next http.Handler) http.Handler {
	allowAll := false
	allowed := map[string]bool{}
	for _, o := range origins {
		if o == "*" {
			allowAll = true
		}
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (allowAll || allowed[origin]) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-Id")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAuth 校验 App 域令牌，并把用户 ID 放进上下文。
func (h *Handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, r, domain.ErrUnauthorized)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		claims, err := authtoken.Parse(h.cfg.JWTSecret, token, authtoken.ScopeApp)
		if err != nil {
			writeError(w, r, domain.ErrUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyUserID, claims.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.IndexByte(v, ','); i > 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	if v := r.Header.Get("X-Real-Ip"); v != "" {
		return v
	}
	if i := strings.LastIndexByte(r.RemoteAddr, ':'); i > 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}

// requireAdmin 校验管理后台令牌：scope 必须是 admin，且账号当前仍是平台管理员。
func (h *Handler) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, r, domain.ErrUnauthorized)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		claims, err := authtoken.Parse(h.cfg.JWTSecret, token, authtoken.ScopeAdmin)
		if err != nil {
			writeError(w, r, domain.ErrUnauthorized)
			return
		}
		user, err := h.admin.VerifyAdmin(r.Context(), claims.UserID)
		if err != nil {
			writeError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyUserID, user.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
