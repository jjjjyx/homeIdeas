// Package mailer 从账单邮箱拉取邮件，并抽出账单附件。
//
// 设计取舍：用 IMAP 拉取一个现成邮箱，而不是自建收信服务器。
// 自建 SMTP 需要公网 IP、25 端口、MX 记录和反垃圾，成本远高于收益。
package mailer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"

	// 注册常见字符集解码器：支付宝/银行的邮件头常是 GBK/GB18030，
	// 不注册的话 go-message 会直接报 unhandled charset 并放弃整封邮件。
	_ "github.com/emersion/go-message/charset"
)

type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	UseTLS   bool
}

func (c Config) addr() string {
	port := c.Port
	if port == 0 {
		port = 993
	}
	return fmt.Sprintf("%s:%d", c.Host, port)
}

func dial(cfg Config) (*client.Client, error) {
	if cfg.Host == "" || cfg.Username == "" {
		return nil, errors.New("邮箱配置不完整（缺少服务器地址或账号）")
	}
	if cfg.UseTLS {
		c, err := client.DialTLS(cfg.addr(), nil)
		if err != nil {
			return nil, fmt.Errorf("连接邮箱服务器失败: %w", err)
		}
		return c, nil
	}
	c, err := client.Dial(cfg.addr())
	if err != nil {
		return nil, fmt.Errorf("连接邮箱服务器失败: %w", err)
	}
	return c, nil
}

// TestConnection 只验证能不能登录，用于「保存前先测一下」。
func TestConnection(cfg Config) error {
	c, err := dial(cfg)
	if err != nil {
		return err
	}
	defer c.Logout()
	if err := c.Login(cfg.Username, cfg.Password); err != nil {
		return fmt.Errorf("登录失败（检查账号或授权码）: %w", err)
	}
	if _, err := c.Select("INBOX", true); err != nil {
		return fmt.Errorf("打开收件箱失败: %w", err)
	}
	return nil
}

type Attachment struct {
	FileName    string
	ContentType string
	Data        []byte
}

// PartInfo 诊断用：邮件里每个 MIME 部件的概要
type PartInfo struct {
	ContentType  string
	Disposition  string
	FileName     string
	Size         int
	IsAttachment bool
}

type Message struct {
	UID         uint32
	From        string
	Subject     string
	ReceivedAt  time.Time
	Attachments []Attachment
	// Links 正文里的链接（微信的账单是下载链接，不是附件）
	Links []string
	Parts []PartInfo
	// ParseError 解析过程中的非致命错误（排查用）
	ParseError string
}

// FetchResult 拉取结果
type FetchResult struct {
	Messages    []Message
	LastUID     uint32
	MailboxSize uint32
}

