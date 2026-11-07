-- 设备改为「永久记住的已授权设备」：一台物理设备一行，keyed by device_key
-- 清理旧状态（过期/拒绝/被顶替）与无标识的历史行
DELETE FROM devices WHERE status IN ('expired', 'rejected', 'superseded');
DELETE FROM devices WHERE device_key = '';

-- 同一个 device_key 只保留最近批准的一条
DELETE FROM devices
WHERE id NOT IN (
  SELECT keep.id FROM devices keep
  WHERE keep.approved_at IS NOT NULL
    AND keep.approved_at = (
      SELECT MAX(x.approved_at) FROM devices x WHERE x.device_key = keep.device_key
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_devices_key ON devices(device_key);

-- 成员视角查询
CREATE INDEX IF NOT EXISTS idx_devices_user ON devices(user_id, last_seen_at DESC);
