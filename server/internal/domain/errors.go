package domain

import (
	"fmt"
	"net/http"
)

// AppError 业务错误：Code 给客户端判断，Status 给 HTTP。
type AppError struct {
	Code    int
	Status  int
	Message string
}

func (e *AppError) Error() string {
	return fmt.Sprintf("code=%d %s", e.Code, e.Message)
}

func New(code, status int, message string) *AppError {
	return &AppError{Code: code, Status: status, Message: message}
}

// WithMessage 复制一份并替换提示文案，用于把具体原因带给客户端。
func (e *AppError) WithMessage(msg string) *AppError {
	return &AppError{Code: e.Code, Status: e.Status, Message: msg}
}

var (
	ErrBadRequest     = New(1001, http.StatusBadRequest, "参数不合法")
	ErrCredInvalid    = New(2001, http.StatusUnauthorized, "账号或密码错误")
	ErrUnauthorized   = New(2002, http.StatusUnauthorized, "未登录或登录已过期")
	ErrUserDisabled   = New(2003, http.StatusForbidden, "账号已被禁用")
	ErrForbidden      = New(3001, http.StatusForbidden, "没有权限执行该操作")
	ErrNotFound       = New(4001, http.StatusNotFound, "资源不存在")
	ErrConflict       = New(4002, http.StatusConflict, "资源已存在")
	ErrInviteRequired = New(4003, http.StatusForbidden, "需要邀请码才能加入家庭")
	ErrInviteInvalid  = New(4004, http.StatusBadRequest, "邀请码无效或已过期")
	ErrLastOwner      = New(4005, http.StatusBadRequest, "家庭至少需要保留一位户主")
	ErrInternal       = New(5000, http.StatusInternalServerError, "服务器开小差了")
)
