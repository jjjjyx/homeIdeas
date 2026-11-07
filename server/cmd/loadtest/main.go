// 压测工具：自带一个临时服务实例（临时库 + 随机端口），跑完自动清理。
//
//	go run ./cmd/loadtest
//	go run ./cmd/loadtest -writers 16 -batches 60 -batch 5 -readers 8
//
// 关注点：
//  1. 写入是否出现 SQLITE_BUSY / 5xx（单写者 + busy_timeout 是否够用）
//  2. 吞吐与 p95/p99 延迟
//  3. 压测后数据条数是否与预期一致（没有丢写）
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"family-ledger/server/internal/pkg/id"
)

var (
	flagWriters = flag.Int("writers", 8, "并发写协程数")
	flagBatches = flag.Int("batches", 40, "每个写协程的批次数量")
	flagBatch   = flag.Int("batch", 5, "每批推送多少条账目")
	flagReaders = flag.Int("readers", 4, "并发读协程数")
	flagServer  = flag.String("server", "", "已有服务地址；留空则自动起一个临时实例")
	flagKeep    = flag.Bool("keep", false, "结束后保留临时数据目录")
)

type metrics struct {
	mu   sync.Mutex
	lat  []time.Duration
	errs []string
}

func (m *metrics) record(d time.Duration) {
	m.mu.Lock()
	m.lat = append(m.lat, d)
	m.mu.Unlock()
}

func (m *metrics) fail(msg string) {
	m.mu.Lock()
	if len(m.errs) < 20 {
		m.errs = append(m.errs, msg)
	}
	m.mu.Unlock()
}

func (m *metrics) report(name string) {
	m.mu.Lock()
	lat := append([]time.Duration(nil), m.lat...)
	errs := append([]string(nil), m.errs...)
	m.mu.Unlock()

	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	pct := func(p float64) time.Duration {
		if len(lat) == 0 {
			return 0
		}
		idx := int(float64(len(lat)-1) * p)
		return lat[idx]
	}
	var total time.Duration
	for _, d := range lat {
		total += d
	}
	avg := time.Duration(0)
	if len(lat) > 0 {
		avg = total / time.Duration(len(lat))
	}
	fmt.Printf("  %-6s 请求 %5d  失败 %3d  avg %6.1fms  p50 %6.1fms  p95 %6.1fms  p99 %6.1fms\n",
		name, len(lat), len(errs),
		float64(avg.Microseconds())/1000,
		float64(pct(0.50).Microseconds())/1000,
		float64(pct(0.95).Microseconds())/1000,
		float64(pct(0.99).Microseconds())/1000,
	)
	for _, e := range errs {
		fmt.Printf("         ! %s\n", e)
	}
}

type client struct {
	base  string
	token string
	http  *http.Client
}

