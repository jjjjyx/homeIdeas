-- 设备唯一标识：同一台手机重复配对时用于识别，避免重复授权、重复绑定账号
ALTER TABLE devices ADD COLUMN device_key TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_devices_key ON devices(device_key);
