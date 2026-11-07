package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"family-ledger/server/internal/config"
	"family-ledger/server/internal/domain"
	"family-ledger/server/internal/importer"
	"family-ledger/server/internal/mailer"
	"family-ledger/server/internal/pkg/id"
	"family-ledger/server/internal/pkg/secretbox"
	"family-ledger/server/internal/store"
)

// MailService 账号邮箱与账单邮件的自动导入。
type MailService struct {
	db     *store.DB
	cfg    *config.Config
	ledger *LedgerService
}

func NewMailService(db *store.DB, cfg *config.Config, ledger *LedgerService) *MailService {
	return &MailService{db: db, cfg: cfg, ledger: ledger}
}

type MailConfigInput struct {
	Host            string
	Port            int
	Username        string
	Password        string
	UseTLS          bool
	Enabled         bool
	ExtractPassword string
	// ResetCursor 置 true 时清空处理记录并把 UID 游标归零，用于「重新扫描」
	ResetCursor bool
}

type MailSyncResult struct {
	Fetched         int      `json:"fetched"`
	Imported        int      `json:"imported"`
	Duplicated      int      `json:"duplicated"`
	PendingPassword int      `json:"pending_password"`
	Failed          int      `json:"failed"`
	Details         []string `json:"details,omitempty"`
}

func (s *MailService) SaveConfig(ctx context.Context, familyID, userID string, in MailConfigInput) error {
	if err := s.requireAdmin(ctx, familyID, userID); err != nil {
		return err
	}
	host := strings.TrimSpace(in.Host)
	username := strings.TrimSpace(in.Username)
	if host == "" || username == "" {
		return domain.ErrBadRequest.WithMessage("请填写邮箱服务器和账号")
	}
	port := in.Port
	if port == 0 {
		port = 993
	}

	existing, err := s.db.MailAccountOfFamily(ctx, s.db.SQL(), familyID)
	if err != nil && err != domain.ErrNotFound {
		return err
	}

	passwordEnc := ""
	extractEnc := ""
	lastUID := int64(0)
	if existing != nil {
		passwordEnc = existing.PasswordEnc
		extractEnc = existing.ExtractPasswordEnc
		lastUID = existing.LastUID
	}
	if in.Password != "" {
		if passwordEnc, err = secretbox.Encrypt(s.cfg.DataKey, in.Password); err != nil {
			return err
		}
	}
	if in.ExtractPassword != "" {
		if extractEnc, err = secretbox.Encrypt(s.cfg.DataKey, in.ExtractPassword); err != nil {
			return err
		}
	}

	accountID := id.New()
	if existing != nil {
		accountID = existing.ID
	}
	if in.ResetCursor {
		lastUID = 0
		if err := s.db.DeleteMailMessages(ctx, s.db.SQL(), familyID); err != nil {
			return err
		}
	}
	now := store.NowMS()
	return s.db.UpsertMailAccount(ctx, s.db.SQL(), &store.MailAccount{
		ID: accountID, FamilyID: familyID, Host: host, Port: port, Username: username,
		PasswordEnc: passwordEnc, ExtractPasswordEnc: extractEnc,
		UseTLS: in.UseTLS, LastUID: lastUID, Enabled: in.Enabled,
		CreatedAt: now, UpdatedAt: now,
	})
}

func (s *MailService) Status(ctx context.Context, familyID, userID string) (map[string]any, error) {
	if _, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID); err != nil {
		return nil, err
	}
	account, err := s.db.MailAccountOfFamily(ctx, s.db.SQL(), familyID)
	if err != nil {
		if err == domain.ErrNotFound {
			return map[string]any{"configured": false}, nil
		}
		return nil, err
	}
	out := map[string]any{
		"configured":           true,
		"host":                 account.Host,
		"port":                 account.Port,
		"username":             account.Username,
		"use_tls":              account.UseTLS,
		"enabled":              account.Enabled,
		"has_password":         account.PasswordEnc != "",
		"has_extract_password": account.ExtractPasswordEnc != "",
		"last_uid":             account.LastUID,
	}
	if account.LastSyncAt != nil {
		out["last_sync_at"] = *account.LastSyncAt
	}
	messages, err := s.db.ListMailMessages(ctx, s.db.SQL(), familyID, 10)
	if err == nil {
		items := make([]map[string]any, 0, len(messages))
		for _, m := range messages {
			items = append(items, map[string]any{
				"id": m.ID, "uid": m.MessageUID, "subject": m.Subject,
				"from": m.FromAddr, "status": m.Status, "detail": m.Detail,
				"created_at": m.CreatedAt,
			})
		}
		out["messages"] = items
	}
	return out, nil
}

