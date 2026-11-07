package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"family-ledger/server/internal/classifier"
	"family-ledger/server/internal/config"
	"family-ledger/server/internal/llm"
	"family-ledger/server/internal/store"
)

// 用大模型把账目分成「家庭/个人」并给出细分类。
//
//	export LLM_API_KEY=sk-xxx
//	go run ./cmd/classify -db ./data/app.db -family 我们的家
//	go run ./cmd/classify -db ./data/app.db -family-id 01a1... -limit 100 -dry-run
func main() {
	dbPath := flag.String("db", "./data/app.db", "SQLite 数据库路径")
	familyName := flag.String("family", "", "家庭名称（与 -family-id 二选一）")
	familyID := flag.String("family-id", "", "家庭 ID")
	limit := flag.Int("limit", 1000, "最多处理多少条待分类账目")
	dryRun := flag.Bool("dry-run", false, "只打印将要分类的数量，不调用模型")
	reset := flag.Bool("reset", false, "先清空已有的 AI 分类结果，重新分类（人工确认过的不动）")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("配置错误: %v", err)
	}

	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if err := db.Migrate(ctx); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}

	target := *familyID
	if target == "" {
		if *familyName == "" {
			log.Fatal("请指定 -family 或 -family-id")
		}
		target, err = lookupFamily(ctx, db, *familyName)
		if err != nil {
			log.Fatalf("%v", err)
		}
	}

	if *reset {
		if _, err := db.SQL().ExecContext(ctx, `
			UPDATE transactions SET scope = 'unknown', ai_confidence = 0, category_source = ', ai_reason = '
			WHERE family_id = ? AND reviewed = 0`, target); err != nil {
			log.Fatalf("重置分类失败: %v", err)
		}
		fmt.Println("已重置未人工确认的分类结果")
	}

	pending, err := db.UnclassifiedTransactions(ctx, db.SQL(), target, *limit)
	if err != nil {
		log.Fatalf("读取待分类账目失败: %v", err)
	}
	fmt.Printf("家庭 %s：待分类 %d 条\n", target, len(pending))
	if *dryRun {
		for i, p := range pending {
			if i >= 10 {
				fmt.Printf("  ... 还有 %d 条\n", len(pending)-10)
				break
			}
			fmt.Printf("  %s  ¥%.2f  %s\n",
				time.UnixMilli(p.OccurredAt).Format("01-02 15:04"), float64(p.AmountCents)/100, p.Note)
		}
		return
	}
	if len(pending) == 0 {
		return
	}

	client := llm.New(llm.Config{
		Provider: cfg.LLMProvider,
		APIKey:   cfg.LLMAPIKey,
		BaseURL:  cfg.LLMBaseURL,
		Model:    cfg.LLMModel,
	})
	if !client.Enabled() {
		log.Fatal("未配置 LLM_API_KEY，无法调用模型")
	}
	fmt.Printf("使用模型: %s @ %s\n", cfg.LLMModel, cfg.LLMBaseURL)

	svc := classifier.New(db, client, cfg.LLMBatchSize)
	start := time.Now()
	result, err := svc.RunFamily(ctx, target, *limit)
	if err != nil {
		log.Fatalf("分类失败: %v", err)
	}

	fmt.Printf("\n完成，耗时 %v\n", time.Since(start).Round(time.Second))
	fmt.Printf("  总数 %d，已分类 %d，无法判断 %d，请求 %d 次\n",
		result.Total, result.Classified, result.Unknown, result.Batches)
	fmt.Printf("  token: 输入 %d / 输出 %d\n", result.PromptTokens, result.CompletionTokens)
	for _, e := range result.Errors {
		fmt.Printf("  ⚠ %s\n", e)
	}
	os.Exit(0)
}

func lookupFamily(ctx context.Context, db *store.DB, name string) (string, error) {
	rows, err := db.SQL().QueryContext(ctx, `SELECT id, name FROM families WHERE deleted_at IS NULL`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id, n string
		if err := rows.Scan(&id, &n); err != nil {
			return "", err
		}
		if n == name {
			return id, nil
		}
	}
	return "", fmt.Errorf("没找到名为 %q 的家庭", name)
}