// FetchSince 拉取 UID 大于 sinceUID 的邮件（最多 max 封），只取带附件的。
func FetchSince(cfg Config, sinceUID uint32, max int) (*FetchResult, error) {
	c, err := dial(cfg)
	if err != nil {
		return nil, err
	}
	defer c.Logout()

	if err := c.Login(cfg.Username, cfg.Password); err != nil {
		return nil, fmt.Errorf("登录失败（检查账号或授权码）: %w", err)
	}
	mbox, err := c.Select("INBOX", false)
	if err != nil {
		return nil, fmt.Errorf("打开收件箱失败: %w", err)
	}

	result := &FetchResult{MailboxSize: mbox.Messages}
	if mbox.Messages == 0 {
		result.LastUID = sinceUID
		return result, nil
	}

	from := sinceUID + 1
	if sinceUID == 0 {
		// 首次同步：只扫最近 max 封，不能从 UID 1 开始，
		// 否则会从最早的历史邮件开始处理，把几年前的推广邮件当账单
		if mbox.UidNext > uint32(max)+1 {
			from = mbox.UidNext - uint32(max)
		} else {
			from = 1
		}
	}
	if from < 1 {
		from = 1
	}

	seqset := new(imap.SeqSet)
	seqset.AddRange(from, 0) // 0 表示 "*"；这里按 UID 语义使用

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, section.FetchItem()}

	ch := make(chan *imap.Message, 10)
	done := make(chan error, 1)
	go func() {
		// 注意：必须用 UidFetch，用 Fetch 会把 UID 当序列号处理，导致漏拉/错拉
		done <- c.UidFetch(seqset, items, ch)
	}()

	var lastUID = sinceUID
	count := 0
	for msg := range ch {
		if msg.Uid > lastUID {
			lastUID = msg.Uid
		}
		if count >= max {
			continue // 继续消费 channel，避免 goroutine 阻塞
		}
		parsed, err := parseMessage(msg, section)
		if err != nil {
			continue
		}
		// 有附件的（支付宝）和「正文里带账单下载链接」的（微信）都算
		if len(parsed.Attachments) > 0 || hasBillLink(parsed.Links) {
			result.Messages = append(result.Messages, *parsed)
			count++
		}
	}
	if err := <-done; err != nil {
		return nil, fmt.Errorf("拉取邮件失败: %w", err)
	}

	result.LastUID = lastUID
	return result, nil
}

// FetchRaw 取回某封邮件的原始 RFC822 内容，排查奇怪邮件用。
func FetchRaw(cfg Config, uid uint32) ([]byte, error) {
	c, err := dial(cfg)
	if err != nil {
		return nil, err
	}
	defer c.Logout()
	if err := c.Login(cfg.Username, cfg.Password); err != nil {
		return nil, fmt.Errorf("登录失败: %w", err)
	}
	if _, err := c.Select("INBOX", true); err != nil {
		return nil, fmt.Errorf("打开收件箱失败: %w", err)
	}
	seqset := new(imap.SeqSet)
	seqset.AddNum(uid)
	section := &imap.BodySectionName{}
	ch := make(chan *imap.Message, 1)
	done := make(chan error, 1)
	go func() { done <- c.UidFetch(seqset, []imap.FetchItem{section.FetchItem()}, ch) }()

	var raw []byte
	for msg := range ch {
		if body := msg.GetBody(section); body != nil {
			raw, _ = io.ReadAll(io.LimitReader(body, 32<<20))
		}
	}
	if err := <-done; err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, errors.New("邮件不存在或已被删除")
	}
	return raw, nil
}

// FetchRecent 取最近 limit 封邮件（不管有没有附件），用于排查「账单邮件长什么样」。
func FetchRecent(cfg Config, limit int) ([]Message, error) {
	c, err := dial(cfg)
	if err != nil {
		return nil, err
	}
	defer c.Logout()
	if err := c.Login(cfg.Username, cfg.Password); err != nil {
		return nil, fmt.Errorf("登录失败: %w", err)
	}
	mbox, err := c.Select("INBOX", true)
	if err != nil {
		return nil, fmt.Errorf("打开收件箱失败: %w", err)
	}
	if mbox.Messages == 0 {
		return nil, nil
	}

	from := mbox.Messages - uint32(limit) + 1
	if from < 1 || from > mbox.Messages {
		from = 1
	}
	seqset := new(imap.SeqSet)
	seqset.AddRange(from, mbox.Messages)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, section.FetchItem()}

	ch := make(chan *imap.Message, 10)
	done := make(chan error, 1)
	go func() { done <- c.Fetch(seqset, items, ch) }()

	out := make([]Message, 0, limit)
	for msg := range ch {
		if parsed, err := parseMessage(msg, section); err == nil {
			out = append(out, *parsed)
		}
	}
	if err := <-done; err != nil {
		return nil, fmt.Errorf("拉取邮件失败: %w", err)
	}
	return out, nil
}

