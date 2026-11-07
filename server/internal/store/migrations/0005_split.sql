-- 拆单：一笔账目可以拆成多行，父账目只做留档、不进统计
ALTER TABLE transactions ADD COLUMN parent_id TEXT NOT NULL DEFAULT '';

ALTER TABLE transactions ADD COLUMN is_split INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_tx_parent ON transactions(parent_id);
