// Package llm 调用大模型做账单分类。
//
// 只依赖 OpenAI 兼容协议：DeepSeek、豆包、通义千问都兼容这一套，
// 换供应商只改环境变量，不用动代码。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	Provider string // deepseek | doubao | qwen，仅用于日志
	APIKey   string
	BaseURL  string
	Model    string
	Timeout  time.Duration
}

func (c Config) Enabled() bool { return strings.TrimSpace(c.APIKey) != "" }

type Client struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 90 * time.Second
	}
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}
}

func (c *Client) Enabled() bool { return c.cfg.Enabled() }

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Temperature    float64       `json:"temperature"`
	ResponseFormat *respFormat   `json:"response_format,omitempty"`
	MaxTokens      int           `json:"max_tokens,omitempty"`
}

type respFormat struct {
	Type string `json:"type"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// ChatJSON 发一轮对话，要求模型返回 JSON，并把结果解析进 out。
func (c *Client) ChatJSON(ctx context.Context, system, user string, out any) (*Usage, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("未配置 LLM_API_KEY")
	}

	endpoint := strings.TrimRight(c.cfg.BaseURL, "/")
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		if !strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/v1"
		}
		endpoint += "/chat/completions"
	}

	reqBody := chatRequest{
		Model:       c.cfg.Model,
		Temperature: 0, // 分类要稳定，不要发挥
		MaxTokens:   4096,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		ResponseFormat: &respFormat{Type: "json_object"},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用模型失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("模型返回 %d: %s", resp.StatusCode, truncate(string(body), 300))
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("模型响应不是合法 JSON: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("模型报错: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("模型没有返回内容")
	}

	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	content = stripCodeFence(content)
	if err := json.Unmarshal([]byte(content), out); err != nil {
		return nil, fmt.Errorf("解析模型输出失败: %w（原始内容: %s）", err, truncate(content, 300))
	}
	return &parsed.Usage, nil
}

// 有些模型会用 ```json 包裹输出，去掉它
func stripCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
