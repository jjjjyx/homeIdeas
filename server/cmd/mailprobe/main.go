package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"family-ledger/server/internal/importer"
	"family-ledger/server/internal/mailer"
)

// 邮件探针：连上 IMAP 看看最近几封邮件、附件长什么样，并把附件落盘验证解析器。
//
//	go run ./cmd/mailprobe -user you@qq.com -pass 授权码 -limit 5
//	go run ./cmd/mailprobe -user you@qq.com -pass 授权码 -limit 3 -parse -zip-pass 123456
func main() {
	user := flag.String("user", "", "邮箱完整地址，例如 123456@qq.com")
	pass := flag.String("pass", "", "IMAP 授权码（不是登录密码）")
	host := flag.String("host", "imap.qq.com", "IMAP 服务器")
	port := flag.Int("port", 993, "IMAP 端口")
	limit := flag.Int("limit", 5, "看最近几封")
	outDir := flag.String("out", "./.mailprobe", "附件保存目录")
	zipPass := flag.String("zip-pass", "", "账单压缩包解压密码")
	rawUID := flag.Uint("raw-uid", 0, "只导出这一封的原始内容（排查用）")
	csvPath := flag.String("csv", "", "直接解析本地账单文件（不连邮箱，排查解析用）")
	flag.Parse()

	if *csvPath != "" {
		data, err := os.ReadFile(*csvPath)
		if err != nil {
			log.Fatalf("读取失败: %v", err)
		}
		result, err := importer.ParseAny(filepath.Base(*csvPath), data, *zipPass)
		if err != nil {
			log.Fatalf("解析失败: %v", err)
		}
		printResult(result)
		return
	}

	if *user == "" || *pass == "" {
		fmt.Fprintln(os.Stderr, "用法: mailprobe -user <邮箱地址> -pass <IMAP授权码>")
		os.Exit(2)
	}

	cfg := mailer.Config{
		Host: *host, Port: *port, Username: *user, Password: *pass, UseTLS: true,
	}

	fmt.Printf("连接 %s:%d 账号 %s ...\n", cfg.Host, cfg.Port, cfg.Username)
	start := time.Now()

	if *rawUID > 0 {
		raw, err := mailer.FetchRaw(cfg, uint32(*rawUID))
		if err != nil {
			log.Fatalf("拉取失败: %v", err)
		}
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			log.Fatalf("创建目录失败: %v", err)
		}
		path := filepath.Join(*outDir, fmt.Sprintf("raw-%d.eml", *rawUID))
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			log.Fatalf("写入失败: %v", err)
		}
		fmt.Printf("原始内容 %d 字节 -> %s\n\n--- 前 1500 字节 ---\n%s\n", len(raw), path, head(raw, 1500))
		return
	}

	messages, err := mailer.FetchRecent(cfg, *limit)
	if err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	fmt.Printf("成功，耗时 %v，取到 %d 封邮件\n\n", time.Since(start).Round(time.Millisecond), len(messages))

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("创建目录失败: %v", err)
	}

	for i, m := range messages {
		fmt.Printf("[%d] UID=%d  %s\n", i+1, m.UID, m.ReceivedAt.Format("2006-01-02 15:04"))
		fmt.Printf("    发件人: %s\n", m.From)
		fmt.Printf("    主题  : %s\n", m.Subject)
		for _, p := range m.Parts {
			fmt.Printf("    部件  : type=%s disp=%s name=%q size=%d att=%v\n",
				p.ContentType, p.Disposition, p.FileName, p.Size, p.IsAttachment)
		}
		if len(m.Attachments) == 0 {
			fmt.Printf("    附件  : 无\n\n")
			continue
		}
		fmt.Printf("    附件  : %d 个\n", len(m.Attachments))
		for _, att := range m.Attachments {
			saved := filepath.Join(*outDir, fmt.Sprintf("%d-%s", m.UID, sanitize(att.FileName)))
			if err := os.WriteFile(saved, att.Data, 0o600); err != nil {
				fmt.Printf("      ! 保存失败: %v\n", err)
				continue
			}
			fmt.Printf("      - %s (%s, %.1f KB) -> %s\n",
				att.FileName, att.ContentType, float64(len(att.Data))/1024, saved)

			if *zipPass != "" || true { // 始终尝试解析，没密码时正好验证报错是否友好
				result, perr := importer.ParseAny(att.FileName, att.Data, *zipPass)
				if perr != nil {
					fmt.Printf("      ! 解析失败: %v\n", perr)
					continue
				}
				fmt.Printf("      ✓ 解析成功：来源=%s 账目=%d 跳过=%d\n",
					result.Source.Label(), len(result.Records), result.Skipped)
				if len(result.Headers) > 0 {
					fmt.Printf("        表头: %v\n", result.Headers)
				}
				for j, rec := range result.Records {
					if j >= 5 {
						fmt.Printf("        ... 还有 %d 条\n", len(result.Records)-5)
						break
					}
					fmt.Printf("        %s  %s  %.2f  %s | %s\n",
						rec.OccurredAt.Format("2006-01-02 15:04"),
						directionLabel(rec.Direction),
						float64(rec.AmountCents)/100,
						truncate(rec.Counterparty, 16),
						truncate(rec.Item, 20))
				}
				for _, w := range result.Warnings {
					fmt.Printf("        ⚠ %s\n", w)
				}
			}
		}
		fmt.Println()
	}
}

func directionLabel(d int) string {
	if d == 1 {
		return "收入"
	}
	return "支出"
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func sanitize(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			out = append(out, '_')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

func head(b []byte, n int) string {
	if len(b) > n {
		b = b[:n]
	}
	return string(b)
}

func printResult(result *importer.Result) {
	fmt.Printf("来源=%s 账目=%d 跳过=%d\n", result.Source.Label(), len(result.Records), result.Skipped)
	if len(result.Headers) > 0 {
		fmt.Printf("表头: %v\n", result.Headers)
	}
	var inCount, outCount int
	var inSum, outSum int64
	for _, r := range result.Records {
		if r.Direction == 1 {
			inCount++
			inSum += r.AmountCents
		} else {
			outCount++
			outSum += r.AmountCents
		}
	}
	fmt.Printf("收入: %d 笔 %.2f 元   支出: %d 笔 %.2f 元\n",
		inCount, float64(inSum)/100, outCount, float64(outSum)/100)
	fmt.Println("--- 跳过原因（最多 20 条）---")
	for _, r := range result.SkipReasons {
		fmt.Printf("  %s\n", r)
	}
	for _, w := range result.Warnings {
		fmt.Printf("  ⚠ %s\n", w)
	}
}
