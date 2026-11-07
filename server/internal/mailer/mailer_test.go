package mailer_test

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"io"
	"strings"
	"testing"

	"family-ledger/server/internal/mailer"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func gbk(s string) []byte {
	out, err := io.ReadAll(transform.NewReader(bytes.NewBufferString(s), simplifiedchinese.GBK.NewEncoder()))
	if err != nil {
		panic(err)
	}
	return out
}

func makeZip(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("bill.csv")
	if err != nil {
		t.Fatalf("创建 zip 失败: %v", err)
	}
	if _, err := f.Write(content); err != nil {
		t.Fatalf("写入 zip 失败: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭 zip 失败: %v", err)
	}
	return buf.Bytes()
}

func gbkB64(t *testing.T, s string) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(gbk(s))
}

// qEncode RFC2047 的 Q 编码：非字母数字字节写成 =XX
func qEncode(raw []byte) string {
	var sb strings.Builder
	const hex = "0123456789ABCDEF"
	for _, b := range raw {
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') {
			sb.WriteByte(b)
			continue
		}
		sb.WriteByte('=')
		sb.WriteByte(hex[b>>4])
		sb.WriteByte(hex[b&0x0f])
	}
	return sb.String()
}

func gb18030Q(t *testing.T, s string) string {
	t.Helper()
	raw, err := io.ReadAll(transform.NewReader(bytes.NewBufferString(s), simplifiedchinese.GB18030.NewEncoder()))
	if err != nil {
		t.Fatalf("GB18030 编码失败: %v", err)
	}
	return qEncode(raw)
}

// buildAlipayLikeMail 复刻支付宝账单邮件的真实结构（不含任何真实数据）：
//   - 头部是 GBK 的 encoded-word，不注册 charset 解码器就会整封解析失败
//   - 附件用 application/octet-stream，文件名是 GB18030 的 encoded-word 且跨行折叠
//   - 附件本身是 zip
func buildAlipayLikeMail(t *testing.T, zipData []byte) []byte {
	t.Helper()
	fileName := gb18030Q(t, "支付宝交易明细(20261001-20261101).zip")

	var body bytes.Buffer
	body.WriteString("MIME-Version: 1.0\r\n")
	body.WriteString("Date: Sun, 1 Nov 2026 10:00:00 +0800\r\n")
	body.WriteString("From: =?GBK?B?" + gbkB64(t, "支付宝提醒") + "?= <service@mail.alipay.com>\r\n")
	body.WriteString("To: test@example.com\r\n")
	body.WriteString("Subject: =?GBK?B?" + gbkB64(t, "支付宝交易流水明细") + "?=\r\n")
	body.WriteString("Content-Type: multipart/mixed; \r\n\tboundary=\"----=_Part_1\"\r\n")
	body.WriteString("\r\n")
	body.WriteString("------=_Part_1\r\n")
	body.WriteString("Content-Type: text/html; charset=GBK\r\n")
	body.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	body.WriteString(base64.StdEncoding.EncodeToString(gbk("<p>账单见附件</p>")) + "\r\n")
	body.WriteString("------=_Part_1\r\n")
	body.WriteString("Content-Type: application/octet-stream; \r\n")
	body.WriteString("\tname=\"=?GB18030?Q?" + fileName + "?=\"\r\n")
	body.WriteString("Content-Transfer-Encoding: base64\r\n")
	body.WriteString("Content-Disposition: attachment; \r\n")
	body.WriteString("\tfilename=\"=?GB18030?Q?" + fileName + "?=\"\r\n\r\n")
	body.WriteString(base64.StdEncoding.EncodeToString(zipData) + "\r\n")
	body.WriteString("------=_Part_1--\r\n")
	return body.Bytes()
}

func TestParseAlipayLikeMail(t *testing.T) {
	zipData := makeZip(t, gbk("交易时间,交易对方,收/支,金额\n2026-10-01,测试商户,支出,10.00\n"))

	msg, err := mailer.ParseRaw(buildAlipayLikeMail(t, zipData))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	if msg.Subject != "支付宝交易流水明细" {
		t.Fatalf("GBK 主题应被解码，实际 %q（err=%s）", msg.Subject, msg.ParseError)
	}
	if !strings.Contains(msg.From, "支付宝提醒") {
		t.Fatalf("GBK 发件人应被解码，实际 %q", msg.From)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("应识别出 1 个附件，实际 %d（部件 %d）", len(msg.Attachments), len(msg.Parts))
	}
	if !strings.Contains(msg.Attachments[0].FileName, "支付宝交易明细") {
		t.Fatalf("GB18030 附件名应被解码，实际 %q", msg.Attachments[0].FileName)
	}
	if !strings.HasSuffix(msg.Attachments[0].FileName, ".zip") {
		t.Fatalf("附件名应以 .zip 结尾，实际 %q", msg.Attachments[0].FileName)
	}
}
