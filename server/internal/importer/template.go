package importer

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TemplateRecord 通用记账模板的一行。
// 这个格式是给「AI 智能体生成 CSV → 分享到 App」用的：
//
//	日期,类型,金额,分类,归属,备注
//	2026-11-02,支出,28.50,餐饮,家庭,好想来-零食
//
// 字段名做了大量别名，因为不同 AI 输出的表头写法不一样。
type TemplateRecord struct {
	OccurredAt   time.Time
	Direction    int // 0 支出 1 收入
	AmountCents  int64
	CategoryName string // 分类名，落库时映射成 id
	Scope        string // family / personal / transfer / unknown
	Note         string
	Raw          map[string]string
}

// TemplateHeader App 唯一接受的固定表头
const TemplateHeader = "日期,类型,金额,分类,归属,备注"

var templateAliases = map[string][]string{
	"date":     {"日期", "时间", "交易时间", "交易日期", "date", "time", "消费日期"},
	"type":     {"类型", "收支", "收/支", "方向", "type", "direction"},
	"amount":   {"金额", "金额(元)", "amount", "价格", "实付", "实付金额", "总价"},
	"category": {"分类", "类别", "category", "类型名称"},
	"scope":    {"归属", "范围", "scope", "家庭/个人"},
	"note":     {"备注", "说明", "商品", "名称", "note", "desc", "描述", "项目名称", "摘要"},
}

// IsTemplateCSV 判断是不是通用记账模板（至少要有日期和金额两列）
func IsTemplateCSV(data []byte) bool {
	text, err := decodeText(data)
	if err != nil {
		return false
	}
	rows, err := readRows(text)
	if err != nil || len(rows) == 0 {
		return false
	}
	idx, _ := locateTemplate(rows)
	return idx >= 0
}

func locateTemplate(rows [][]string) (int, map[string]int) {
	for i, row := range rows {
		if i > 30 {
			break
		}
		cols := map[string]int{}
		for col, cell := range row {
			h := normalizeHeader(cell)
			for field, aliases := range templateAliases {
				if _, ok := cols[field]; ok {
					continue
				}
				for _, alias := range aliases {
					if h == strings.ToLower(alias) || strings.Contains(h, strings.ToLower(alias)) {
						cols[field] = col
						break
					}
				}
			}
		}
		if _, ok := cols["date"]; ok {
			if _, ok := cols["amount"]; ok {
				return i, cols
			}
		}
	}
	return -1, nil
}

// ParseTemplate 解析通用记账模板 CSV。
func ParseTemplate(data []byte) ([]TemplateRecord, []string, error) {
	text, err := decodeText(data)
	if err != nil {
		return nil, nil, err
	}
	rows, err := readRows(text)
	if err != nil {
		return nil, nil, fmt.Errorf("CSV 解析失败: %w", err)
	}
	headerIdx, cols := locateTemplate(rows)
	if headerIdx < 0 {
		return nil, nil, fmt.Errorf("没找到「日期」和「金额」两列")
	}
	headers := rows[headerIdx]

	out := make([]TemplateRecord, 0, len(rows))
	for i, row := range rows[headerIdx+1:] {
		lineNo := headerIdx + 2 + i
		_ = lineNo
		if len(row) == 0 {
			continue
		}
		dateText := cell(row, cols["date"])
		amountText := cell(row, cols["amount"])
		if dateText == "" && amountText == "" {
			continue
		}

		occurred, err := parseTime(dateText)
		if err != nil {
			if serial, ok := parseExcelSerial(dateText); ok {
				occurred = serial
			} else {
				continue
			}
		}
		amount, err := parseAmount(amountText)
		if err != nil || amount == 0 {
			continue
		}
		if amount < 0 {
			amount = -amount
		}

		dir := 0
		switch strings.TrimSpace(cell(row, cols["type"])) {
		case "收入", "入", "in", "income":
			dir = 1
		case "", "支出", "出", "out", "expense":
			dir = 0
		default:
			// 类型列写了别的东西，退回看金额正负
			if strings.HasPrefix(strings.TrimSpace(amountText), "-") {
				dir = 0
			}
		}

		raw := make(map[string]string, len(headers))
		for j, h := range headers {
			if j < len(row) {
				raw[normalizeHeader(h)] = row[j]
			}
		}

		out = append(out, TemplateRecord{
			OccurredAt:   occurred,
			Direction:    dir,
			AmountCents:  amount,
			CategoryName: strings.TrimSpace(cell(row, cols["category"])),
			Scope:        normalizeScopeText(cell(row, cols["scope"])),
			Note:         strings.TrimSpace(cell(row, cols["note"])),
			Raw:          raw,
		})
	}
	if len(out) == 0 {
		return nil, headers, fmt.Errorf("没有解析出任何记录")
	}
	return out, headers, nil
}

// normalizeScopeText 把 AI 写的「家庭」「个人」「待定」映射成 scope
func normalizeScopeText(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "unknown"
	case strings.Contains(s, "家庭"), strings.Contains(s, "共同"), s == "family":
		return "family"
	case strings.Contains(s, "个人"), s == "personal":
		return "personal"
	case strings.Contains(s, "往来"), strings.Contains(s, "转账"), s == "transfer":
		return "transfer"
	default:
		return "unknown"
	}
}

var _ = strconv.Atoi
var _ = csv.ErrFieldCount

// DecodeText 把账单文件转成 UTF-8 文本并截断，供模型做列映射。
func DecodeText(data []byte, limit int) (string, error) {
	text, err := decodeText(data)
	if err != nil {
		return "", err
	}
	r := []rune(text)
	if limit > 0 && len(r) > limit {
		r = r[:limit]
	}
	return string(r), nil
}
