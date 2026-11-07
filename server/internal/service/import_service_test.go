package service_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"family-ledger/server/internal/service"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func serviceListAll() service.ListOptions { return service.ListOptions{Limit: 100} }

func mustCategoryID(t *testing.T, env *ledgerEnv, name string) string {
	t.Helper()
	cats, err := env.db.CategoriesOfFamily(context.Background(), env.db.SQL(), env.familyID)
	if err != nil {
		t.Fatalf("读取分类失败: %v", err)
	}
	for _, c := range cats {
		if c.Name == name {
			return c.ID
		}
	}
	t.Fatalf("没找到分类 %s", name)
	return ""
}

const importCSV = `微信支付账单明细,,,,,,,,,,
----------------------微信支付账单明细列表--------------------,,,,,,,,,,
交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号,备注
2026-10-05 12:30:45,商户消费,美团,午餐,支出,￥28.50,零钱,支付成功,WX-001,1001,/
2026-10-06 18:10:00,商户消费,星巴克,咖啡,支出,￥33.00,零钱通,支付成功,WX-002,1002,/
2026-10-07 09:00:00,转账,张三,转账,不计收支,￥200.00,零钱,朋友已收钱,WX-003,0,/
2026-10-09 08:00:00,转账,李四,红包,收入,￥88.00,零钱,已存入零钱,WX-004,0,/
`

func toGBK(t *testing.T, s string) []byte {
	t.Helper()
	out, err := io.ReadAll(transform.NewReader(bytes.NewBufferString(s), simplifiedchinese.GBK.NewEncoder()))
	if err != nil {
		t.Fatalf("GBK 编码失败: %v", err)
	}
	return out
}

// 导入：解析、跳过不计收支、重复导入不产生重复账目
func TestImportBillDeduplicates(t *testing.T) {
	env := newLedgerEnv(t)
	ctx := context.Background()
	data := toGBK(t, importCSV)

	first, err := env.ledger.ImportBill(ctx, env.familyID, env.ownerID, "微信支付账单.csv", data, "")
	if err != nil {
		t.Fatalf("首次导入失败: %v", err)
	}
	if first.Total != 3 {
		t.Fatalf("应解析出 3 条，实际 %d", first.Total)
	}
	if first.Imported != 3 {
		t.Fatalf("应导入 3 条，实际 %d", first.Imported)
	}
	if first.Skipped != 1 {
		t.Fatalf("应跳过 1 条不计收支，实际 %d", first.Skipped)
	}

	// 同一份文件再导一次：全部命中去重
	second, err := env.ledger.ImportBill(ctx, env.familyID, env.ownerID, "微信支付账单.csv", data, "")
	if err != nil {
		t.Fatalf("二次导入失败: %v", err)
	}
	if second.Imported != 0 {
		t.Fatalf("二次导入不应新增，实际 %d", second.Imported)
	}
	if second.Duplicated != 3 {
		t.Fatalf("二次导入应全部判为重复，实际 %d", second.Duplicated)
	}

	list, err := env.ledger.List(ctx, env.familyID, env.ownerID, serviceListAll())
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("库里应只有 3 条，实际 %d", len(list))
	}
	for _, tx := range list {
		if tx.Source != "wechat" {
			t.Fatalf("来源应标记为 wechat，实际 %s", tx.Source)
		}
		if tx.ExternalID == "" {
			t.Fatal("外部单号不能为空")
		}
	}
}

// 普通成员不能导入（避免成员把家庭账本刷乱）
func TestImportRequiresAdminRole(t *testing.T) {
	env := newLedgerEnv(t)
	ctx := context.Background()
	if _, err := env.ledger.ImportBill(ctx, env.familyID, env.memberID, "x.csv", toGBK(t, importCSV), ""); err == nil {
		t.Fatal("普通成员导入应被拒绝")
	}
}

// 分类规则：命中关键词的账目自动落到指定分类
func TestCategoryRuleApplied(t *testing.T) {
	env := newLedgerEnv(t)
	ctx := context.Background()

	// 把「美团」映射到交通分类（故意跟直觉不同，用来证明规则生效）
	if err := env.ledger.SaveCategoryRule(ctx, env.familyID, env.ownerID, "美团", mustCategoryID(t, env, "交通")); err != nil {
		t.Fatalf("保存规则失败: %v", err)
	}

	if _, err := env.ledger.ImportBill(ctx, env.familyID, env.ownerID, "w.csv", toGBK(t, importCSV), ""); err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	list, err := env.ledger.List(ctx, env.familyID, env.ownerID, serviceListAll())
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	trafficID := mustCategoryID(t, env, "交通")
	found := false
	for _, tx := range list {
		if strings.HasPrefix(tx.Note, "美团") {
			found = true
			if tx.CategoryID != trafficID {
				t.Fatalf("美团应被规则映射到交通，实际分类 %s", tx.CategoryID)
			}
		}
	}
	if !found {
		t.Fatal("没找到美团的账目")
	}
}
