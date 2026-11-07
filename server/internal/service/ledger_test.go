package service_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"family-ledger/server/internal/config"
	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/pkg/id"
	"family-ledger/server/internal/service"
	"family-ledger/server/internal/store"
)

type ledgerEnv struct {
	db        *store.DB
	auth      *service.AuthService
	families  *service.FamilyService
	ledger    *service.LedgerService
	familyID  string
	ownerID   string
	memberID  string
	catFood   string
	catSalary string
}

func newLedgerEnv(t *testing.T) *ledgerEnv {
	t.Helper()
	ctx := context.Background()

	db, err := store.Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	cfg := &config.Config{
		Env: "test", JWTSecret: "unit-test-secret-0123456789abcdef",
		AccessTTL: 15 * time.Minute, RefreshTTL: 24 * time.Hour,
	}
	auth := service.NewAuthService(db, cfg)
	families := service.NewFamilyService(db)
	ledger := service.NewLedgerService(db)

	owner, err := auth.Register(ctx, service.RegisterInput{Account: "papa", Password: testPassword, Nickname: "爸爸"})
	if err != nil {
		t.Fatalf("注册户主失败: %v", err)
	}
	card, err := families.Create(ctx, owner.UserID, "我们的家", "")
	if err != nil {
		t.Fatalf("建房失败: %v", err)
	}
	inv, err := families.CreateInvite(ctx, owner.UserID, card.ID, domain.RoleMember, 24, "")
	if err != nil {
		t.Fatalf("邀请失败: %v", err)
	}
	member, err := auth.Register(ctx, service.RegisterInput{
		Account: "mama", Password: testPassword, Nickname: "妈妈", InviteCode: inv.Code,
	})
	if err != nil {
		t.Fatalf("成员注册失败: %v", err)
	}

	cats, err := db.CategoriesOfFamily(ctx, db.SQL(), card.ID)
	if err != nil {
		t.Fatalf("读取分类失败: %v", err)
	}
	env := &ledgerEnv{
		db: db, auth: auth, families: families, ledger: ledger,
		familyID: card.ID, ownerID: owner.UserID, memberID: member.UserID,
	}
	for _, c := range cats {
		switch c.Name {
		case "餐饮":
			env.catFood = c.ID
		case "工资":
			env.catSalary = c.ID
		}
	}
	if env.catFood == "" || env.catSalary == "" {
		t.Fatal("默认分类未正确种入")
	}
	return env
}

