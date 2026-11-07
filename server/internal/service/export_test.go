package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/service"
)

func serviceTx(typ int, amount int64, categoryID, note, visibility string) service.TxInput {
	return service.TxInput{
		Type:        typ,
		AmountCents: amount,
		CategoryID:  categoryID,
		OccurredAt:  time.Now().UnixMilli(),
		Note:        note,
		Visibility:  visibility,
	}
}

// CSV 导出：表头、金额格式、私密账过滤。
func TestExportCSV(t *testing.T) {
	env := newLedgerEnv(t)
	ctx := context.Background()

	env.newTx(t, domain.TypeExpense, 12850, env.catFood)
	if _, err := env.ledger.Create(ctx, env.familyID, env.ownerID, serviceTx(domain.TypeExpense, 9900, env.catFood, "私房钱", domain.VisibilityPrivate)); err != nil {
		t.Fatalf("创建私密账失败: %v", err)
	}

	from := time.Now().AddDate(0, 0, -1).UnixMilli()
	to := time.Now().AddDate(0, 0, 1).UnixMilli()

	data, err := env.ledger.ExportCSV(ctx, env.familyID, env.ownerID, from, to)
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "\uFEFF") {
		t.Fatal("CSV 应带 UTF-8 BOM，否则 Excel 打开中文乱码")
	}
	if !strings.Contains(text, "日期,类型,分类,账户,金额,备注,记账人,可见性") {
		t.Fatalf("表头不符合预期:\n%s", firstLine(text))
	}
	if !strings.Contains(text, "128.50") {
		t.Fatal("金额应格式化为 128.50")
	}
	if !strings.Contains(text, "私房钱") {
		t.Fatal("本人导出应包含自己的私密账")
	}

	// 家人导出不应包含别人的私密账
	memberData, err := env.ledger.ExportCSV(ctx, env.familyID, env.memberID, from, to)
	if err != nil {
		t.Fatalf("成员导出失败: %v", err)
	}
	if strings.Contains(string(memberData), "私房钱") {
		t.Fatal("成员导出不应包含别人的私密账")
	}
	if !strings.Contains(string(memberData), "128.50") {
		t.Fatal("成员导出应包含家庭可见的账")
	}
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}