// FetchByUID 重新取回某一封邮件，用于「补填解压码后重试」。
func FetchByUID(cfg Config, uid uint32) (*Message, error) {
	c, err := dial(cfg)
	if err != nil {
		return nil, err
	}
	defer c.Logout()
	if err := c.Login(cfg.Username, cfg.Password); err != nil {
		return nil, fmt.Errorf("登录失败: %w", err)
	}
	if _, err := c.Select("INBOX", true); err != nil {
		return nil, fmt.Errorf("打开收件箱失败: %w", err)
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(uid)
	section := &imap.BodySectionName{}
	items := []imap.FetchItem{imap.FetchUid, imap.FetchEnvelope, section.FetchItem()}

	ch := make(chan *imap.Message, 1)
	done := make(chan error, 1)
	go func() { done <- c.UidFetch(seqset, items, ch) }()

	var out *Message
	for msg := range ch {
		parsed, err := parseMessage(msg, section)
		if err == nil {
			out = parsed
		}
	}
	if err := <-done; err != nil {
		return nil, fmt.Errorf("拉取邮件失败: %w", err)
	}
	if out == nil {
		return nil, errors.New("找不到该邮件（可能已被删除）")
	}
	return out, nil
}

func parseMessage(msg *imap.Message, section *imap.BodySectionName) (*Message, error) {
	body := msg.GetBody(section)
	if body == nil {
		return nil, errors.New("邮件正文为空")
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	out, err := ParseRaw(raw)
	if out != nil {
		out.UID = msg.Uid
	}
	return out, err
}

// ParseRaw 解析一封原始 RFC822 邮件，便于用本地 .eml 排查。
func ParseRaw(raw []byte) (*Message, error) {
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("解析邮件头失败: %w", err)
	}

	out := &Message{ReceivedAt: time.Now()}
	if subject, err := mr.Header.Subject(); err == nil {
		out.Subject = subject
	}
	if fromList, err := mr.Header.AddressList("From"); err == nil && len(fromList) > 0 {
		from := fromList[0]
		name := decodeEncodedWords(from.Name)
		if name != "" {
			out.From = name + " <" + from.Address + ">"
		} else {
			out.From = from.Address
		}
	}
	if t, err := mr.Header.Date(); err == nil {
		out.ReceivedAt = t
	}

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			out.ParseError = err.Error()
			break
		}

		rawCT := part.Header.Get("Content-Type")
		rawDisp := part.Header.Get("Content-Disposition")
		ct, _, _ := mime.ParseMediaType(rawCT)
		disp, dispParams, _ := mime.ParseMediaType(rawDisp)
		name := partFileName(part)

		data, readErr := io.ReadAll(io.LimitReader(part.Body, 32<<20))
		if readErr != nil {
			continue
		}

		out.Parts = append(out.Parts, PartInfo{
			ContentType: ct, Disposition: disp, FileName: name,
			Size: len(data), IsAttachment: disp == "attachment",
		})

		// 判定是不是账单附件：先看扩展名，再看内容魔数/关键词，
		// 因为有些邮件客户端会给附件用 application/octet-stream 且不带文件名
		// 微信账单邮件没有附件，账单是正文里的下载链接
		if ct == "text/html" || ct == "text/plain" {
			out.Links = append(out.Links, extractLinks(string(data))...)
		}

		if !isBillPart(name, ct, disp, dispParams, data) {
			continue
		}
		saveName := name
		if saveName == "" {
			saveName = syntheticName(ct, data)
		}
		out.Attachments = append(out.Attachments, Attachment{
			FileName:    saveName,
			ContentType: rawCT,
			Data:        data,
		})
	}
	return out, nil
}

