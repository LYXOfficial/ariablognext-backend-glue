package upstream

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestDo_JSONBodyAndQueryAndHeaderWhitelist(t *testing.T) {
	var gotMethod, gotPath, gotRawQuery, gotAuth, gotCookie, gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotRawQuery = r.Method, r.URL.Path, r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		gotCookie = r.Header.Get("Cookie") // 不在白名单，必须不带过去
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":[1,2],"meta":{"count":2}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, 5*time.Second, []string{"Authorization", "Content-Type"})
	q := url.Values{"page": {"3"}, "pageSize": {"16"}}
	status, data, err := c.Do(context.Background(), http.MethodGet, "/posts/list", q,
		map[string]any{"slug": "x"},
		map[string]string{"Authorization": "Bearer tok", "Cookie": "steal=1", "Content-Type": "application/json"})
	if err != nil {
		t.Fatal(err)
	}
	if status != 200 {
		t.Errorf("status = %d", status)
	}
	if gotMethod != "GET" || gotPath != "/posts/list" || gotRawQuery != "page=3&pageSize=16" {
		t.Errorf("上游收到 %s %s?%s", gotMethod, gotPath, gotRawQuery)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization 应原样透传，实际 %q", gotAuth)
	}
	if gotCookie != "" {
		t.Errorf("白名单外的头不该透传，Cookie=%q", gotCookie)
	}
	// 结构化 body 会被 JSON 编码（即使 GET，也是调用方自己的选择）
	if gotCT != "application/json" || gotBody != `{"slug":"x"}` {
		t.Errorf("body = %s (CT=%q)", gotBody, gotCT)
	}
	if string(data) != `{"ok":true,"data":[1,2],"meta":{"count":2}}` {
		t.Errorf("响应原始字节应原样返回: %s", data)
	}
}

func TestDo_RawBytesKeepsContentType(t *testing.T) {
	// multipart 转发场景：body 是原始字节，Content-Type 必须带上 boundary
	var gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New(srv.URL, 5*time.Second, []string{"Authorization", "Content-Type"})
	_, _, err := c.Do(context.Background(), http.MethodPost, "/images/upload", nil,
		[]byte("--BOUNDARY\r\n"), map[string]string{"Authorization": "Bearer t", "Content-Type": "multipart/form-data; boundary=BOUNDARY"})
	if err != nil {
		t.Fatal(err)
	}
	if gotCT != "multipart/form-data; boundary=BOUNDARY" || gotBody != "--BOUNDARY\r\n" {
		t.Errorf("multipart 透传失败: CT=%q body=%q", gotCT, gotBody)
	}
}

func TestDoWithHeader_ReturnsUpstreamHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true,"data":{"color":"#fff"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, 5*time.Second, nil)
	_, _, hdr, err := c.DoWithHeader(context.Background(), http.MethodGet, "/links/theme-color", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Get("Cache-Control") != "public, max-age=86400" {
		t.Errorf("Cache-Control 没拿到: %v", hdr)
	}
}

func TestDo_NetworkError(t *testing.T) {
	// 指向一个必然关闭的端口，验证连不上时返回 error 而不是 panic
	c := New("http://127.0.0.1:1", time.Second, nil)
	if _, _, err := c.Do(context.Background(), "GET", "/x", nil, nil, nil); err == nil {
		t.Fatal("连不上上游应返回 error")
	}
}
