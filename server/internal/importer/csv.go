package importer

import (
	"bytes"
	"crypto/sha1"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

type field int

const (
	fTime field = iota
	fDirection
	fAmount
	fCounterparty
	fItem
	fMethod
	fStatus
	fExternal
	fieldCount
)

type profile struct {
	source  Source
	aliases map[field][]string
}

var profiles = []profile{
	{
		source: SourceWeChat,
		aliases: map[field][]string{
			fTime:         {"交易时间"},
			fDirection:    {"收/支", "收支"},
			fAmount:       {"金额"},
			fCounterparty: {"交易对方"},
			fItem:         {"商品"},
			fMethod:       {"支付方式"},
			fStatus:       {"当前状态"},
			fExternal:     {"交易单号"},
		},
	},
	{
		source: SourceAlipay,
		aliases: map[field][]string{
			fTime:         {"交易时间"},
			fDirection:    {"收/支", "收支"},
			fAmount:       {"金额"},
			fCounterparty: {"交易对方"},
			fItem:         {"商品说明", "商品名称"},
			fMethod:       {"收/付款方式", "付款方式"},
			fStatus:       {"交易状态"},
			fExternal:     {"交易订单号", "交易号"},
		},
	},
	{
		source: SourceUnionPay,
		aliases: map[field][]string{
			fTime:         {"交易时间", "交易日期"},
			fDirection:    {"收/支", "收支", "借贷标志"},
			fAmount:       {"金额", "交易金额"},
			fCounterparty: {"交易对方", "商户名称", "对方户名"},
			fItem:         {"商品", "商品说明", "交易说明", "摘要"},
			fMethod:       {"支付方式", "付款方式"},
			fStatus:       {"交易状态", "当前状态"},
			fExternal:     {"交易单号", "交易订单号", "流水号"},
		},
	},
	{
		source: SourceBank,
		aliases: map[field][]string{
			fTime:         {"交易时间", "交易日期", "记账日期", "交易日期时间"},
			fDirection:    {"收/支", "收支", "借贷标志", "借贷方向"},
			fAmount:       {"金额", "交易金额", "发生额"},
			fCounterparty: {"交易对方", "对方户名", "对方名称", "交易对手"},
			fItem:         {"商品", "商品说明", "摘要", "用途", "交易说明"},
			fMethod:       {"支付方式", "付款方式", "交易渠道"},
			fStatus:       {"交易状态", "当前状态", "状态"},
			fExternal:     {"交易单号", "交易订单号", "流水号", "交易流水号"},
		},
	},
}

// decodeText 微信/支付宝/银行的账单 CSV 基本都是 GBK，先按 UTF-8 试，不行再转。
func decodeText(data []byte) (string, error) {
	// 去 BOM
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(data) {
		return string(data), nil
	}
	decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(data), simplifiedchinese.GBK.NewDecoder()))
	if err != nil {
		return "", fmt.Errorf("账单文件编码无法识别（既不是 UTF-8 也不是 GBK）: %w", err)
	}
	return string(decoded), nil
}

func readRows(text string) ([][]string, error) {
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	return r.ReadAll()
}

