-- 开支归属：家庭共同 / 个人（含归属人），以及 AI 分类的溯源信息

ALTER TABLE transactions ADD COLUMN scope TEXT NOT NULL DEFAULT 'unknown';

ALTER TABLE transactions ADD COLUMN owner_id TEXT NOT NULL DEFAULT '';

ALTER TABLE transactions ADD COLUMN category_source TEXT NOT NULL DEFAULT '';

ALTER TABLE transactions ADD COLUMN ai_confidence REAL NOT NULL DEFAULT 0;

ALTER TABLE transactions ADD COLUMN ai_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE transactions ADD COLUMN reviewed INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_tx_scope ON transactions(family_id, scope, occurred_at DESC);
