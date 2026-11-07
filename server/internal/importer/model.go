// Package importer 解析各家导出的账单文件（CSV / 加密 zip）。
//
// 现实约束：微信、支付宝都不提供个人账单 API，只能由用户导出后再导入。
// 这些文件的共同特点：
//   - GBK 编码（不是 UTF-8）
//   - 前面若干行是"账单说明"，真正的表头在中间
//   - 金额带 ¥ 符号、千分位逗号
//   - "不计收支"的行（转账、还信用卡、买卖理财）必须排除，否则统计翻倍
package importer

import (
	"fmt"
	"time"
)

type Source string

const (
	SourceWeChat   Source = "wechat"
	SourceAlipay   Source = "alipay"
	SourceUnionPay Source = "unionpay"
	SourceBank     Source = "bank"
	SourceUnknown  Source = "unknown"
)

func (s Source) Label() string {
	switch s {
	case SourceWeChat:
		return "微信支付"
	case SourceAlipay:
		return "支付宝"
	case SourceUnionPay:
		return "云闪付"
	case SourceBank:
		return "银行卡"
	default:
		return "未知来源"
	}
}

// Record 解析出的一条账目（还没入库）
type Record struct {
	ExternalID   string // 交易单号，用于去重
	OccurredAt   time.Time
	Direction    int // 0 支出 1 收入
	AmountCents  int64
	Counterparty string // 交易对方
	Item         string // 商品/摘要
	Method       string // 支付方式/账户
	Status       string
	Raw          map[string]string
}

// Result 一次解析的结果
type Result struct {
	Source   Source
	Records  []Record
	Skipped  int      // 不计收支 / 退款 / 关闭 等被主动跳过的行数
	Warnings []string // 非致命问题，给用户看
	Headers  []string
	// SkipReasons 每一条被跳过记录的原因（最多留 20 条），排查「为什么少了很多账」用
	SkipReasons []string
}

func (r *Result) addWarning(format string, args ...any) {
	if len(r.Warnings) < 20 {
		r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
	}
}

func (r *Result) skip(row int, format string, args ...any) {
	r.Skipped++
	if len(r.SkipReasons) < 20 {
		r.SkipReasons = append(r.SkipReasons, fmt.Sprintf("第%d行: %s", row, fmt.Sprintf(format, args...)))
	}
}