// TestConnection 用输入或已保存的配置试着登录一次。
func (s *MailService) TestConnection(ctx context.Context, familyID, userID string, in MailConfigInput) error {
	if err := s.requireAdmin(ctx, familyID, userID); err != nil {
		return err
	}
	cfg, err := s.buildConfig(ctx, familyID, in)
	if err != nil {
		return err
	}
	if err := mailer.TestConnection(cfg); err != nil {
		return domain.ErrBadRequest.WithMessage(err.Error())
	}
	return nil
}

func (s *MailService) buildConfig(ctx context.Context, familyID string, in MailConfigInput) (mailer.Config, error) {
	password := in.Password
	if password == "" {
		account, err := s.db.MailAccountOfFamily(ctx, s.db.SQL(), familyID)
		if err != nil {
			return mailer.Config{}, domain.ErrBadRequest.WithMessage("请先填写邮箱密码或授权码")
		}
		password, err = secretbox.Decrypt(s.cfg.DataKey, account.PasswordEnc)
		if err != nil {
			return mailer.Config{}, domain.ErrBadRequest.WithMessage("已保存的邮箱密码无法解密，请重新填写")
		}
	}
	host := strings.TrimSpace(in.Host)
	username := strings.TrimSpace(in.Username)
	port := in.Port
	if host == "" || username == "" {
		account, err := s.db.MailAccountOfFamily(ctx, s.db.SQL(), familyID)
		if err != nil {
			return mailer.Config{}, domain.ErrBadRequest.WithMessage("邮箱配置不完整")
		}
		host, username, port = account.Host, account.Username, account.Port
	}
	return mailer.Config{Host: host, Port: port, Username: username, Password: password, UseTLS: in.UseTLS}, nil
}

// SyncFamily 拉取并导入一个家庭的账单邮件。
func (s *MailService) SyncFamily(ctx context.Context, familyID string) (*MailSyncResult, error) {
	result := &MailSyncResult{}

	account, err := s.db.MailAccountOfFamily(ctx, s.db.SQL(), familyID)
	if err != nil {
		if err == domain.ErrNotFound {
			return result, nil
		}
		return nil, err
	}
	if !account.Enabled {
		return result, nil
	}
	password, err := secretbox.Decrypt(s.cfg.DataKey, account.PasswordEnc)
	if err != nil || password == "" {
		return nil, domain.ErrBadRequest.WithMessage("邮箱密码未设置或无法解密，请重新填写")
	}
	extractPassword, _ := secretbox.Decrypt(s.cfg.DataKey, account.ExtractPasswordEnc)

	cfg := mailer.Config{
		Host: account.Host, Port: account.Port, Username: account.Username,
		Password: password, UseTLS: account.UseTLS,
	}
	fetched, err := mailer.FetchSince(cfg, uint32(account.LastUID), 10)
	if err != nil {
		return nil, domain.ErrBadRequest.WithMessage(err.Error())
	}
	result.Fetched = len(fetched.Messages)

	ownerID, err := s.familyOwner(ctx, familyID)
	if err != nil {
		return nil, err
	}

	now := store.NowMS()
	for _, msg := range fetched.Messages {
		record := &store.MailMessage{
			ID: id.New(), FamilyID: familyID, MessageUID: int64(msg.UID),
			Subject: msg.Subject, FromAddr: msg.From, Status: "pending", CreatedAt: now,
		}
		inserted, err := s.db.InsertMailMessage(ctx, s.db.SQL(), record)
		if err != nil {
			return nil, err
		}
		if !inserted {
			continue // 这封邮件处理过了
		}
		s.importMessage(ctx, familyID, ownerID, record, msg, extractPassword, result)
	}

	newUID := account.LastUID
	if int64(fetched.LastUID) > newUID {
		newUID = int64(fetched.LastUID)
	}
	_ = s.db.UpdateMailProgress(ctx, s.db.SQL(), familyID, newUID, now)
	return result, nil
}

