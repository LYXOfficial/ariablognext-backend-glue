package legacy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/LYXOfficial/ariablognext-backend-glue/internal/upstream"
)

// ---------- 假上游 ----------
//
// 测试的核心是「给定一份新格式响应，输出的旧格式必须逐字节对得上」
// （docs/testing.md），所以用 httptest 假上游：能录下收到的请求
// （断言参数换算），并按路径回放预置的新格式 JSON。

// capture 记录假上游最近一次收到的请求。
type capture struct {
	Method  string
	Path    string
	Query   url.Values
	Headers http.Header
	Body    []byte
}

type fakeUpstream struct {
	srv  *httptest.Server
	last *capture
	// respond 按请求返回状态码、响应体与响应头
	respond func(c *capture) (int, string, map[string]string)
}

func newFakeUpstream(t *testing.T, respond func(c *capture) (int, string, map[string]string)) *fakeUpstream {
	t.Helper()
	f := &fakeUpstream{respond: respond}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.last = &capture{
			Method:  r.Method,
			Path:    r.URL.Path,
			Query:   r.URL.Query(),
			Headers: r.Header.Clone(),
			Body:    body,
		}
		status, respBody, hdr := f.respond(f.last)
		for k, v := range hdr {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// client 构造指向假上游的客户端（白名单与生产配置一致）。
func (f *fakeUpstream) client() *upstream.Client {
	return upstream.New(f.srv.URL, 5_000_000_000, []string{"Authorization", "Content-Type"})
}

// okResp 是新后端成功信封的快捷构造。
func okResp(dataJSON, metaJSON string) (int, string, map[string]string) {
	body := `{"ok":true,"requestId":"t","data":` + dataJSON
	if metaJSON == "" {
		body += `}`
	} else {
		body += `,"meta":` + metaJSON + `}`
	}
	return 200, body, map[string]string{"Content-Type": "application/json"}
}

func errResp(status int, code, message, details string) (int, string, map[string]string) {
	body := `{"ok":false,"requestId":"t","error":{"code":"` + code + `","message":"` + message + `","details":` + details + `}}`
	return status, body, map[string]string{"Content-Type": "application/json"}
}

// legacyReq 构造一条旧请求（转换函数直接吃 *http.Request）。
func legacyReq(method, path, query string, body []byte, hdr map[string]string) *http.Request {
	r := httptest.NewRequest(method, path+"?"+query, bytes.NewReader(body))
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

// run 跑一条路由并断言返回可以 JSON 化。
func run(t *testing.T, h Handler, client *upstream.Client, r *http.Request) (int, map[string]any) {
	t.Helper()
	status, body := h(context.Background(), client, r)
	if m, ok := body.(map[string]any); ok {
		return status, m
	}
	// 非对象返回（如字符串 detail）统一包一层便于断言
	return status, map[string]any{"__raw__": body}
}
