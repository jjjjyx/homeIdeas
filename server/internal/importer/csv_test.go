package importer

import (
	"bytes"
	"io"
	"testing"

	"github.com/yeka/zip"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// GBK 是微信/支付宝账单的真实编码，测试数据也必须走 GBK，否则测不出编码问题
func gbk(s string) []byte {
	out, err := io.ReadAll(transform.NewReader(bytes.NewBufferString(s), simplifiedchinese.GBK.NewEncoder()))
	if err != nil {
		panic(err)
	}
	return out
}

const wechatCSV = `微信支付账单明细,,,,,,,,,,
微信昵称：[爸爸],,,,,,,,,,
起始时间：[2026-10-01 00:00:00] 终止时间：[2026-11-01 00:00:00],,,,,,,,,,
导出类型：[全部],,,,,,,,,,
----------------------微信支付账单明细列表--------------------,,,,,,,,,,
交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号,备注
2026-10-05 12:30:45,商户消费,美团,午餐,支出,￥28.50,零钱,支付成功,4200001234202610051234567890,100012345,/
2026-10-06 18:10:00,商户消费,星巴克,咖啡,支出,￥33.00,零钱通,支付成功,4200001234202610061234567891,100012346,/
2026-10-07 09:00:00,转账,张三,转账,不计收支,￥200.00,零钱,朋友已收钱,4200001234202610071234567892,0,/
2026-10-08 10:00:00,商户消费,京东,退款商品,支出,￥99.00,零钱,已全额退款,4200001234202610081234567893,100012347,/
2026-10-09 08:00:00,转账,李四,红包,收入,￥88.00,零钱,已存入零钱,4200001234202610091234567894,0,/
`

const alipayCSV = `支付宝交易记录明细查询,,,,,,,,,,,,
账号:[papa@example.com],,,,,,,,,,,,
起始日期:[2026-10-01 00:00:00]    终止日期:[2026-11-01 00:00:00],,,,,,,,,,,,
---------------------------------交易记录明细列表------------------------------------,,,,,,,,,,,,
交易时间,交易分类,交易对方,对方账号,商品说明,收/支,金额,收/付款方式,交易状态,交易订单号,商家订单号,备注
2026-10-05 12:30:45,餐饮美食,美团,,午餐,支出,28.50,余额宝,交易成功,2026100522001234567890,10001,
2026-10-06 10:00:00,转账红包,张三,,,不计收支,100.00,余额,交易成功,2026100622001234567891,,
2026-10-07 09:00:00,其他,公司,,工资,收入,8000.00,银行卡,交易成功,2026100722001234567892,,
`

const bankCSV = `工商银行账户明细,,,,,
交易日期,摘要,交易金额,借贷标志,对方户名,交易流水号
2026-10-01,消费,120.00,借,某某超市,2026100100001
2026-10-02,工资,8000.00,贷,某某公司,2026100200002
`

func TestParseWechat(t *testing.T) {
	res, err := ParseCSV(gbk(wechatCSV), "微信支付账单(20261001-20261101).csv")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if res.Source != SourceWeChat {
		t.Fatalf("来源识别错误: %s", res.Source)
	}
	// 退款行要保留（账单里原交易和退款是两行，都留着净额才对）
	if len(res.Records) != 4 {
		t.Fatalf("应解析出 4 条（含退款），实际 %d（跳过 %d）", len(res.Records), res.Skipped)
	}
	if res.Skipped != 1 {
		t.Fatalf("应跳过 1 条（不计收支），实际 %d", res.Skipped)
	}

	first := res.Records[0]
	if first.AmountCents != 2850 || first.Direction != 0 {
		t.Fatalf("第一条应为支出 28.50，实际 %d / dir=%d", first.AmountCents, first.Direction)
	}
	if first.Counterparty != "美团" || first.OccurredAt.Day() != 5 {
		t.Fatalf("对方/时间解析错误: %+v", first)
	}
	if first.ExternalID != "4200001234202610051234567890" {
		t.Fatalf("交易单号应作为去重键，实际 %s", first.ExternalID)
	}

	income := res.Records[3]
	if income.Direction != 1 || income.AmountCents != 8800 {
		t.Fatalf("第三条应为收入 88.00，实际 %+v", income)
	}
}

func TestParseAlipay(t *testing.T) {
	res, err := ParseCSV(gbk(alipayCSV), "alipay_record_20261101.csv")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if res.Source != SourceAlipay {
		t.Fatalf("来源识别错误: %s", res.Source)
	}
	if len(res.Records) != 2 {
		t.Fatalf("应解析出 2 条，实际 %d", len(res.Records))
	}
	if res.Records[0].Method != "余额宝" {
		t.Fatalf("付款方式解析错误: %s", res.Records[0].Method)
	}
	if res.Records[1].Direction != 1 || res.Records[1].AmountCents != 800000 {
		t.Fatalf("收入解析错误: %+v", res.Records[1])
	}
}

func TestParseBank(t *testing.T) {
	res, err := ParseCSV(gbk(bankCSV), "工行明细.csv")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if res.Source != SourceBank {
		t.Fatalf("来源应为 bank，实际 %s", res.Source)
	}
	if len(res.Records) != 2 {
		t.Fatalf("应解析出 2 条，实际 %d", len(res.Records))
	}
	if res.Records[0].Direction != 0 || res.Records[0].AmountCents != 12000 {
		t.Fatalf("借方应为支出 120.00，实际 %+v", res.Records[0])
	}
	if res.Records[1].Direction != 1 || res.Records[1].AmountCents != 800000 {
		t.Fatalf("贷方应为收入 8000.00，实际 %+v", res.Records[1])
	}
}

func TestUTF8WithBOM(t *testing.T) {
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(wechatCSV)...)
	res, err := ParseCSV(data, "微信账单.csv")
	if err != nil {
		t.Fatalf("BOM 文件解析失败: %v", err)
	}
	if len(res.Records) != 4 {
		t.Fatalf("应解析出 4 条，实际 %d", len(res.Records))
	}
}

