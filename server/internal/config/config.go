package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 全部来自环境变量，缺省值面向本地开发。
type Config struct {
	Env                 string
	Addr                string
	DBPath              string
	JWTSecret           string
	AccessTTL           time.Duration
	RefreshTTL          time.Duration
	AllowPublicRegister bool
	CORSOrigins         []string

	// DataKey 用于加密邮箱密码、账单解压码等敏感字段；换掉它会导致已存密文无法解密
	DataKey string
	// MailPollMinutes 账单邮箱轮询间隔（分钟），0 表示关闭定时拉取
	MailPollMinutes int

	// 账单分类用的模型（OpenAI 兼容协议，DeepSeek/豆包/通义都适用）
	LLMEnabled   bool
	LLMProvider  string
	LLMAPIKey    string
	LLMBaseURL   string
	LLMModel     string
	LLMBatchSize int
}

func Load() (*Config, error) {
	c := &Config{
		Env:                 getenv("APP_ENV", "dev"),
		Addr:                getenv("HTTP_ADDR", ":8080"),
		DBPath:              getenv("DB_PATH", "./data/app.db"),
		JWTSecret:           os.Getenv("JWT_SECRET"),
		AccessTTL:           time.Duration(getenvInt("ACCESS_TTL_MINUTES", 15)) * time.Minute,
		RefreshTTL:          time.Duration(getenvInt("REFRESH_TTL_HOURS", 720)) * time.Hour,
		AllowPublicRegister: getenvBool("ALLOW_PUBLIC_REGISTER", false),
		CORSOrigins:         splitList(getenv("CORS_ORIGINS", "*")),
		DataKey:             getenv("DATA_KEY", ""),
		MailPollMinutes:     getenvInt("MAIL_POLL_MINUTES", 15),
		LLMProvider:         getenv("LLM_PROVIDER", "deepseek"),
		LLMAPIKey:           getenv("LLM_API_KEY", ""),
		LLMBaseURL:          getenv("LLM_BASE_URL", ""),
		LLMModel:            getenv("LLM_MODEL", "deepseek-chat"),
		LLMBatchSize:        getenvInt("LLM_BATCH_SIZE", 30),
	}
	if c.JWTSecret == "" {
		if c.Env == "prod" {
			return nil, fmt.Errorf("生产环境必须设置 JWT_SECRET")
		}
		c.JWTSecret = "dev-insecure-secret-change-me"
	}
	if len(c.JWTSecret) < 16 && c.Env == "prod" {
		return nil, fmt.Errorf("JWT_SECRET 至少 16 位")
	}
	if c.DataKey == "" {
		// 没单独配 DATA_KEY 时退化为 JWT_SECRET，换 JWT_SECRET 会让已存的邮箱密码失效
		c.DataKey = c.JWTSecret
	}
	if c.MailPollMinutes < 0 {
		c.MailPollMinutes = 0
	}
	c.LLMEnabled = c.LLMAPIKey != "" && getenv("LLM_ENABLED", "true") != "false"
	if c.LLMBaseURL == "" {
		// 各家默认地址；豆包用的是自家 endpoint，需要显式配置
		switch c.LLMProvider {
		case "doubao":
			c.LLMBaseURL = "https://ark.cn-beijing.volces.com/api/v3"
			if c.LLMModel == "deepseek-chat" {
				c.LLMModel = "doubao-pro-32k"
			}
		case "qwen":
			c.LLMBaseURL = "https://dashscope.aliyuncs.com/compatible-mode"
			if c.LLMModel == "deepseek-chat" {
				c.LLMModel = "qwen-plus"
			}
		default:
			c.LLMBaseURL = "https://api.deepseek.com"
		}
	}
	if c.LLMBatchSize <= 0 || c.LLMBatchSize > 60 {
		c.LLMBatchSize = 30
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func getenvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