func (e *ledgerEnv) newTx(t *testing.T, typ int, amount int64, cat string) *domain.Transaction {
	t.Helper()
	tx, err := e.ledger.Create(context.Background(), e.familyID, e.ownerID, service.TxInput{
		Type: typ, AmountCents: amount, CategoryID: cat, OccurredAt: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("记账失败: %v", err)
	}
	return tx
}

func TestLedgerCreateAndStats(t *testing.T) {
	env := newLedgerEnv(t)
	ctx := context.Background()

	env.newTx(t, domain.TypeExpense, 10000, env.catFood)
	env.newTx(t, domain.TypeExpense, 5000, env.catFood)
	env.newTx(t, domain.TypeIncome, 860000, env.catSalary)

	summary, err := env.ledger.Summary(ctx, env.familyID, env.ownerID, "")
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if summary.ExpenseCents != 15000 {
		t.Fatalf("支出应为 15000，实际 %d", summary.ExpenseCents)
	}
	if summary.IncomeCents != 860000 {
		t.Fatalf("收入应为 860000，实际 %d", summary.IncomeCents)
	}
	if summary.Count != 3 {
		t.Fatalf("笔数应为 3，实际 %d", summary.Count)
	}

	stats, err := env.ledger.CategoryStats(ctx, env.familyID, env.ownerID, "", domain.TypeExpense)
	if err != nil {
		t.Fatalf("分类统计失败: %v", err)
	}
	if len(stats) != 1 || stats[0].AmountCents != 15000 {
		t.Fatalf("分类统计不符合预期: %+v", stats)
	}
	if stats[0].Ratio < 0.99 || stats[0].Ratio > 1.01 {
		t.Fatalf("占比应为 100%%，实际 %f", stats[0].Ratio)
	}

	trend, err := env.ledger.Trend(ctx, env.familyID, env.ownerID, 3, domain.TypeExpense)
	if err != nil {
		t.Fatalf("趋势失败: %v", err)
	}
	if len(trend) != 3 || trend[2].AmountCents != 15000 {
		t.Fatalf("趋势数据不符合预期: %+v", trend)
	}
}

// 私密账只有本人可见：列表和同步都要过滤。
func TestPrivateTransactionIsolation(t *testing.T) {
	env := newLedgerEnv(t)
	ctx := context.Background()

	if _, err := env.ledger.Create(ctx, env.familyID, env.ownerID, service.TxInput{
		Type: domain.TypeExpense, AmountCents: 8800, CategoryID: env.catFood,
		OccurredAt: time.Now().UnixMilli(), Note: "私房钱", Visibility: domain.VisibilityPrivate,
	}); err != nil {
		t.Fatalf("创建私密账失败: %v", err)
	}

	ownerList, err := env.ledger.List(ctx, env.familyID, env.ownerID, service.ListOptions{})
	if err != nil {
		t.Fatalf("户主列表失败: %v", err)
	}
	if len(ownerList) != 1 {
		t.Fatalf("户主应看到 1 条，实际 %d", len(ownerList))
	}

	memberList, err := env.ledger.List(ctx, env.familyID, env.memberID, service.ListOptions{})
	if err != nil {
		t.Fatalf("成员列表失败: %v", err)
	}
	if len(memberList) != 0 {
		t.Fatalf("成员不应看到私密账，实际 %d 条", len(memberList))
	}

	pull, err := env.ledger.Pull(ctx, env.familyID, env.memberID, 0, 100)
	if err != nil {
		t.Fatalf("同步拉取失败: %v", err)
	}
	if len(pull.Transactions) != 0 {
		t.Fatalf("同步也不应该下发给成员，实际 %d 条", len(pull.Transactions))
	}
}

// 同步的核心行为：游标递增、增量只拉变化、冲突返回 server 版本、删除是墓碑。
func TestSyncPullPushFlow(t *testing.T) {
	env := newLedgerEnv(t)
	ctx := context.Background()

	txID := id.New()
	ops := []service.SyncOpInput{{
		ClientOpID: "op-create",
		Entity:     "transaction",
		Op:         "upsert",
		Payload: service.TxInput{
			ID: txID, Type: domain.TypeExpense, AmountCents: 2850, CategoryID: env.catFood,
			OccurredAt: time.Now().UnixMilli(), Note: "打车",
		},
	}}
	results, err := env.ledger.Push(ctx, env.familyID, env.ownerID, ops)
	if err != nil {
		t.Fatalf("推送失败: %v", err)
	}
	if results[0].Status != "applied" || results[0].Rev <= 0 {
		t.Fatalf("推送结果不符合预期: %+v", results[0])
	}
	firstRev := results[0].Rev

	// 全量拉取
	pull, err := env.ledger.Pull(ctx, env.familyID, env.ownerID, 0, 100)
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if len(pull.Transactions) != 1 || pull.Transactions[0].ID != txID {
		t.Fatalf("拉取结果不符合预期: %+v", pull.Transactions)
	}
	if pull.Cursor < firstRev {
		t.Fatalf("cursor 应 >= 最新 rev")
	}

	// 增量：游标之后的没有变化
	empty, err := env.ledger.Pull(ctx, env.familyID, env.ownerID, pull.Cursor, 100)
	if err != nil {
		t.Fatalf("增量拉取失败: %v", err)
	}
	if len(empty.Transactions) != 0 {
		t.Fatalf("无变化时不应返回账单，实际 %d", len(empty.Transactions))
	}

	// 版本冲突
	conflictOps := []service.SyncOpInput{{
		ClientOpID: "op-stale", Entity: "transaction", Op: "upsert", BaseRev: firstRev - 1,
		Payload: service.TxInput{
			ID: txID, Type: domain.TypeExpense, AmountCents: 9999, CategoryID: env.catFood,
			OccurredAt: time.Now().UnixMilli(),
		},
	}}
	cResults, _ := env.ledger.Push(ctx, env.familyID, env.ownerID, conflictOps)
	if cResults[0].Status != "conflict" || cResults[0].Server == nil {
		t.Fatalf("应判定为冲突并返回服务端版本: %+v", cResults[0])
	}

	// 正确版本 → 覆盖
	okOps := make([]service.SyncOpInput, 1)
	okOps[0] = conflictOps[0]
	okOps[0].ClientOpID = "op-update"
	okOps[0].BaseRev = firstRev
	okResults, _ := env.ledger.Push(ctx, env.familyID, env.ownerID, okOps)
	if okResults[0].Status != "applied" {
		t.Fatalf("修正版本后应成功: %+v", okResults[0])
	}

	// 删除 → 墓碑
	delResults, _ := env.ledger.Push(ctx, env.familyID, env.ownerID, []service.SyncOpInput{{
		ClientOpID: "op-delete", Entity: "transaction", Op: "delete",
		Payload: service.TxInput{ID: txID},
	}})
	if delResults[0].Status != "applied" {
		t.Fatalf("删除应成功: %+v", delResults[0])
	}
	afterDel, _ := env.ledger.Pull(ctx, env.familyID, env.ownerID, okResults[0].Rev, 100)
	if len(afterDel.Transactions) != 1 || afterDel.Transactions[0].DeletedAt == nil {
		t.Fatalf("删除应下发墓碑，实际 %+v", afterDel.Transactions)
	}
}

// 普通成员不能改别人的账。
func TestMemberCannotModifyOthersEntry(t *testing.T) {
	env := newLedgerEnv(t)
	ctx := context.Background()

	tx := env.newTx(t, domain.TypeExpense, 10000, env.catFood)

	if _, err := env.ledger.Update(ctx, env.familyID, env.memberID, tx.ID, service.TxInput{
		Type: domain.TypeExpense, AmountCents: 1, CategoryID: env.catFood,
	}); err == nil {
		t.Fatal("成员不应该能修改户主记的账")
	}

	results, err := env.ledger.Push(ctx, env.familyID, env.memberID, []service.SyncOpInput{{
		ClientOpID: "op-hack", Entity: "transaction", Op: "delete",
		Payload: service.TxInput{ID: tx.ID},
	}})
	if err != nil {
		t.Fatalf("推送失败: %v", err)
	}
	if results[0].Status != "rejected" {
		t.Fatalf("成员删除他人账目应被拒绝，实际 %s", results[0].Status)
	}

	if err := env.ledger.Delete(ctx, env.familyID, env.memberID, tx.ID); err == nil {
		t.Fatal("成员不应该能删除户主记的账")
	}
}

// 成员离线推送的账，户主下一次增量拉取必须能看到。
func TestSyncPushVisibleToOtherMember(t *testing.T) {
	env := newLedgerEnv(t)
	ctx := context.Background()

	// 先拿一个基准游标
	base, err := env.ledger.Pull(ctx, env.familyID, env.ownerID, 0, 100)
	if err != nil {
		t.Fatalf("基准拉取失败: %v", err)
	}

	txID := id.New()
	results, err := env.ledger.Push(ctx, env.familyID, env.memberID, []service.SyncOpInput{{
		ClientOpID: "m1", Entity: "transaction", Op: "upsert",
		Payload: service.TxInput{
			ID: txID, Type: domain.TypeExpense, AmountCents: 3000,
			CategoryID: env.catFood, OccurredAt: time.Now().UnixMilli(), Note: "离线记的",
		},
	}})
	if err != nil {
		t.Fatalf("推送失败: %v", err)
	}
	if results[0].Status != "applied" {
		t.Fatalf("推送应成功，实际 %s (%s)", results[0].Status, results[0].Reason)
	}

	pull, err := env.ledger.Pull(ctx, env.familyID, env.ownerID, base.Cursor, 100)
	if err != nil {
		t.Fatalf("增量拉取失败: %v", err)
	}
	found := false
	for _, tx := range pull.Transactions {
		if tx.ID == txID {
			found = true
			if tx.MemberID != env.memberID {
				t.Fatalf("记账人应为成员，实际 %s", tx.MemberID)
			}
		}
	}
	if !found {
		t.Fatalf("户主应能拉到成员推送的账，本次返回 %d 条，base.Cursor=%d, 新 rev=%d",
			len(pull.Transactions), base.Cursor, results[0].Rev)
	}
}