// isBillPart 判断某个 MIME 部件是不是账单附件。
func isBillPart(name, contentType, disposition string, dispParams map[string]string, data []byte) bool {
	if name != "" && looksLikeBill(name) {
		return true
	}
	// 没文件名但内容是 zip：支付宝/微信的账单包里很常见
	if len(data) > 4 && data[0] == 'P' && data[1] == 'K' && data[2] == 3 && data[3] == 4 {
		return true
	}
	if contentType == "text/csv" {
		return true
	}
	// 兜底：内容里出现账单表头关键词
	head := data
	if len(head) > 2048 {
		head = head[:2048]
	}
	lower := strings.ToLower(string(head))
	for _, kw := range []string{"交易时间", "交易对方", "交易金额", "交易单号", "交易订单号"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	// 附件但没有扩展名时也保留，让上层解析器报更明确的错
	if disposition == "attachment" && dispParams["filename"] == "" && len(data) > 0 {
		return true
	}
	return false
}

func syntheticName(contentType string, data []byte) string {
	if len(data) > 4 && data[0] == 'P' && data[1] == 'K' {
		return "attachment.zip"
	}
	if contentType == "text/csv" {
		return "attachment.csv"
	}
	return "attachment.bin"
}

// partFileName 从附件头里取文件名。
// go-message 的 PartHeader 是接口，具体实现是 *AttachmentHeader / *InlineHeader，
// 所以要按类型断言；兜底再手工解析 Content-Disposition / Content-Type。
func partFileName(part *mail.Part) string {
	if h, ok := part.Header.(*mail.AttachmentHeader); ok {
		if name, err := h.Filename(); err == nil && name != "" {
			return name
		}
	}
	if raw := part.Header.Get("Content-Disposition"); raw != "" {
		if _, params, err := mime.ParseMediaType(raw); err == nil && params["filename"] != "" {
			return params["filename"]
		}
	}
	if raw := part.Header.Get("Content-Type"); raw != "" {
		if _, params, err := mime.ParseMediaType(raw); err == nil && params["name"] != "" {
			return params["name"]
		}
	}
	return ""
}

// looksLikeBill 只关心账单文件，避免把图片之类的附件也下载下来
func looksLikeBill(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range []string{".zip", ".csv", ".txt", ".xlsx", ".xls"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// decodeEncodedWords 兜底解码 =?charset?B?...?= 形式的头部。
// 某些邮件客户端会把整段 From 塞进 encoded-word，AddressList 解不干净。
func decodeEncodedWords(s string) string {
	if !strings.Contains(s, "=?") {
		return s
	}
	dec := &mime.WordDecoder{CharsetReader: message.CharsetReader}
	if decoded, err := dec.DecodeHeader(s); err == nil {
		return decoded
	}
	return s
}

var linkPattern = regexp.MustCompile(`(?i)href\s*=\s*["'']([^"'']+)["'']`)

// extractLinks 从正文里抽出 http(s) 链接并去重。
func extractLinks(body string) []string {
	matches := linkPattern.FindAllStringSubmatch(body, -1)
	seen := map[string]bool{}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		link := strings.TrimSpace(strings.ReplaceAll(m[1], "&amp;", "&"))
		if !strings.HasPrefix(link, "https://") && !strings.HasPrefix(link, "http://") {
			continue
		}
		if seen[link] {
			continue
		}
		seen[link] = true
		out = append(out, link)
	}
	return out
}

// 只允许从这些域名下载账单，防止邮件里的链接把服务端当跳板访问内网（SSRF）
var billURLHosts = []string{
	"wechatpay.cn", "tenpay.com", "wxp.qq.com",
	"alipay.com", "alipayobjects.com", "mybank.cn",
}

// IsAllowedBillURL 判断链接是否是可信任的账单下载地址
func IsAllowedBillURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, allowed := range billURLHosts {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

// hasBillLink 邮件里是否有像账单下载的链接
func hasBillLink(links []string) bool {
	for _, link := range links {
		if !IsAllowedBillURL(link) {
			continue
		}
		lower := strings.ToLower(link)
		if strings.Contains(lower, "bill") || strings.Contains(lower, "download") || strings.Contains(lower, "账单") {
			return true
		}
	}
	return false
}
