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

func setup(t *testing.T) (*service.AuthService, *service.FamilyService, *store.DB) {
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
	return service.NewAuthService(db, cfg), service.NewFamilyService(db), db
}

func mustRegister(t *testing.T, auth *service.AuthService, account string) *service.Session {
	t.Helper()
	session, err := auth.Register(context.Background(), service.RegisterInput{
		Account:  account,
		Password: testPassword,
		Nickname: account,
	})
	if err != nil {
		t.Fatalf("初始化 %s 失败: %v", account, err)
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

// 单家庭：首次注册创建唯一家庭和户主；之后不再允许自助注册。
func TestBootstrapCreatesSingleFamily(t *testing.T) {
	auth, families, db := setup(t)
	ctx := context.Background()

	owner := mustRegister(t, auth, "papa")
	if owner.UserID == "" || owner.Role != domain.RoleOwner {
		t.Fatalf("首个用户应成为户主，实际 %+v", owner)
	}

	family, err := db.PrimaryFamily(ctx, db.SQL())
	if err != nil {
		t.Fatalf("应创建唯一家庭: %v", err)
	}
	if family.OwnerID != owner.UserID {
		t.Fatalf("家庭户主应为初始化用户")
	}

	detail, err := families.Detail(ctx, owner.UserID)
	if err != nil {
		t.Fatalf("读取家庭失败: %v", err)
	}
	if len(detail.Members) != 1 || detail.MyRole != domain.RoleOwner {
		t.Fatalf("家庭详情不符合预期: %+v", detail)
	}

	if _, err := auth.Register(ctx, service.RegisterInput{
		Account: "stranger", Password: testPassword, Nickname: "路人",
	}); err != nil {
		wantCode(t, err, 4003)
	} else {
		t.Fatal("系统初始化后没有邀请码不应该能注册")
	}
}

// 邀请码是成员加入的主要方式：一次性、绑定唯一家庭。
func TestInviteRegistration(t *testing.T) {
	auth, families, _ := setup(t)
	ctx := context.Background()

	owner := mustRegister(t, auth, "papa")
	inv, err := families.CreateInvite(ctx, owner.UserID, domain.RoleMember, 24, "")
	if err != nil {
		t.Fatalf("生成邀请码失败: %v", err)
	}

	session, err := auth.Register(ctx, service.RegisterInput{
		Account: "mama", Password: testPassword, Nickname: "妈妈", InviteCode: inv.Code,
	})
	if err != nil {
		t.Fatalf("凭邀请码注册失败: %v", err)
	}
	if session.Role != domain.RoleMember {
		t.Fatalf("邀请码角色应为 member，实际 %s", session.Role)
	}

	detail, err := families.Detail(ctx, owner.UserID)
	if err != nil {
		t.Fatalf("读取家庭失败: %v", err)
	}
	if len(detail.Members) != 2 {
		t.Fatalf("成员数应为 2，实际 %d", len(detail.Members))
	}

	// 同一个码不能再用
	if _, err := auth.Register(ctx, service.RegisterInput{
		Account: "uncle", Password: testPassword, Nickname: "叔叔", InviteCode: inv.Code,
	}); err != nil {
		wantCode(t, err, 4004)
	} else {
		t.Fatal("已使用的邀请码不应该能再次注册")
	}
}

// 成员由户主/管理员直接创建，不再需要邀请码。
func TestAddMemberByOwner(t *testing.T) {
	auth, families, _ := setup(t)
	ctx := context.Background()

	owner := mustRegister(t, auth, "papa")
	before, err := families.Detail(ctx, owner.UserID)
	if err != nil {
		t.Fatalf("读取家庭失败: %v", err)
	}

	member, err := families.AddMember(ctx, owner.UserID, "mama", testPassword, "妈妈", domain.RoleMember)
	if err != nil {
		t.Fatalf("添加成员失败: %v", err)
	}
	if member.Role != domain.RoleMember || member.Nickname != "妈妈" {
		t.Fatalf("成员信息不符合预期: %+v", member)
	}

	after, err := families.Detail(ctx, owner.UserID)
	if err != nil {
		t.Fatalf("读取家庭失败: %v", err)
	}
	if len(after.Members) != 2 {
		t.Fatalf("成员数应为 2，实际 %d", len(after.Members))
	}
	if after.Family.Rev <= before.Family.Rev {
		t.Fatalf("加入成员后 rev 应递增，之前 %d，现在 %d", before.Family.Rev, after.Family.Rev)
	}

	// 账号重复要拒绝
	if _, err := families.AddMember(ctx, owner.UserID, "mama", testPassword, "妈妈2", domain.RoleMember); err == nil {
		t.Fatal("重复账号不应该能添加")
	}

	// 新成员可以正常登录
	if _, err := auth.Login(ctx, service.LoginInput{Account: "mama", Password: testPassword}); err != nil {
		t.Fatalf("成员登录失败: %v", err)
	}
}

// 角色与成员管理的基本约束。
func TestMemberPermissions(t *testing.T) {
	auth, families, _ := setup(t)
	ctx := context.Background()

	owner := mustRegister(t, auth, "papa")
	member, err := families.AddMember(ctx, owner.UserID, "mama", testPassword, "妈妈", domain.RoleMember)
	if err != nil {
		t.Fatalf("添加成员失败: %v", err)
	}

	// 普通成员不能改名、不能加人
	if err := families.Rename(ctx, member.UserID, "改名"); err == nil {
		t.Fatal("普通成员不应该能修改家庭名")
	}
	if _, err := families.AddMember(ctx, member.UserID, "uncle", testPassword, "叔叔", domain.RoleMember); err == nil {
		t.Fatal("普通成员不应该能添加成员")
	}

	// 户主可以提升成员
	if err := families.SetRole(ctx, owner.UserID, member.UserID, domain.RoleAdmin); err != nil {
		t.Fatalf("提升成员失败: %v", err)
	}

	// 户主不能被移除
	if err := families.RemoveMember(ctx, member.UserID, owner.UserID); err == nil {
		t.Fatal("户主不应该能被移除")
	}

	// 成员被移除后不能再读家庭
	if err := families.RemoveMember(ctx, owner.UserID, member.UserID); err != nil {
		t.Fatalf("移除成员失败: %v", err)
	}
	if _, err := families.Detail(ctx, member.UserID); err == nil {
		t.Fatal("被移除的成员不应该还能访问家庭")
	}
}

// 密码必须以哈希形式保存，且能通过校验。
func TestPasswordIsHashed(t *testing.T) {
	auth, _, _ := setup(t)
	ctx := context.Background()
	mustRegister(t, auth, "papa")

	session, err := auth.Login(ctx, service.LoginInput{Account: "papa", Password: testPassword})
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if session.AccessToken == "" || session.RefreshToken == "" {
		t.Fatal("登录应该返回两个令牌")
	}
	if session.Role != domain.RoleOwner {
		t.Fatalf("户主登录角色应为 owner，实际 %s", session.Role)
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