func TestEncryptedZip(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Encrypt("微信支付账单.csv", "123456", zip.AES256Encryption)
	if err != nil {
		t.Fatalf("创建加密 zip 失败: %v", err)
	}
	if _, err := f.Write(gbk(wechatCSV)); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	data := buf.Bytes()
	if !IsZip(data) {
		t.Fatal("应识别为 zip")
	}

	res, err := ParseAny("账单.zip", data, "123456")
	if err != nil {
		t.Fatalf("解密解析失败: %v", err)
	}
	if len(res.Records) != 4 {
		t.Fatalf("应解析出 4 条，实际 %d", len(res.Records))
	}

	// 密码错误必须报错，不能静默返回空
	if _, err := ParseAny("账单.zip", data, "wrong"); err == nil {
		t.Fatal("错误密码应该报错")
	}

	// 加密包 + 没给密码：必须返回明确错误，不能 panic
	// （yeka/zip 在空密码时会 nil 解引用，靠 ExtractCSV 的前置判断挡住）
	if _, err := ParseAny("账单.zip", data, ""); err == nil {
		t.Fatal("加密包缺少密码时应该报错")
	}
}

func TestMissingHeader(t *testing.T) {
	if _, err := ParseCSV([]byte("随便写点东西\n没有表头\n"), "x.csv"); err == nil {
		t.Fatal("没有表头应该报错")
	}
}

// 支付宝账单用的是「传统 ZipCrypto + deflate」，和 AES 是两条不同的解密路径，
// 单独验证一遍，避免把「密码错」误判成「代码不支持」。
func TestZipCryptoEncrypted(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Encrypt("支付宝交易明细.csv", "121235", zip.StandardEncryption)
	if err != nil {
		t.Fatalf("创建 ZipCrypto zip 失败: %v", err)
	}
	if _, err := f.Write(gbk(wechatCSV)); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}

	res, err := ParseAny("账单.zip", buf.Bytes(), "121235")
	if err != nil {
		t.Fatalf("ZipCrypto 解密失败: %v", err)
	}
	if len(res.Records) != 4 {
		t.Fatalf("应解析出 4 条，实际 %d", len(res.Records))
	}

	if _, err := ParseAny("账单.zip", buf.Bytes(), "000000"); err == nil {
		t.Fatal("错误密码应该报错")
	}
}
