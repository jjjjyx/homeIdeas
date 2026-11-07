package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"time"

	"family-ledger/server/internal/domain"
)

// ExportCSV 导出 CSV（带 UTF-8 BOM，Excel 直接打开不乱码）。
func (s *LedgerService) ExportCSV(ctx context.Context, familyID, userID string, fromMS, toMS int64) ([]byte, error) {
	if _, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID); err != nil {
		return nil, err
	}
	if toMS <= 0 {
		toMS = time.Now().AddDate(0, 1, 0).UnixMilli()
	}
	if fromMS <= 0 {
		fromMS = time.Date(2000, 1, 1, 0, 0, 0, 0, time.Local).UnixMilli()
	}
	if toMS <= fromMS {
		return nil, domain.ErrBadRequest.WithMessage("结束时间必须晚于开始时间")
	}

	rows, err := s.db.ExportTransactions(ctx, s.db.SQL(), familyID, userID, fromMS, toMS)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString("\uFEFF") // Excel 识别 UTF-8 的 BOM
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"日期", "类型", "分类", "账户", "金额", "备注", "记账人", "可见性"})
	for _, r := range rows {
		kind := "支出"
		if r.Type == domain.TypeIncome {
			kind = "收入"
		}
		visibility := "家庭"
		if r.Visibility == domain.VisibilityPrivate {
			visibility = "仅自己"
		}
		_ = w.Write([]string{
			time.UnixMilli(r.OccurredAt).Format("2006-01-02"),
			kind,
			r.CategoryName,
			r.AccountName,
			fmt.Sprintf("%.2f", float64(r.AmountCents)/100),
			r.Note,
			r.MemberName,
			visibility,
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