func (c *client) do(ctx context.Context, method, path string, body any) (map[string]any, int, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out, resp.StatusCode, nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func main() {
	flag.Parse()
	ctx := context.Background()

	base := *flagServer
	var stop func()
	if base == "" {
		var err error
		base, stop, err = startTempServer()
		if err != nil {
			fmt.Fprintf(os.Stderr, "启动临时服务失败: %v\n", err)
			os.Exit(1)
		}
		defer stop()
		fmt.Printf("临时服务: %s\n", base)
	}

	c := &client{base: base, http: &http.Client{Timeout: 30 * time.Second}}

	// --- 引导：注册 → 建家庭 → 拿分类 ---
	account := "load" + strings.ToLower(id.New()[0:6])
	if _, status, err := c.do(ctx, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"account": account, "password": "Load-Test-Passw0rd!", "nickname": "压测",
	}); err != nil || status != 201 {
		fmt.Fprintf(os.Stderr, "注册失败 status=%d err=%v\n", status, err)
		os.Exit(1)
	}

	login, _, err := c.do(ctx, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"account": account, "password": "Load-Test-Passw0rd!",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "登录失败: %v\n", err)
		os.Exit(1)
	}
	c.token = login["data"].(map[string]any)["access_token"].(string)

	var family map[string]any
	if resp, status, _ := c.do(ctx, http.MethodPost, "/api/v1/families", map[string]any{"name": "压测家庭"}); status == 201 {
		family = resp["data"].(map[string]any)
	} else {
		fmt.Fprintf(os.Stderr, "建家庭失败 status=%d\n", status)
		os.Exit(1)
	}
	familyID := family["id"].(string)

	pull, _, err := c.do(ctx, http.MethodGet, "/api/v1/families/"+familyID+"/sync?since=0", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "首次同步失败: %v\n", err)
		os.Exit(1)
	}
	changes := pull["data"].(map[string]any)["changes"].(map[string]any)
	categories := changes["categories"].([]any)
	if len(categories) == 0 {
		fmt.Fprintln(os.Stderr, "没有分类可用")
		os.Exit(1)
	}
	categoryID := categories[0].(map[string]any)["id"].(string)
	cursor := int64(pull["data"].(map[string]any)["cursor"].(float64))

	fmt.Printf("家庭 %s，分类 %d 个，起始游标 %d\n", familyID[:8], len(categories), cursor)
	fmt.Printf("并发：写 %d × %d 批 × %d 条 = %d 条；读 %d 个协程\n\n",
		*flagWriters, *flagBatches, *flagBatch, *flagWriters**flagBatches**flagBatch, *flagReaders)

	writeM := &metrics{}
	readM := &metrics{}

	start := time.Now()

	var wg sync.WaitGroup
	// 写入
	for w := 0; w < *flagWriters; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for b := 0; b < *flagBatches; b++ {
				ops := make([]map[string]any, 0, *flagBatch)
				for i := 0; i < *flagBatch; i++ {
					ops = append(ops, map[string]any{
						"client_op_id": id.New(),
						"entity":       "transaction",
						"op":           "upsert",
						"base_rev":     0,
						"payload": map[string]any{
							"id":           id.New(),
							"type":         0,
							"amount_cents": 100 + worker*10 + i,
							"category_id":  categoryID,
							"occurred_at":  time.Now().UnixMilli(),
							"note":         fmt.Sprintf("w%d-b%d", worker, b),
							"visibility":   "family",
						},
					})
				}
				begin := time.Now()
				resp, status, err := c.do(ctx, http.MethodPost,
					"/api/v1/families/"+familyID+"/sync/push", map[string]any{"ops": ops})
				if err != nil {
					writeM.fail(err.Error())
					continue
				}
				if status != 200 {
					writeM.fail(fmt.Sprintf("push status=%d body=%v", status, resp))
					continue
				}
				writeM.record(time.Since(begin))
			}
		}(w)
	}

	// 读取
	for r := 0; r < *flagReaders; r++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			local := cursor
			for i := 0; i < *flagBatches**flagWriters; i++ {
				begin := time.Now()
				resp, status, err := c.do(ctx, http.MethodGet,
					fmt.Sprintf("/api/v1/families/%s/sync?since=%d&limit=100", familyID, local), nil)
				if err != nil {
					readM.fail(err.Error())
					continue
				}
				if status != 200 {
					readM.fail(fmt.Sprintf("sync status=%d", status))
					continue
				}
				if data, ok := resp["data"].(map[string]any); ok {
					if cur, ok := data["cursor"].(float64); ok {
						local = int64(cur)
					}
				}
				readM.record(time.Since(begin))
			}
		}(r)
	}

	wg.Wait()
	elapsed := time.Since(start)

	fmt.Println("结果：")
	writeM.report("写")
	readM.report("读")

	totalWrites := *flagWriters * *flagBatches * *flagBatch
	fmt.Printf("\n总耗时 %.2fs\n", elapsed.Seconds())
	fmt.Printf("写入吞吐 %.0f 条/秒（%d 条）\n", float64(totalWrites)/elapsed.Seconds(), totalWrites)

	// 校验：最终条数应等于写入数（示例分类等不计）
	summary, _, _ := c.do(ctx, http.MethodGet, "/api/v1/families/"+familyID+"/stats/summary", nil)
	if data, ok := summary["data"].(map[string]any); ok {
		if cnt, ok := data["count"].(float64); ok {
			fmt.Printf("服务端账单数 %d / 期望 %d %s\n", int(cnt), totalWrites,
				map[bool]string{true: "✓", false: "✗ 有丢写"}[int(cnt) == totalWrites])
		}
	}
}

// startTempServer 起一个临时实例，返回地址与清理函数。
func startTempServer() (string, func(), error) {
	root, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}
	bin := filepath.Join(root, "bin", "ledger-server.exe")
	if _, err := os.Stat(bin); err != nil {
		return "", nil, fmt.Errorf("找不到 %s，请先 go build -o bin/ledger-server.exe ./cmd/api", bin)
	}
	dir, err := os.MkdirTemp(root, ".loadtest-")
	if err != nil {
		return "", nil, err
	}
	port, err := freePort()
	if err != nil {
		return "", nil, err
	}

	cmd := exec.Command(bin)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"APP_ENV=dev",
		fmt.Sprintf("HTTP_ADDR=127.0.0.1:%d", port),
		"DB_PATH="+filepath.Join(dir, "app.db"),
		"JWT_SECRET=loadtest-secret-0123456789",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return "", nil, err
	}

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 2 * time.Second}
	ready := false
	for i := 0; i < 60; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := client.Get(base + "/readyz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ready = true
				break
			}
		}
	}
	if !ready {
		_ = cmd.Process.Kill()
		os.RemoveAll(dir)
		return "", nil, fmt.Errorf("服务启动超时")
	}

	stop := func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		if !*flagKeep {
			os.RemoveAll(dir)
		} else {
			fmt.Printf("保留数据目录: %s\n", dir)
		}
	}
	return base, stop, nil
}
