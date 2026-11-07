-- 账单导入：来源标记、外部单号去重、导入批次、分类规则、邮箱配置

ALTER TABLE transactions ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';

ALTER TABLE transactions ADD COLUMN external_id TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_tx_external ON transactions(family_id, external_id) WHERE external_id <> '';

CREATE TABLE IF NOT EXISTS import_batches (
  id TEXT PRIMARY KEY,
  family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
  source TEXT NOT NULL,
  file_name TEXT NOT NULL DEFAULT '',
  total INTEGER NOT NULL DEFAULT 0,
  imported INTEGER NOT NULL DEFAULT 0,
  skipped INTEGER NOT NULL DEFAULT 0,
  failed INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'done',
  message TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_import_batches_family ON import_batches(family_id, created_at DESC);

CREATE TABLE IF NOT EXISTS category_rules (
  id TEXT PRIMARY KEY,
  family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
  keyword TEXT NOT NULL,
  category_id TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 0,
  hits INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_rules_family_keyword ON category_rules(family_id, keyword);

CREATE TABLE IF NOT EXISTS mail_accounts (
  id TEXT PRIMARY KEY,
  family_id TEXT NOT NULL UNIQUE REFERENCES families(id) ON DELETE CASCADE,
  host TEXT NOT NULL,
  port INTEGER NOT NULL DEFAULT 993,
  username TEXT NOT NULL,
  password_enc TEXT NOT NULL DEFAULT '',
  extract_password_enc TEXT NOT NULL DEFAULT '',
  use_tls INTEGER NOT NULL DEFAULT 1,
  last_uid INTEGER NOT NULL DEFAULT 0,
  last_sync_at INTEGER,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS mail_messages (
  id TEXT PRIMARY KEY,
  family_id TEXT NOT NULL,
  message_uid TEXT NOT NULL,
  subject TEXT NOT NULL DEFAULT '',
  from_addr TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',
  detail TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_msg ON mail_messages(family_id, message_uid);
