package mailer_test

import (
	"testing"

	"family-ledger/server/internal/mailer"
)

// 邮件正文里的链接来自外部，必须限制域名，避免服务端被当成 SSRF 跳板。
func TestIsAllowedBillURL(t *testing.T) {
	allowed := []string{
		"https://tenpay.wechatpay.cn/userroll/userbilldownload/downloadfilefromemail?x=1",
		"https://wechatpay.cn/bill",
		"https://render.alipay.com/p/s/bill.zip",
		"https://mybank.cn/bill",
	}
	for _, u := range allowed {
		if !mailer.IsAllowedBillURL(u) {
			t.Fatalf("应允许: %s", u)
		}
	}

	blocked := []string{
		"http://tenpay.wechatpay.cn/bill",          // 非 https
		"https://127.0.0.1/bill",                   // 内网
		"https://localhost:8088/bill",              // 内网
		"https://169.254.169.254/latest/meta-data", // 云元数据
		"https://evil.com/wechatpay.cn/bill",       // 仿冒域名
		"https://wechatpay.cn.evil.com/bill",       // 后缀混淆
		"ftp://wechatpay.cn/bill",                  // 协议不对
	}
	for _, u := range blocked {
		if mailer.IsAllowedBillURL(u) {
			t.Fatalf("应拒绝: %s", u)
		}
	}
}
