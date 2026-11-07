package httpapi

import (
	"net/http"

	"family-ledger/server/internal/config"
	"family-ledger/server/internal/domain"
)

// NewRouter 组装路由。鉴权中间件只包在需要登录的接口上。
//
// 单家庭模式：所有数据接口都不再带 family_id 路径段，唯一家庭由中间件注入。
func NewRouter(cfg *config.Config, h *Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET /readyz", h.ready)

	// 公开接口
	mux.HandleFunc("POST /api/v1/auth/register", h.register) // 仅首次初始化
	mux.HandleFunc("POST /api/v1/auth/login", h.login)
	// 设备配对：新设备在这里发起，管理员批准后 App 轮询拿令牌
	mux.HandleFunc("POST /api/v1/auth/device/login", h.deviceLogin)
	mux.HandleFunc("POST /api/v1/auth/device/start", h.deviceStart)
	mux.HandleFunc("POST /api/v1/auth/device/poll", h.devicePoll)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", h.logout)

	// 需要登录
	auth := h.requireAuth
	mux.Handle("GET /api/v1/me", auth(http.HandlerFunc(h.me)))

	// 家庭信息与成员
	mux.Handle("GET /api/v1/family", auth(http.HandlerFunc(h.getFamily)))
	mux.Handle("PATCH /api/v1/family", auth(http.HandlerFunc(h.renameFamily)))
	mux.Handle("POST /api/v1/family/invites", auth(http.HandlerFunc(h.createInvite)))
	mux.Handle("GET /api/v1/family/members", auth(http.HandlerFunc(h.listMembers)))
	mux.Handle("POST /api/v1/family/members", auth(http.HandlerFunc(h.addMember)))
	mux.Handle("PATCH /api/v1/family/members/{uid}", auth(http.HandlerFunc(h.setRole)))
	mux.Handle("DELETE /api/v1/family/members/{uid}", auth(http.HandlerFunc(h.removeMember)))

	// 账目
	mux.Handle("GET /api/v1/transactions", auth(http.HandlerFunc(h.listTransactions)))
	mux.Handle("POST /api/v1/transactions", auth(http.HandlerFunc(h.createTransaction)))
	mux.Handle("PATCH /api/v1/transactions/{tid}", auth(http.HandlerFunc(h.updateTransaction)))
	mux.Handle("DELETE /api/v1/transactions/{tid}", auth(http.HandlerFunc(h.deleteTransaction)))

	// 增量同步
	mux.Handle("GET /api/v1/sync", auth(http.HandlerFunc(h.syncPull)))
	mux.Handle("POST /api/v1/sync/push", auth(http.HandlerFunc(h.syncPush)))

	// 统计
	mux.Handle("GET /api/v1/stats/summary", auth(http.HandlerFunc(h.statsSummary)))
	mux.Handle("GET /api/v1/stats/categories", auth(http.HandlerFunc(h.statsCategories)))
	mux.Handle("GET /api/v1/stats/trend", auth(http.HandlerFunc(h.statsTrend)))
	mux.Handle("GET /api/v1/stats/scopes", auth(http.HandlerFunc(h.statsScopes)))

	// 导出
	mux.Handle("GET /api/v1/export.csv", auth(http.HandlerFunc(h.exportCSV)))

	// 账单导入
	mux.Handle("POST /api/v1/imports", auth(http.HandlerFunc(h.importBill)))
	mux.Handle("GET /api/v1/imports", auth(http.HandlerFunc(h.listImports)))
	mux.Handle("POST /api/v1/imports/preview", auth(http.HandlerFunc(h.previewImport)))
	mux.Handle("POST /api/v1/imports/confirm", auth(http.HandlerFunc(h.confirmImport)))
	mux.Handle("POST /api/v1/category-rules", auth(http.HandlerFunc(h.saveCategoryRule)))

	// 拆单
	mux.Handle("POST /api/v1/transactions/{tid}/split", auth(http.HandlerFunc(h.splitTransaction)))
	mux.Handle("POST /api/v1/transactions/{tid}/unsplit", auth(http.HandlerFunc(h.undoSplit)))
	mux.Handle("GET /api/v1/transactions/{tid}/children", auth(http.HandlerFunc(h.listSplitChildren)))

	// AI 分类与人工修正
	mux.Handle("POST /api/v1/classify", auth(http.HandlerFunc(h.runClassify)))
	mux.Handle("GET /api/v1/classify/pending", auth(http.HandlerFunc(h.classifyPending)))
	mux.Handle("POST /api/v1/transactions/{tid}/review", auth(http.HandlerFunc(h.reviewTransaction)))

	// 账单邮箱（自动导入）
	mux.Handle("GET /api/v1/mail", auth(http.HandlerFunc(h.getMailConfig)))
	mux.Handle("PUT /api/v1/mail", auth(http.HandlerFunc(h.saveMailConfig)))
	mux.Handle("POST /api/v1/mail/test", auth(http.HandlerFunc(h.testMailConfig)))
	mux.Handle("POST /api/v1/mail/sync", auth(http.HandlerFunc(h.syncMail)))
	mux.Handle("POST /api/v1/mail/password", auth(http.HandlerFunc(h.submitMailPassword)))

	// 管理后台 API（独立 scope=admin 令牌）
	adminAuth := h.requireAdmin
	mux.HandleFunc("POST /admin/api/auth/login", h.adminLogin)
	mux.Handle("GET /admin/api/me", adminAuth(http.HandlerFunc(h.adminMe)))
	mux.Handle("GET /admin/api/overview", adminAuth(http.HandlerFunc(h.adminOverview)))
	mux.Handle("GET /admin/api/family", adminAuth(http.HandlerFunc(h.getFamily)))
	mux.Handle("PATCH /admin/api/family", adminAuth(http.HandlerFunc(h.renameFamily)))
	mux.Handle("POST /admin/api/invites", adminAuth(http.HandlerFunc(h.createInvite)))
	// 成员管理
	mux.Handle("GET /admin/api/members", adminAuth(http.HandlerFunc(h.adminMembers)))
	mux.Handle("POST /admin/api/members", adminAuth(http.HandlerFunc(h.addMember)))
	mux.Handle("PATCH /admin/api/members/{uid}", adminAuth(http.HandlerFunc(h.adminUpdateMember)))
	mux.Handle("DELETE /admin/api/members/{uid}", adminAuth(http.HandlerFunc(h.removeMember)))
	// 设备授权
	// 待授权设备（新设备首次加入时批准一次，之后永久记住）
	mux.Handle("GET /admin/api/devices/pending", adminAuth(http.HandlerFunc(h.adminPendingDevices)))
	mux.Handle("POST /admin/api/devices/{did}/approve", adminAuth(http.HandlerFunc(h.adminApproveDevice)))
	mux.Handle("POST /admin/api/devices/{did}/reject", adminAuth(http.HandlerFunc(h.adminRejectDevice)))
	// 成员名下的设备：查看 + 解绑
	mux.Handle("GET /admin/api/members/{uid}/devices", adminAuth(http.HandlerFunc(h.memberDevices)))
	mux.Handle("DELETE /admin/api/members/{uid}/devices/{did}", adminAuth(http.HandlerFunc(h.unbindMemberDevice)))
	// 账目与审计
	mux.Handle("GET /admin/api/transactions", adminAuth(http.HandlerFunc(h.adminTransactions)))
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
