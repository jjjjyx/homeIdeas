-- 每封账单邮件单独存解压密码：微信和支付宝的密码各不相同，不能共用一个
ALTER TABLE mail_messages ADD COLUMN password_enc TEXT NOT NULL DEFAULT '';