// normalizeHeader 去掉空格和括号里的单位，便于别名匹配： "金额(元)" -> "金额"
func normalizeHeader(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\u3000", "")
	if i := strings.IndexAny(s, "(（"); i > 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

func matchColumn(header string, aliases []string) bool {
	h := normalizeHeader(header)
	for _, alias := range aliases {
		if h == strings.ToLower(alias) || strings.Contains(h, strings.ToLower(alias)) {
			return true
		}
	}
	return false
}

// locate 找到表头行，并建立「字段 -> 列下标」的映射
func locate(rows [][]string, p profile, res *Result) (int, map[field]int, int) {
	for idx, row := range rows {
		if idx > 60 { // 表头不会埋得太深
			break
		}
		cols := map[field]int{}
		for i := range fieldCount {
			cols[field(i)] = -1
		}
		for colIndex, cell := range row {
			for f := 0; f < int(fieldCount); f++ {
				if cols[field(f)] != -1 {
					continue
				}
				if matchColumn(cell, p.aliases[field(f)]) {
					cols[field(f)] = colIndex
				}
			}
		}
		// 至少要识别出时间和金额，才算真正的表头行
		if cols[fTime] >= 0 && cols[fAmount] >= 0 {
			res.Headers = append([]string(nil), row...)
			return idx, cols, 0
		}
	}
	return -1, nil, 0
}

func cell(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

// parseAmount 处理 "¥1,234.56" / "-1234.56" / "1234.56" 这类写法
func parseAmount(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("金额为空")
	}
	s = strings.NewReplacer("¥", "", "￥", "", ",", "", " ", "", "\u3000", "").Replace(s)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimPrefix(s, "+")
	value, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("金额格式不对: %s", s)
	}
	cents := int64(value*100 + 0.5)
	if neg {
		cents = -cents
	}
	return cents, nil
}

var timeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"2006/01/02 15:04",
	"2006/01/02",
	"20060102150405",
	"20060102",
}

func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("时间为空")
	}
	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("时间格式不对: %s", s)
}

// direction 判定收支；第二个返回值 false 表示「这条不该记账」（不计收支/退款/关闭）
func directionOf(source Source, dirCell string, amountCents int64, statusCell string) (int, bool) {
	dir := strings.TrimSpace(dirCell)
	status := strings.TrimSpace(statusCell)

	switch {
	case strings.Contains(dir, "不计收支"):
		return 0, false
	case strings.Contains(dir, "支出"), dir == "借", strings.Contains(dir, "借"):
		return 0, true
	case strings.Contains(dir, "收入"), strings.HasPrefix(dir, "贷"), strings.Contains(dir, "贷"):
		return 1, true
	}

	// 没有收支列时退化为看金额正负（银行流水常见）
	if amountCents < 0 {
		return 0, true
	}

	// 再退化为看状态
	if strings.Contains(status, "退款") && strings.Contains(status, "成功") {
		return 1, true
	}
	return 0, true
}

func shouldSkipStatus(status string) bool {
	s := strings.TrimSpace(status)
	switch {
	case s == "":
		return false
	// 只跳过「没真正发生」的交易。
	// 退款不跳过：账单里原交易（支出）和退款（收入）是两行，
	// 都保留才能让净额正确；只留一行反而会把账算歪。
	case strings.Contains(s, "关闭"), strings.Contains(s, "失败"),
		strings.Contains(s, "已撤销"), strings.Contains(s, "撤销成功"):
		return true
	default:
		return false
	}
}

func fallbackID(occurred time.Time, amount int64, counterparty, item string) string {
	raw := fmt.Sprintf("%d|%d|%s|%s", occurred.UnixMilli(), amount, counterparty, item)
	sum := sha1.Sum([]byte(raw))
	return "auto-" + hex.EncodeToString(sum[:])[:16]
}

// ParseCSV 解析账单 CSV（自动识别来源）。
func ParseCSV(data []byte, fileName string) (*Result, error) {
	text, err := decodeText(data)
	if err != nil {
		return nil, err
	}
	rows, err := readRows(text)
	if err != nil {
		return nil, fmt.Errorf("CSV 解析失败: %w", err)
	}

	src := detectSource(text, fileName)
	return parseRows(rows, src)
}

