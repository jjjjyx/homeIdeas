package httpapi

import (
	"net/http"
	"strconv"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/service"
)

// ---------- App 侧（无需登录） ----------

type deviceStartReq struct {
	DeviceUID  string `json:"device_uid"`
	DeviceKey  string `json:"device_key"`
	Model      string `json:"model"`
	OSVersion  string `json:"os_version"`
	AppVersion string `json:"app_version"`
}

// deviceStart App 发起配对请求
func (h *Handler) deviceStart(w http.ResponseWriter, r *http.Request) {
	var req deviceStartReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := h.devices.StartDevice(r.Context(), service.StartDeviceInput{
		DeviceUID:  req.DeviceUID,
		DeviceKey:  req.DeviceKey,
		Model:      req.Model,
		OSVersion:  req.OSVersion,
		AppVersion: req.AppVersion,
		IP:         clientIP(r),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, result)
}

type devicePollReq struct {
	PairCode   string `json:"pair_code"`
	PairSecret string `json:"pair_secret"`
}

// devicePoll App 轮询配对状态
func (h *Handler) devicePoll(w http.ResponseWriter, r *http.Request) {
	var req devicePollReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := h.devices.PollDevice(r.Context(), req.PairCode, req.PairSecret)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, result)
}

// ---------- 后台侧（平台管理员） ----------

func (h *Handler) adminPendingDevices(w http.ResponseWriter, r *http.Request) {
	list, err := h.devices.PendingWithHistory(r.Context(), userIDFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, it := range list {
		m := deviceJSON(it.Device)
		m["device_key"] = it.DeviceKey
		if it.LastUserID != "" {
			m["last_user_id"] = it.LastUserID
			m["last_account"] = it.LastAccount
			m["last_nickname"] = it.LastNickname
			m["last_approved_at"] = it.LastApprovedAt
			m["last_family_id"] = it.LastFamilyID
			m["last_role"] = it.LastRole
		}
		items = append(items, m)
	}
	writeData(w, r, map[string]any{"items": items})
}

func (h *Handler) adminAllDevices(w http.ResponseWriter, r *http.Request) {
	includeExpired := r.URL.Query().Get("include_expired") == "1"
	list, err := h.devices.List(r.Context(), userIDFrom(r.Context()), includeExpired)
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, d := range list {
		items = append(items, deviceJSON(d))
	}
	writeData(w, r, map[string]any{"items": items})
}

type deviceApproveReq struct {
	UserID      string `json:"user_id"`
	NewAccount  string `json:"new_account"`
	NewNickname string `json:"new_nickname"`
	NewPassword string `json:"new_password"`
	FamilyID    string `json:"family_id"`
	Role        string `json:"role"`
}

func (h *Handler) adminApproveDevice(w http.ResponseWriter, r *http.Request) {
	var req deviceApproveReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := h.devices.Approve(r.Context(), userIDFrom(r.Context()), r.PathValue("did"),
		service.ApproveInput{
			UserID: req.UserID, NewAccount: req.NewAccount, NewNickname: req.NewNickname,
			NewPassword: req.NewPassword, FamilyID: req.FamilyID, Role: req.Role,
		})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, result)
}

func (h *Handler) adminRejectDevice(w http.ResponseWriter, r *http.Request) {
	if err := h.devices.Reject(r.Context(), userIDFrom(r.Context()), r.PathValue("did")); err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已拒绝")
}

func (h *Handler) adminRevokeDevice(w http.ResponseWriter, r *http.Request) {
	if err := h.devices.Revoke(r.Context(), userIDFrom(r.Context()), r.PathValue("did")); err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已吊销，该设备的登录已失效")
}

func deviceJSON(d domain.Device) map[string]any {
	out := map[string]any{
		"id": d.ID, "device_uid": d.DeviceUID, "device_key": d.DeviceKey, "model": d.Model, "os_version": d.OSVersion,
		"app_version": d.AppVersion, "ip": d.IP, "status": d.Status, "pair_code": d.PairCode,
		"created_at": d.CreatedAt, "expires_at": d.ExpiresAt, "user_id": d.UserID,
		"family_id": d.FamilyID, "role": d.Role,
	}
	if d.ApprovedAt != nil {
		out["approved_at"] = *d.ApprovedAt
	}
	if d.LastSeenAt != nil {
		out["last_seen_at"] = *d.LastSeenAt
	}
	return out
}

var _ = strconv.Itoa
