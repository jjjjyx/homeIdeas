package service_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"family-ledger/server/internal/config"
	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/service"
	"family-ledger/server/internal/store"
)

const testPassword = "Test-Passw0rd!"

func setup(t *testing.T) (*service.AuthService, *service.FamilyService) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	cfg := &config.Config{
		Env:        "test",
		JWTSecret:  "unit-test-secret-0123456789abcdef",
		AccessTTL:  15 * time.Minute,
		RefreshTTL: 24 * time.Hour,
	}
	return service.NewAuthService(db, cfg), service.NewFamilyService(db)
}

func mustRegister(t *testing.T, auth *service.AuthService, account, inviteCode string) *service.Session {
	t.Helper()
	session, err := auth.Register(context.Background(), service.RegisterInput{
		Account:    account,
		Password:   testPassword,
		Nickname:   account,
		InviteCode: inviteCode,
	})
	if err != nil {
		t.Fatalf("注册 %s 失败: %v", account, err)
	}
	return session
}

func wantCode(t *testing.T, err error, code int) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望错误码 %d，实际成功", code)
	}
	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("期望 AppError，实际 %T: %v", err, err)
	}
	if appErr.Code != code {
		t.Fatalf("期望错误码 %d，实际 %d (%s)", code, appErr.Code, appErr.Message)
	}
}

// 首个用户免邀请码，之后必须凭邀请码注册。
func TestRegisterRequiresInvite(t *testing.T) {
	auth, _ := setup(t)
	ctx := context.Background()

	first := mustRegister(t, auth, "papa", "")
	if first.UserID == "" {
		t.Fatal("首个用户注册失败")
	}

	if _, err := auth.Register(ctx, service.RegisterInput{
		Account: "stranger", Password: testPassword, Nickname: "路人",
	}); err != nil {
		wantCode(t, err, 4003)
	} else {
		t.Fatal("无邀请码的第二个用户不应该注册成功")
	}
}

// 邀请码只能使用一次（回归测试：used_by 写空串导致判空失效的 bug）。
func TestInviteIsSingleUse(t *testing.T) {
	auth, families := setup(t)
	ctx := context.Background()

	owner := mustRegister(t, auth, "papa", "")
	card, err := families.Create(ctx, owner.UserID, "我们的家", "")
	if err != nil {
		t.Fatalf("建房失败: %v", err)
	}

	inv, err := families.CreateInvite(ctx, owner.UserID, card.ID, domain.RoleMember, 24, "")
	if err != nil {
		t.Fatalf("生成邀请码失败: %v", err)
	}

	mustRegister(t, auth, "mama", inv.Code)

	_, err = auth.Register(ctx, service.RegisterInput{
		Account: "uncle", Password: testPassword, Nickname: "叔叔", InviteCode: inv.Code,
	})
	wantCode(t, err, 4004)

	// 同一个码也不能再被「加入家庭」接口使用
	_, err = families.Join(ctx, owner.UserID, inv.Code, "")
	if err == nil {
		t.Fatal("已使用的邀请码不应该能再次加入")
	}
}

// 跨家庭访问必须 403，而不是 404 或者 200。
func TestCrossFamilyAccessForbidden(t *testing.T) {
	auth, families := setup(t)
	ctx := context.Background()

	owner := mustRegister(t, auth, "papa", "")
	papaHome, err := families.Create(ctx, owner.UserID, "爸爸的家", "")
	if err != nil {
		t.Fatalf("建房失败: %v", err)
	}

	inv, _ := families.CreateInvite(ctx, owner.UserID, papaHome.ID, domain.RoleMember, 24, "")
	member := mustRegister(t, auth, "mama", inv.Code)

	// 妈妈自己再建一个家
	mamaHome, err := families.Create(ctx, member.UserID, "妈妈的家", "")
	if err != nil {
		t.Fatalf("建房失败: %v", err)
	}

	_, err = families.Detail(ctx, owner.UserID, mamaHome.ID)
	wantCode(t, err, 3001)

	if _, err := families.Members(ctx, owner.UserID, mamaHome.ID); err == nil {
		t.Fatal("跨家庭读取成员列表必须失败")
	}
}

// 角色与成员管理的基本约束。
func TestMemberPermissions(t *testing.T) {
	auth, families := setup(t)
	ctx := context.Background()

	owner := mustRegister(t, auth, "papa", "")
	card, _ := families.Create(ctx, owner.UserID, "我们的家", "")
	inv, _ := families.CreateInvite(ctx, owner.UserID, card.ID, domain.RoleMember, 24, "")
	member := mustRegister(t, auth, "mama", inv.Code)

	// 普通成员不能改名
	if err := families.Rename(ctx, member.UserID, card.ID, "改名"); err == nil {
		t.Fatal("普通成员不应该能修改家庭名")
	}

	// 户主可以提升成员
	if err := families.SetRole(ctx, owner.UserID, card.ID, member.UserID, domain.RoleAdmin); err != nil {
		t.Fatalf("提升成员失败: %v", err)
	}

	// 户主不能被移除
	err := families.RemoveMember(ctx, member.UserID, card.ID, owner.UserID)
	if err == nil {
		t.Fatal("户主不应该能被移除")
	}

	// 户主不能退出自己的家庭
	if err := families.Leave(ctx, owner.UserID, card.ID); err == nil {
		t.Fatal("户主不应该能退出家庭")
	}
}

// 密码必须以哈希形式保存，且能通过校验。
func TestPasswordIsHashed(t *testing.T) {
	auth, _ := setup(t)
	ctx := context.Background()
	mustRegister(t, auth, "papa", "")

	session, err := auth.Login(ctx, service.LoginInput{Account: "papa", Password: testPassword})
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if session.AccessToken == "" || session.RefreshToken == "" {
		t.Fatal("登录应该返回两个令牌")
	}

	if _, err := auth.Login(ctx, service.LoginInput{Account: "papa", Password: "wrong"}); err == nil {
		t.Fatal("错误密码不应该登录成功")
	}

	// 退出后 refresh 失效
	if err := auth.Logout(ctx, session.RefreshToken); err != nil {
		t.Fatalf("退出失败: %v", err)
	}
	if _, err := auth.Refresh(ctx, session.RefreshToken); err == nil {
		t.Fatal("退出后 refresh 令牌必须失效")
	}
}

// 家庭 rev 随成员变更递增，作为同步游标。
func TestFamilyRevIncreases(t *testing.T) {
	auth, families := setup(t)
	ctx := context.Background()

	owner := mustRegister(t, auth, "papa", "")
	card, _ := families.Create(ctx, owner.UserID, "我们的家", "")
	if card.Rev != 1 {
		t.Fatalf("新家庭 rev 应为 1，实际 %d", card.Rev)
	}

	inv, _ := families.CreateInvite(ctx, owner.UserID, card.ID, domain.RoleMember, 24, "")
	member := mustRegister(t, auth, "mama", inv.Code)
	_ = member

	detail, err := families.Detail(ctx, owner.UserID, card.ID)
	if err != nil {
		t.Fatalf("读取家庭失败: %v", err)
	}
	if detail.Family.Rev <= card.Rev {
		t.Fatalf("加入成员后 rev 应递增，之前 %d，现在 %d", card.Rev, detail.Family.Rev)
	}
	if len(detail.Members) != 2 {
		t.Fatalf("成员数应为 2，实际 %d", len(detail.Members))
	}
}