func (s *MailService) importMessage(
	ctx context.Context,
	familyID, ownerID string,
	record *store.MailMessage,
	msg mailer.Message,
	extractPassword string,
	result *MailSyncResult,
) {
	if !looksLikeBillMail(msg) {
		// 账单邮箱里必然混着推广、通知类邮件，直接跳过而不是报失败
		_ = s.db.UpdateMailMessage(ctx, s.db.SQL(), record.ID, "skipped", "不是账单邮件，已跳过")
		return
	}

	// 每一份账单都有独立的解压密码（微信和支付宝都是，且没有固定规则），
	// 所以优先用这封邮件自己存的那份，账号级密码只当兜底
	password := extractPassword
	if record.PasswordEnc != "" {
		if p, err := secretbox.Decrypt(s.cfg.DataKey, record.PasswordEnc); err == nil && p != "" {
			password = p
		}
	}

	attachments := msg.Attachments
	if len(attachments) == 0 {
		// 微信账单没有附件，是正文里的下载链接（7 天有效）
		name, data, err := s.fetchLinkedBill(ctx, msg)
		if err != nil {
			_ = s.db.UpdateMailMessage(ctx, s.db.SQL(), record.ID, "skipped", err.Error())
			return
		}
		attachments = []mailer.Attachment{{FileName: name, Data: data}}
	}

	for _, att := range attachments {
		parsed, err := importer.ParseAny(att.FileName, att.Data, password)
		if err != nil {
			if isPasswordError(err) {
				result.PendingPassword++
				_ = s.db.UpdateMailMessage(ctx, s.db.SQL(), record.ID, "need_password",
					"账单压缩包需要解压密码，请在 App 里填写")
				return
			}
			if strings.Contains(err.Error(), "没有找到账单文件") {
				// 附件是 zip 但不是账单（比如保单、发票包），跳过
				_ = s.db.UpdateMailMessage(ctx, s.db.SQL(), record.ID, "skipped", "压缩包里没有账单文件")
				return
			}
			result.Failed++
			_ = s.db.UpdateMailMessage(ctx, s.db.SQL(), record.ID, "failed", err.Error())
			return
		}

		summary, err := s.ledger.importRecords(ctx, familyID, ownerID, parsed, att.FileName)
		if err != nil {
			result.Failed++
			_ = s.db.UpdateMailMessage(ctx, s.db.SQL(), record.ID, "failed", err.Error())
			return
		}
		result.Imported += summary.Imported
		result.Duplicated += summary.Duplicated
		detail := fmt.Sprintf("导入 %d 条，重复 %d 条，跳过 %d 条",
			summary.Imported, summary.Duplicated, summary.Skipped)
		result.Details = append(result.Details, detail)
		_ = s.db.UpdateMailMessage(ctx, s.db.SQL(), record.ID, "done", detail)
	}
}

