// Package upstream 是调新后端（blognext-go）的 HTTP 客户端。
//
// 设计要点：返回**原始字节**而不是解码后的对象——各条旧接口要取的字段
// 都不一样（data / meta.count / error.message…），在客户端里统一解码
// 反而要多一层假设；让 legacy 包按需 decode 才好单测（喂假 JSON 就行）。
package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client 指向一个新后端实例。并发安全（底层 http.Client 可复用）。
type Client struct {
	base           string
	hc             *http.Client
	forwardHeaders []string
}

// New 构造客户端。base 不带结尾斜杠；forwardHeaders 是透传请求头白名单。
func New(base string, timeout time.Duration, forwardHeaders []string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"),
		hc:   &http.Client{Timeout: timeout},
		// 白名单转小写比较，调用方传什么大小写都能匹配
		forwardHeaders: lowerAll(forwardHeaders),
	}
}

func lowerAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, h := range in {
		out = append(out, strings.ToLower(h))
	}
	return out
}

// Do 按统一签名调上游。body 的三种形态：
//   - nil        → 无请求体
//   - []byte     → 原样发送（multipart / 已序列化的 JSON），Content-Type 取 hdr 里的值
//   - 其它任意值 → JSON 序列化后发送，Content-Type: application/json
//
// 返回状态码与原始响应体；网络层失败（连不上、超时）返回 error，
// 由调用方转成旧格式的 502。
func (c *Client) Do(ctx context.Context, method, path string, q url.Values, body any, hdr map[string]string) (int, []byte, error) {
	status, data, _, err := c.doFull(ctx, method, path, q, body, hdr)
	return status, data, err
}

// DoWithHeader 与 Do 相同，额外返回上游响应头。
// 只有两个接口需要它：sitemap 要透传 Content-Type、themeColor 要透传 Cache-Control。
func (c *Client) DoWithHeader(ctx context.Context, method, path string, q url.Values, body any, hdr map[string]string) (int, []byte, http.Header, error) {
	return c.doFull(ctx, method, path, q, body, hdr)
}

func (c *Client) doFull(ctx context.Context, method, path string, q url.Values, body any, hdr map[string]string) (int, []byte, http.Header, error) {
	var reader io.Reader
	contentType := ""
	switch b := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(b)
		// 原始字节（multipart 等）必须带调用方给的内容类型，不能默认 JSON
		contentType = hdr["Content-Type"]
	default:
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("序列化请求体失败: %w", err)
		}
		reader = bytes.NewReader(encoded)
		contentType = "application/json"
	}

	full := c.base + path
	if len(q) > 0 {
		full += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, full, reader)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("构造上游请求失败: %w", err)
	}
	for key, value := range hdr {
		if c.allowed(key) {
			req.Header.Set(key, value)
		}
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("上游请求失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, resp.Header, fmt.Errorf("读上游响应失败: %w", err)
	}
	return resp.StatusCode, data, resp.Header, nil
}

// allowed 判断某个请求头是否在透传白名单里。
// 白名单的意义：浏览器的 Host、Cookie、X-Forwarded-* 不能跟着请求一起到新后端。
func (c *Client) allowed(header string) bool {
	h := strings.ToLower(header)
	// X-Internal-Token 例外：glue 自己生成的服务间鉴权头（screening/latency/export
	// 的 secret 挪头），不来自浏览器，必须放行
	if h == "x-internal-token" {
		return true
	}
	for _, allowed := range c.forwardHeaders {
		if h == allowed {
			return true
		}
	}
	return false
}
