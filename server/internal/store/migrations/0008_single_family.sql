-- 单家庭模式：系统只围绕一个家庭运行。
--
-- 老库里如果有多个家庭，保留「账单最多、创建最早」的那个作为唯一家庭，
-- 其余家庭软删除（数据仍在库里，可随时手工找回），避免直接物理删除造成不可逆损失。
--
-- 注意：invites 表保留。邀请码仍是家庭成员加入/注册的主要方式。
UPDATE families
SET deleted_at = CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE deleted_at IS NULL
  AND id <> (
    SELECT f.id
    FROM families f
    WHERE f.deleted_at IS NULL
    ORDER BY (
        SELECT COUNT(*) FROM transactions t
        WHERE t.family_id = f.id AND t.deleted_at IS NULL
      ) DESC,
      f.created_at ASC
    LIMIT 1
  );
