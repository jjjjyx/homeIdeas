package importer

import (
	"archive/zip"
	"bytes"
	"testing"
)

// 造一个最小的 xlsx（微信账单同构）：共享字符串 + 一张表，
// 时间用 Excel 序列号，金额用数字，收/支用文本。
func buildXLSX(t *testing.T, rows [][]string) []byte {
	t.Helper()

	shared := []string{}
	index := map[string]int{}
	getIdx := func(s string) int {
		if i, ok := index[s]; ok {
			return i
		}
		index[s] = len(shared)
		shared = append(shared, s)
		return index[s]
	}

	var sheet bytes.Buffer
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for r, row := range rows {
		sheet.WriteString(`<row r="` + itoa(r+1) + `">`)
		for c, val := range row {
			if val == "" {
				continue
			}
			ref := colName(c) + itoa(r+1)
			if isNumeric(val) {
				sheet.WriteString(`<c r="` + ref + `"><v>` + val + `</v></c>`)
			} else {
				sheet.WriteString(`<c r="` + ref + `" t="s"><v>` + itoa(getIdx(val)) + `</v></c>`)
			}
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData></worksheet>`)

	var sst bytes.Buffer
	sst.WriteString(`<?xml version="1.0" encoding="UTF-8"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	for _, s := range shared {
		sst.WriteString(`<si><t>` + s + `</t></si>`)
	}
	sst.WriteString(`</sst>`)

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	write := func(name, content string) {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	write("xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"></workbook>`)
	write("xl/sharedStrings.xml", sst.String())
	write("xl/worksheets/sheet1.xml", sheet.String())
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func colName(i int) string {
	return string(rune('A' + i))
}

func isNumeric(s string) bool {
	dot := false
	for i, ch := range s {
		if ch == '.' && !dot {
			dot = true
			continue
		}
		if i == 0 && (ch == '-' || ch == '+') {
			continue
		}
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return s != ""
}

func TestParseXLSXWechatLike(t *testing.T) {
	rows := [][]string{
		{"微信支付账单明细"},
		{"----------------------微信支付账单明细列表--------------------"},
		{"交易时间", "交易类型", "交易对方", "商品", "收/支", "金额(元)", "支付方式", "当前状态", "交易单号", "商户单号", "备注"},
		// Excel 序列号 46327.5 = 2026-11-01 12:00（1899-12-30 起算）
		{"46327.5", "商户消费", "美团", "午餐", "支出", "28.5", "零钱", "支付成功", "WX-A1", "M1", "/"},
		{"46327.5208333", "退款", "美团", "午餐退款", "收入", "8.5", "零钱", "已退款¥8.5", "WX-A2", "", "/"},
		{"46326.5", "商户消费", "滴滴", "打车", "支出", "20", "零钱", "已全额退款", "WX-A3", "M3", "/"},
		{"46325.5", "零钱通转入", "零钱通", "转入", "/", "100", "零钱", "已转入", "WX-A4", "", "/"},
	}

	res, err := ParseAny("微信支付账单.xlsx", buildXLSX(t, rows), "")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if res.Source != SourceWeChat {
		t.Fatalf("来源应识别为微信支付，实际 %s", res.Source)
	}
	// 退款行要保留（净额才对），中性交易行（收/支=/）要跳过
	if len(res.Records) != 3 {
		t.Fatalf("应解析出 3 条（含退款），实际 %d，跳过 %d", len(res.Records), res.Skipped)
	}
	if res.Skipped != 1 {
		t.Fatalf("应跳过 1 条中性交易，实际 %d", res.Skipped)
	}

	first := res.Records[0]
	if first.AmountCents != 2850 {
		t.Fatalf("金额解析错误: %d", first.AmountCents)
	}
	if first.OccurredAt.Year() != 2026 || first.OccurredAt.Month() != 11 || first.OccurredAt.Day() != 1 {
		t.Fatalf("Excel 序列号转时间错误: %s", first.OccurredAt)
	}
	if res.Records[1].Direction != 1 {
		t.Fatalf("退款行应为收入，实际 %d", res.Records[1].Direction)
	}
	if res.Records[2].Status != "已全额退款" || res.Records[2].Direction != 0 {
		t.Fatalf("已全额退款的支出行应保留: %+v", res.Records[2])
	}
}

func TestIsXLSX(t *testing.T) {
	data := buildXLSX(t, [][]string{{"a"}})
	if !IsXLSX(data) {
		t.Fatal("应识别为 xlsx")
	}
	if IsXLSX(gbk("交易时间,金额\n")) {
		t.Fatal("CSV 不应被识别为 xlsx")
	}
}
