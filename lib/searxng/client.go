// Package searxng 封装对自部署 SearXNG 实例的 JSON 搜索。
// 结果面向 Agent：标题 / 详情 / 链接，可再 FormatForAgent 成可读文本。
package searxng

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Result 单条搜索结果（Agent 侧只关心这三项）。
type Result struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	URL     string `json:"url"`
	Engine  string `json:"engine,omitempty"`
}

// Client 访问一个 SearXNG 实例。
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	// Limit 最多返回条数，0 表示不截断（用实例默认）。
	Limit int
	// Language 可选，如 "zh-CN"；空则不传。
	Language string
}

// New 创建客户端。baseURL 形如 http://127.0.0.1:54911（不要带尾斜杠路径）。
func New(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		Limit: 10,
	}
}

// Search 用关键词搜索，返回按实例排序的结果列表。
func (c *Client) Search(ctx context.Context, query string) ([]Result, error) {
	query = strings.TrimSpace(query)
	if c == nil || c.BaseURL == "" {
		return nil, fmt.Errorf("searxng: empty base URL")
	}
	if query == "" {
		return nil, fmt.Errorf("searxng: empty query")
	}

	u, err := url.Parse(c.BaseURL + "/search")
	if err != nil {
		return nil, fmt.Errorf("searxng: invalid base URL: %w", err)
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "json")
	if c.Language != "" {
		q.Set("language", c.Language)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("searxng: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Plrx-SearXNG/1.0")

	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("searxng: read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("searxng: status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var raw rawResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("searxng: decode json: %w", err)
	}

	out := make([]Result, 0, len(raw.Results))
	for _, r := range raw.Results {
		title := strings.TrimSpace(r.Title)
		link := strings.TrimSpace(r.URL)
		if title == "" && link == "" {
			continue
		}
		out = append(out, Result{
			Title:   title,
			Content: strings.TrimSpace(r.Content),
			URL:     link,
			Engine:  strings.TrimSpace(r.Engine),
		})
		if c.Limit > 0 && len(out) >= c.Limit {
			break
		}
	}
	return out, nil
}

// Search 便捷函数：baseURL + 关键词，默认最多 10 条。
func Search(baseURL, query string) ([]Result, error) {
	return New(baseURL).Search(context.Background(), query)
}

// FormatForAgent 把结果排成给 Agent 读的纯文本（标题 / 详情 / 链接）。
func FormatForAgent(results []Result) string {
	if len(results) == 0 {
		return "（无搜索结果）"
	}
	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "[%d] 标题: %s\n", i+1, emptyAs(r.Title, "（无标题）"))
		fmt.Fprintf(&b, "    详情: %s\n", emptyAs(r.Content, "（无摘要）"))
		fmt.Fprintf(&b, "    链接: %s\n", emptyAs(r.URL, "（无链接）"))
		if i < len(results)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// SearchForAgent 搜索并直接返回 Agent 可读文本。
func SearchForAgent(baseURL, query string) (string, error) {
	results, err := Search(baseURL, query)
	if err != nil {
		return "", err
	}
	return FormatForAgent(results), nil
}

type rawResponse struct {
	Results []rawResult `json:"results"`
}

type rawResult struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	URL     string `json:"url"`
	Engine  string `json:"engine"`
}

func emptyAs(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