// parseRows 是 CSV / xlsx 共用的核心：定位表头、逐行映射、排除不计收支。
func parseRows(rows [][]string, src Source) (*Result, error) {
	p := profileFor(src)
	res := &Result{Source: src}

	headerIdx, cols, _ := locate(rows, p, res)
	if headerIdx < 0 {
		return nil, fmt.Errorf("没找到表头（需要至少包含「交易时间」和「金额」两列）")
	}

	for i, row := range rows[headerIdx+1:] {
		lineNo := headerIdx + 2 + i
		if len(row) == 0 {
			continue
		}
		timeCell := cell(row, cols[fTime])
		amountCell := cell(row, cols[fAmount])
		if timeCell == "" && amountCell == "" {
			continue // 说明行/空行
		}

		occurred, err := parseTime(timeCell)
		if err != nil {
			// xlsx 里的时间常被存成 Excel 序列号（如 46326.7334 = 2026-11-01 17:36）
			if serial, ok := parseExcelSerial(timeCell); ok {
				occurred, err = serial, nil
			}
		}
		if err != nil {
			res.skip(lineNo, "时间无法识别: %q", timeCell)
			continue
		}
		amount, err := parseAmount(amountCell)
		if err != nil {
			res.skip(lineNo, "金额无法识别: %q", amountCell)
			continue
		}

		status := cell(row, cols[fStatus])
		if shouldSkipStatus(status) {
			res.skip(lineNo, "状态为 %q（退款/关闭）", status)
			continue
		}

		dirCell := cell(row, cols[fDirection])
		if cols[fDirection] >= 0 && dirCell == "/" {
			res.skip(lineNo, "收/支为空（中性交易）")
			continue
		}
		dir, ok := directionOf(src, dirCell, amount, status)
		if !ok {
			res.skip(lineNo, "收/支为 %q（不计收支）", dirCell)
			continue
		}

		if amount < 0 {
			amount = -amount
		}
		if amount == 0 {
			res.skip(lineNo, "金额为 0")
			continue
		}

		counterparty := cell(row, cols[fCounterparty])
		item := cell(row, cols[fItem])
		externalID := cell(row, cols[fExternal])
		if externalID == "" {
			externalID = fallbackID(occurred, amount, counterparty, item)
		}

		raw := make(map[string]string, len(res.Headers))
		for i, h := range res.Headers {
			if i < len(row) {
				raw[normalizeHeader(h)] = row[i]
			}
		}

		res.Records = append(res.Records, Record{
			ExternalID:   externalID,
			OccurredAt:   occurred,
			Direction:    dir,
			AmountCents:  amount,
			Counterparty: counterparty,
			Item:         item,
			Method:       cell(row, cols[fMethod]),
			Status:       status,
			Raw:          raw,
		})
	}

	if len(res.Records) == 0 {
		res.addWarning("没有解析出任何账目，可能是文件格式变化了")
	}
	return res, nil
}

func profileFor(src Source) profile {
	for _, p := range profiles {
		if p.source == src {
			return p
		}
	}
	return profiles[len(profiles)-1]
}

func detectSource(text, fileName string) Source {
	lower := strings.ToLower(fileName)
	switch {
	case strings.Contains(lower, "微信"), strings.Contains(lower, "wechat"):
		return SourceWeChat
	case strings.Contains(lower, "alipay"), strings.Contains(lower, "支付宝"):
		return SourceAlipay
	case strings.Contains(lower, "云闪付"), strings.Contains(lower, "unionpay"):
		return SourceUnionPay
	}

	head := text
	if len(head) > 4000 {
		head = head[:4000]
	}
	switch {
	case strings.Contains(head, "微信支付") || strings.Contains(head, "商户单号"):
		return SourceWeChat
	case strings.Contains(head, "支付宝") || strings.Contains(head, "商家订单号"):
		return SourceAlipay
	case strings.Contains(head, "云闪付") || strings.Contains(head, "银联"):
		return SourceUnionPay
	}
	return SourceBank
}

// parseExcelSerial 解析 Excel 的日期序列号（1899-12-30 起的天数）。
// 微信导出的 xlsx 把「交易时间」存成这种数字，例如 46326.7334 -> 2026-11-01 17:36。
// 微信/支付宝账单的时间都按 UTC+8，这里固定用 +08:00，避免服务器时区影响结果。
func parseExcelSerial(s string) (time.Time, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v <= 0 || v > 100000 {
		return time.Time{}, false
	}
	days := math.Floor(v)
	frac := v - days
	loc := time.FixedZone("CST", 8*3600)
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, loc)
	seconds := int64(math.Round(frac * 86400))
	return base.AddDate(0, 0, int(days)).Add(time.Duration(seconds) * time.Second), true
}