// SubmitPassword 提交解压密码。
//   - 指定 messageID：这份账单自己的密码不同，只重试这一封
//   - 不指定：当成默认密码，重试所有等待密码的邮件
func (s *MailService) SubmitPassword(ctx context.Context, familyID, userID, messageID, password string) (*MailSyncResult, error) {
	if err := s.requireAdmin(ctx, familyID, userID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(password) == "" {
		return nil, domain.ErrBadRequest.WithMessage("密码不能为空")
	}
	account, err := s.db.MailAccountOfFamily(ctx, s.db.SQL(), familyID)
	if err != nil {
		return nil, domain.ErrBadRequest.WithMessage("还没有配置账单邮箱")
	}
	mailPassword, err := secretbox.Decrypt(s.cfg.DataKey, account.PasswordEnc)
	if err != nil || mailPassword == "" {
		return nil, domain.ErrBadRequest.WithMessage("邮箱密码无法解密，请重新填写邮箱配置")
	}
	cfg := mailer.Config{Host: account.Host, Port: account.Port, Username: account.Username,
		Password: mailPassword, UseTLS: account.UseTLS}

	ownerID, err := s.familyOwner(ctx, familyID)
	if err != nil {
		return nil, err
	}
	result := &MailSyncResult{}

	enc, err := secretbox.Encrypt(s.cfg.DataKey, password)
	if err != nil {
		return nil, err
	}

	var targets []store.MailMessage
	if messageID != "" {
		m, err := s.db.MailMessageByID(ctx, s.db.SQL(), familyID, messageID)
		if err != nil {
			return nil, err
		}
		if err := s.db.SetMailMessagePassword(ctx, s.db.SQL(), m.ID, enc); err != nil {
			return nil, err
		}
		m.PasswordEnc = enc
		targets = append(targets, *m)
	} else {
		account.ExtractPasswordEnc = enc
		account.UpdatedAt = store.NowMS()
		if err := s.db.UpsertMailAccount(ctx, s.db.SQL(), account); err != nil {
			return nil, err
		}
		pending, err := s.db.MailMessagesByStatus(ctx, s.db.SQL(), familyID, "need_password")
		if err != nil {
			return nil, err
		}
		targets = pending
	}

	for i := range targets {
		m := targets[i]
		msg, err := mailer.FetchByUID(cfg, uint32(m.MessageUID))
		if err != nil {
			result.Failed++
			_ = s.db.UpdateMailMessage(ctx, s.db.SQL(), m.ID, "failed", err.Error())
			continue
		}
		s.importMessage(ctx, familyID, ownerID, &m, *msg, password, result)
	}
	return result, nil
}

// fetchLinkedBill 微信账单没有附件，邮件正文里给的是下载链接（7 天有效）。
func (s *MailService) fetchLinkedBill(ctx context.Context, msg mailer.Message) (string, []byte, error) {
	for _, link := range msg.Links {
		if !mailer.IsAllowedBillURL(link) {
			continue
		}
		name, data, err := download(ctx, link)
		if err != nil || len(data) == 0 {
			continue
		}
		return name, data, nil
	}
	return "", nil, errors.New("邮件里没有找到可下载的账单链接（可能已过期）")
}

func download(ctx context.Context, link string) (string, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; FamilyLedger/1.0)")

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("下载账单失败: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return "", nil, err
	}

	name := fileNameFromHeader(resp.Header.Get("Content-Disposition"))
	if name == "" {
		if base := path.Base(urlPath(link)); base != "." && base != "/" && base != "" {
			name = base
		}
	}
	if name == "" || name == "downloadfilefromemail" {
		name = "微信支付账单.zip"
	}
	return name, data, nil
}

func fileNameFromHeader(disposition string) string {
	if disposition == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(disposition)
	if err != nil {
		return ""
	}
	if name := params["filename"]; name != "" {
		return name
	}
	return params["filename*"]
}

func urlPath(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Path
	}
	return ""
}

// SyncAll 供定时任务调用：同步唯一家庭的账单邮箱。
func (s *MailService) SyncAll(ctx context.Context) {
	familyID, err := s.db.PrimaryFamilyID(ctx, s.db.SQL())
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return // 还没初始化家庭
		}
		slog.Error("读取家庭失败", "err", err)
		return
	}
	if _, err := s.SyncFamily(ctx, familyID); err != nil {
		slog.Warn("账单邮件同步失败", "family", familyID, "err", err)
	}
}

func (s *MailService) requireAdmin(ctx context.Context, familyID, userID string) error {
	member, err := mustMember(ctx, s.db, s.db.SQL(), familyID, userID)
	if err != nil {
		return err
	}
	if member.Role == domain.RoleMember {
		return domain.ErrForbidden.WithMessage("只有户主或管理员可以配置账单邮箱")
	}
	return nil
}

func (s *MailService) familyOwner(ctx context.Context, familyID string) (string, error) {
	family, err := s.db.FamilyByID(ctx, s.db.SQL(), familyID)
	if err != nil {
		return "", err
	}
	return family.OwnerID, nil
}

func isPasswordError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "密码") || strings.Contains(msg, "password")
}

// looksLikeBillMail 粗筛：账单邮箱里必然混着推广、通知邮件，
// 先按发件人和标题判断，避免把「车险保单」这种也去解析一遍。
func looksLikeBillMail(msg mailer.Message) bool {
	hay := strings.ToLower(msg.Subject + " " + msg.From)
	for _, kw := range []string{
		"支付宝", "alipay", "微信支付", "微信", "wechat", "tenpay",
		"账单", "交易流水", "对账单", "明细", "交易记录",
	} {
		if strings.Contains(hay, kw) {
			return true
		}
	}
	for _, att := range msg.Attachments {
		name := strings.ToLower(att.FileName)
		for _, kw := range []string{"账单", "明细", "流水", "bill"} {
			if strings.Contains(name, kw) {
				return true
			}
		}
	}
	return false
}
