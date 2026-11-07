package httpapi

import "net/http"

type deviceLoginReq struct {
	DeviceKey string `json:"device_key"`
}

// deviceLogin 用永久记住的设备标识直接换令牌。
// 设备没被授权过时返回 4101，App 会转而发起配对。
func (h *Handler) deviceLogin(w http.ResponseWriter, r *http.Request) {
	var req deviceLoginReq
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	session, err := h.devices.LoginByDevice(r.Context(), req.DeviceKey, clientIP(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, toSessionResp(session))
}

// memberDevices 某个成员名下的设备
func (h *Handler) memberDevices(w http.ResponseWriter, r *http.Request) {
	list, err := h.devices.MemberDevices(r.Context(), userIDFrom(r.Context()), r.PathValue("uid"))
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

// unbindMemberDevice 解绑设备
func (h *Handler) unbindMemberDevice(w http.ResponseWriter, r *http.Request) {
	err := h.devices.UnbindDevice(r.Context(), userIDFrom(r.Context()),
		r.PathValue("uid"), r.PathValue("did"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeMessage(w, r, "已解绑，该设备下次打开需要重新批准")
}
