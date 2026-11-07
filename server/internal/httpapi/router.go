package httpapi

import (
	"net/http"

	"family-ledger/server/internal/config"
	"family-ledger/server/internal/domain"
)

// NewRouter 组装路由。鉴权中间件只包在需要登录的接口上。
func NewRouter(cfg *config.Config, h *Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET /readyz", h.ready)

	// 公开接口
	mux.HandleFunc("POST /api/v1/auth/register", h.register)
	mux.HandleFunc("POST /api/v1/auth/login", h.login)
	// 设备配对：新设备在这里发起，管理员批准后 App 轮询拿令牌
	mux.HandleFunc("POST /api/v1/auth/device/start", h.deviceStart)
	mux.HandleFunc("POST /api/v1/auth/device/poll", h.devicePoll)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", h.logout)

	// 需要登录
	auth := h.requireAuth
	mux.Handle("GET /api/v1/me", auth(http.HandlerFunc(h.me)))

	mux.Handle("GET /api/v1/families", auth(http.HandlerFunc(h.listFamilies)))
	mux.Handle("POST /api/v1/families", auth(http.HandlerFunc(h.createFamily)))
	mux.Handle("POST /api/v1/families/join", auth(http.HandlerFunc(h.joinFamily)))
	mux.Handle("GET /api/v1/families/{id}", auth(http.HandlerFunc(h.getFamily)))
	mux.Handle("PATCH /api/v1/families/{id}", auth(http.HandlerFunc(h.renameFamily)))
	mux.Handle("POST /api/v1/families/{id}/invites", auth(http.HandlerFunc(h.createInvite)))
	mux.Handle("GET /api/v1/families/{id}/members", auth(http.HandlerFunc(h.listMembers)))
	mux.Handle("PATCH /api/v1/families/{id}/members/{uid}", auth(http.HandlerFunc(h.setRole)))
	mux.Handle("DELETE /api/v1/families/{id}/members/{uid}", auth(http.HandlerFunc(h.removeMember)))
	mux.Handle("POST /api/v1/families/{id}/leave", auth(http.HandlerFunc(h.leaveFamily)))

	// 账目
	mux.Handle("GET /api/v1/families/{id}/transactions", auth(http.HandlerFunc(h.listTransactions)))
	mux.Handle("POST /api/v1/families/{id}/transactions", auth(http.HandlerFunc(h.createTransaction)))
	mux.Handle("PATCH /api/v1/families/{id}/transactions/{tid}", auth(http.HandlerFunc(h.updateTransaction)))
	mux.Handle("DELETE /api/v1/families/{id}/transactions/{tid}", auth(http.HandlerFunc(h.deleteTransaction)))

	// 增量同步
	mux.Handle("GET /api/v1/families/{id}/sync", auth(http.HandlerFunc(h.syncPull)))
	mux.Handle("POST /api/v1/families/{id}/sync/push", auth(http.HandlerFunc(h.syncPush)))

	// 统计
	mux.Handle("GET /api/v1/families/{id}/stats/summary", auth(http.HandlerFunc(h.statsSummary)))
	mux.Handle("GET /api/v1/families/{id}/stats/categories", auth(http.HandlerFunc(h.statsCategories)))
	mux.Handle("GET /api/v1/families/{id}/stats/trend", auth(http.HandlerFunc(h.statsTrend)))

	// 导出
	mux.Handle("GET /api/v1/families/{id}/export.csv", auth(http.HandlerFunc(h.exportCSV)))

	// 账单导入
	mux.Handle("POST /api/v1/families/{id}/imports", auth(http.HandlerFunc(h.importBill)))
	mux.Handle("GET /api/v1/families/{id}/imports", auth(http.HandlerFunc(h.listImports)))
	mux.Handle("POST /api/v1/families/{id}/imports/preview", auth(http.HandlerFunc(h.previewImport)))
	mux.Handle("POST /api/v1/families/{id}/imports/confirm", auth(http.HandlerFunc(h.confirmImport)))
	mux.Handle("POST /api/v1/families/{id}/category-rules", auth(http.HandlerFunc(h.saveCategoryRule)))

	// 拆单
	mux.Handle("POST /api/v1/families/{id}/transactions/{tid}/split", auth(http.HandlerFunc(h.splitTransaction)))
	mux.Handle("POST /api/v1/families/{id}/transactions/{tid}/unsplit", auth(http.HandlerFunc(h.undoSplit)))
	mux.Handle("GET /api/v1/families/{id}/transactions/{tid}/children", auth(http.HandlerFunc(h.listSplitChildren)))

	mux.Handle("GET /api/v1/families/{id}/stats/scopes", auth(http.HandlerFunc(h.statsScopes)))

	// AI 分类与人工修正
	mux.Handle("POST /api/v1/families/{id}/classify", auth(http.HandlerFunc(h.runClassify)))
	mux.Handle("GET /api/v1/families/{id}/classify/pending", auth(http.HandlerFunc(h.classifyPending)))
	mux.Handle("POST /api/v1/families/{id}/transactions/{tid}/review", auth(http.HandlerFunc(h.reviewTransaction)))

	// 账单邮箱（自动导入）
	mux.Handle("GET /api/v1/families/{id}/mail", auth(http.HandlerFunc(h.getMailConfig)))
	mux.Handle("PUT /api/v1/families/{id}/mail", auth(http.HandlerFunc(h.saveMailConfig)))
	mux.Handle("POST /api/v1/families/{id}/mail/test", auth(http.HandlerFunc(h.testMailConfig)))
	mux.Handle("POST /api/v1/families/{id}/mail/sync", auth(http.HandlerFunc(h.syncMail)))
	mux.Handle("POST /api/v1/families/{id}/mail/password", auth(http.HandlerFunc(h.submitMailPassword)))

	// 管理后台 API（独立 scope=admin 令牌）
	adminAuth := h.requireAdmin
	mux.HandleFunc("POST /admin/api/auth/login", h.adminLogin)
	mux.Handle("GET /admin/api/me", adminAuth(http.HandlerFunc(h.adminMe)))
	mux.Handle("GET /admin/api/overview", adminAuth(http.HandlerFunc(h.adminOverview)))
	// 设备授权
	mux.Handle("GET /admin/api/devices/pending", adminAuth(http.HandlerFunc(h.adminPendingDevices)))
	mux.Handle("GET /admin/api/devices", adminAuth(http.HandlerFunc(h.adminAllDevices)))
	mux.Handle("POST /admin/api/devices/{did}/approve", adminAuth(http.HandlerFunc(h.adminApproveDevice)))
	mux.Handle("POST /admin/api/devices/{did}/reject", adminAuth(http.HandlerFunc(h.adminRejectDevice)))
	mux.Handle("POST /admin/api/devices/{did}/revoke", adminAuth(http.HandlerFunc(h.adminRevokeDevice)))
	mux.Handle("GET /admin/api/users", adminAuth(http.HandlerFunc(h.adminUsers)))
	mux.Handle("PATCH /admin/api/users/{uid}", adminAuth(http.HandlerFunc(h.adminSetUserStatus)))
	mux.Handle("GET /admin/api/families", adminAuth(http.HandlerFunc(h.adminFamilies)))
	mux.Handle("GET /admin/api/families/{id}/transactions", adminAuth(http.HandlerFunc(h.adminFamilyTransactions)))
	mux.Handle("GET /admin/api/audit-logs", adminAuth(http.HandlerFunc(h.adminAuditLogs)))

	// 管理后台未知接口返回 JSON 404，不要落到前端 index.html
	mux.HandleFunc("/admin/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, domain.ErrNotFound.WithMessage("接口不存在"))
	})

	// 管理后台静态页面（单页应用）
	mux.Handle("/admin/", adminStatic())
	mux.Handle("/admin", adminStatic())

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, domain.ErrNotFound.WithMessage("接口不存在"))
	})

	// 顺序有讲究：请求 ID 要在日志中间件之外，日志里才带得上 request_id
	return withRecover(withCORS(cfg.CORSOrigins, withRequestID(withLogging(mux))))
}
