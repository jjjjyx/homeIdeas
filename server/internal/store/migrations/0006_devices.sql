-- 设备配对授权：新设备由管理员在后台批准后直接登录，不用输密码

CREATE TABLE IF NOT EXISTS devices (
  id           TEXT PRIMARY KEY,
  device_uid   TEXT NOT NULL,
  user_id      TEXT,
  family_id    TEXT,
  role         TEXT,
  model        TEXT NOT NULL DEFAULT '',
  os_version   TEXT NOT NULL DEFAULT '',
  app_version  TEXT NOT NULL DEFAULT '',
  ip           TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'pending',
  pair_code    TEXT NOT NULL,
  pair_secret  TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  expires_at   INTEGER NOT NULL,
  approved_at  INTEGER,
  approved_by  TEXT,
  last_seen_at INTEGER
);

CREATE INDEX IF NOT EXISTS idx_devices_status ON devices(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_devices_uid ON devices(device_uid);
CREATE INDEX IF NOT EXISTS idx_devices_code ON devices(pair_code);

ALTER TABLE refresh_tokens ADD COLUMN device_id TEXT NOT NULL DEFAULT '';
