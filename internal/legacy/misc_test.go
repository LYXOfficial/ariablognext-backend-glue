package legacy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------- 图片：三层嵌套是刻意的 ----------

func TestUploadImage_NestedLinks(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"url":"https://img.0v0.my/2026/10/27/abc.png"}`, "")
	})
	multipart := "--X\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.png\"\r\nContent-Type: image/png\r\n\r\nPNG\r\n--X--\r\n"
	r := legacyReq("POST", "/update/image/uploadImage", "", []byte(multipart),
		map[string]string{"Authorization": "Bearer tok", "Content-Type": "multipart/form-data; boundary=X"})
	status, body := run(t, uploadImage, f.client(), r)
	if status != 200 {
		t.Fatalf("status=%d", status)
	}
	// 必须是 data.data.links.url（Vditor 兼容），不许「顺手清理」
	outer, _ := body["data"].(map[string]any)
	mid, _ := outer["data"].(map[string]any)
	links, _ := mid["links"].(map[string]any)
	if links == nil || links["url"] != "https://img.0v0.my/2026/10/27/abc.png" {
		t.Errorf("三层嵌套形状错误: %#v", body["data"])
	}
	// multipart 要原样透传且 Content-Type 带 boundary
	if !strings.Contains(string(f.last.Body), "PNG") {
		t.Errorf("multipart body 应原样转发: %s", f.last.Body)
	}
	if !strings.Contains(f.last.Headers.Get("Content-Type"), "boundary=X") {
		t.Errorf("Content-Type（含 boundary）必须透传: %q", f.last.Headers.Get("Content-Type"))
	}
}

func TestDeleteImage_DeletedIsURL(t *testing.T) {
	// 旧接口 deleted 的值是**请求里的 url**（不是 true），要补回去
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"deleted":true}`, "")
	})
	r := legacyReq("DELETE", "/update/image/deleteImage", "url=https://img.0v0.my/2026/10/27/abc.png", nil,
		map[string]string{"Authorization": "Bearer tok"})
	_, body := run(t, deleteImage, f.client(), r)
	inner, _ := body["data"].(map[string]any)
	if inner["deleted"] != "https://img.0v0.my/2026/10/27/abc.png" {
		t.Errorf("deleted 应为请求的 url: %#v", inner)
	}
	if f.last.Query.Get("url") != "https://img.0v0.my/2026/10/27/abc.png" {
		t.Errorf("url 没透传: %v", f.last.Query)
	}
}

func TestListImages_ShapeKept(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"images":["https://img.0v0.my/a.png"]}`, "")
	})
	_, body := run(t, listImages, f.client(), legacyReq("GET", "/update/image/listImages", "", nil,
		map[string]string{"Authorization": "Bearer tok"}))
	// data 是原样透传的 RawMessage，反序列化后验证形状
	var inner map[string]any
	if err := json.Unmarshal(body["data"].(json.RawMessage), &inner); err != nil || inner["images"] == nil {
		t.Errorf("data.images 形状要保持: %#v", body["data"])
	}
}

func TestRehostImageByUrl(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"url":"https://img.0v0.my/x.png"}`, "")
	})
	r := legacyReq("POST", "/update/image/rehostImageByUrl", "", []byte(`{"url":"https://out/x.png"}`), nil)
	_, body := run(t, rehostImageByUrl, f.client(), r)
	var inner map[string]any
	if err := json.Unmarshal(body["data"].(json.RawMessage), &inner); err != nil || inner["url"] != "https://img.0v0.my/x.png" {
		t.Errorf("data.url = %#v", body["data"])
	}
	if !strings.Contains(string(f.last.Body), `"url":"https://out/x.png"`) {
		t.Errorf("body 应原样转发: %s", f.last.Body)
	}
}

// ---------- sitemap：XML 原样直通 ----------

func TestSitemapGen_RawXMLPassThrough(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return 200, "<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\"></urlset>",
			map[string]string{"Content-Type": "application/xml"}
	})
	r := legacyReq("GET", "/get/sitemap/sitemapGen", "", nil, nil)
	ctx, respHdr := withRespHeader(r.Context())
	status, raw := sitemapGen(ctx, f.client(), r)
	if status != 200 {
		t.Fatalf("status=%d", status)
	}
	// 返回的是原始字节（不 JSON 编码）
	if b, ok := raw.([]byte); !ok || !strings.HasPrefix(string(b), "<urlset") {
		t.Errorf("XML 应原样直通: %#v", raw)
	}
	if respHdr.Get("Content-Type") != "application/xml" {
		t.Errorf("Content-Type 要透传: %v", respHdr)
	}
}

func TestSitemapGen_UpstreamError_NoEnvelopeLeak(t *testing.T) {
	// 上游挂掉时回的是新信封 JSON；透传会把 {"ok":false,...} 泄漏给爬虫，
	// 必须换成旧格式的 detail 对象
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return 500, `{"ok":false,"requestId":"t","error":{"code":"db.failed","message":"boom","details":null}}`,
			map[string]string{"Content-Type": "application/json"}
	})
	r := legacyReq("GET", "/get/sitemap/sitemapGen", "", nil, nil)
	ctx, _ := withRespHeader(r.Context())
	status, body := sitemapGen(ctx, f.client(), r)
	if status != 502 {
		t.Fatalf("status=%d", status)
	}
	m, ok := body.(map[string]any)
	if !ok {
		t.Fatalf("应为 JSON 对象而非原始字节: %#v", body)
	}
	detail, _ := m["detail"].(map[string]any)
	if detail["message"] != "fail" || detail["error"] == "" {
		t.Errorf("detail 形态不对: %v", m)
	}
}

// ---------- export：secret 挪头 + collections 提顶 ----------

func TestExportCollections(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		if c.Path != "/maintenance/export" {
			t.Errorf("path = %s", c.Path)
		}
		return okResp(`{"collections":{"Posts":[{"_id":"x"}],"Speaks":[]}}`, "")
	})
	r := legacyReq("POST", "/get/export/collections", "", []byte(`{"secret":"SEC"}`), nil)
	status, body := run(t, exportCollections, f.client(), r)
	if status != 200 || body["message"] != "success" {
		t.Fatalf("status=%d body keys=%v", status, bodyKeys(body))
	}
	if f.last.Headers.Get("X-Internal-Token") != "SEC" {
		t.Errorf("secret 应挪到 X-Internal-Token: %v", f.last.Headers)
	}
	// collections 提到顶层，内部结构不动（值是原样透传的 RawMessage）
	collsRaw, _ := json.Marshal(body["collections"])
	var colls map[string]json.RawMessage
	if err := json.Unmarshal(collsRaw, &colls); err != nil || colls["Posts"] == nil {
		t.Errorf("collections 应提到顶层: %v", body)
	}
	if strings.TrimSpace(string(colls["Posts"])) != `[{"_id":"x"}]` {
		t.Errorf("集合内容应原样: %s", colls["Posts"])
	}
}

func TestExportCollections_BadSecret(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return errResp(403, "auth.invalid_token", "internal token 不正确", "null")
	})
	r := legacyReq("POST", "/get/export/collections", "", []byte(`{"secret":"bad"}`), nil)
	status, body := run(t, exportCollections, f.client(), r)
	if status != 403 {
		t.Errorf("status=%d", status)
	}
	if _, ok := body["detail"]; !ok {
		t.Errorf("403 要带 detail: %v", body)
	}
}

// ---------- site / misc ----------

func TestLatestUpdateTimeWrite(t *testing.T) {
	// refreshCache 是 fire-and-forget 的真实公网调用，测试里必须换掉，
	// 并用 channel 等 goroutine 真正跑完再断言
	fired := make(chan struct{})
	orig := notifyRefreshCache
	notifyRefreshCache = func() { close(fired) }
	defer func() { notifyRefreshCache = orig }()

	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"updated":true,"latestUpdateTime":1761400000}`, "")
	})
	r := legacyReq("POST", "/update/siteInfo/latestUpdateTime", "", nil,
		map[string]string{"Authorization": "Bearer tok"})
	status, body := run(t, latestUpdateTimeWrite, f.client(), r)
	if status != 200 || body["message"] != "success" {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if f.last.Method != "POST" || f.last.Path != "/site/update-time" {
		t.Errorf("上游收到 %s %s", f.last.Method, f.last.Path)
	}
	// 旧后端忽略请求体、永远写当前时间：转发时 body 必须是 `{}`——既丢弃了
	// 旧调用方的 {"time":N} 覆写，又满足新后端「至少一个 JSON 对象」的要求
	// （真机实测：完全无 body 新后端回 400）
	if string(f.last.Body) != "{}" {
		t.Errorf("上游应收到 {}，实际 %q", f.last.Body)
	}
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Error("转发成功后没有触发 refreshCache 通知")
	}
}

func TestLatestUpdateTimeWrite_UpstreamFail_NoRefresh(t *testing.T) {
	// 上游失败时不能触发 refreshCache（旧后端写库失败也不调它）
	fired := make(chan struct{})
	orig := notifyRefreshCache
	notifyRefreshCache = func() { close(fired) }
	defer func() { notifyRefreshCache = orig }()

	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return errResp(500, "site.update_failed", "写时间戳失败", "null")
	})
	r := legacyReq("POST", "/update/siteInfo/latestUpdateTime", "", nil, nil)
	status, body := run(t, latestUpdateTimeWrite, f.client(), r)
	if status != 500 {
		t.Fatalf("status=%d", status)
	}
	if _, ok := body["detail"]; !ok {
		t.Errorf("失败要带 detail: %v", body)
	}
	select {
	case <-fired:
		t.Error("上游失败不应触发 refreshCache")
	default:
	}
}

func TestNotifyRefreshCache_SwallowsErrors(t *testing.T) {
	// 通知失败（连不上 / 非 2xx）必须安静吞掉，不能 panic、不能有返回值
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(500)
	}))
	defer srv.Close()

	origURL, origClient := refreshCacheURL, refreshCacheClient
	t.Cleanup(func() { refreshCacheURL, refreshCacheClient = origURL, origClient })
	refreshCacheURL = srv.URL
	refreshCacheClient = srv.Client()

	notifyRefreshCache() // 500 也算「尽力而为」，不报错
	if hits != 1 {
		t.Errorf("通知请求应发出 1 次，实际 %d", hits)
	}

	refreshCacheURL = "http://127.0.0.1:1/unreachable"
	notifyRefreshCache() // 连不上也不许 panic
}

func TestFlinksAndFlinkCount(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		if c.Path == "/links/list" && c.Query.Get("countOnly") == "true" {
			return okResp(`null`, `{"count":27}`)
		}
		return okResp(`[{"name":"友达","links":[]}]`, `{"count":27}`)
	})
	_, body := run(t, flinks, f.client(), legacyReq("GET", "/get/flink/flinks", "", nil, nil))
	if _, ok := body["data"]; !ok {
		t.Error("flinks 缺 data")
	}
	_, body = run(t, flinkCount, f.client(), legacyReq("GET", "/get/flink/flinkCount", "", nil, nil))
	if body["count"] != int64(27) {
		t.Errorf("flinkCount = %v", body["count"])
	}
}

func TestSpeaks_Paging(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`[{"time":1,"content":"hi"}]`, `{"count":5}`)
	})
	_, _ = run(t, speaks, f.client(), legacyReq("GET", "/get/speaks/speaks", "startl=0&endl=3", nil, nil))
	if f.last.Query.Get("pageSize") != "3" {
		t.Errorf("speaks 分页换算错误: %v", f.last.Query)
	}
	_, _ = run(t, speaksCount, f.client(), legacyReq("GET", "/get/speaks/speaksCount", "", nil, nil))
	if f.last.Query.Get("countOnly") != "true" {
		t.Errorf("speaksCount 应 countOnly: %v", f.last.Query)
	}
}

// ---------- 路由表完整性 ----------

func TestRouteTableHas72Routes(t *testing.T) {
	// 72 = 27 公开读 + 11 后台读 + 31 写 + 3 已删；差一条前端就白屏
	got := len(All())
	if got != 72 {
		t.Errorf("路由表应有 72 条，实际 %d", got)
	}
	seen := map[string]bool{}
	for _, r := range All() {
		key := r.Method + " " + r.Path
		if seen[key] {
			t.Errorf("路由重复: %s", key)
		}
		seen[key] = true
		if r.Handle == nil {
			t.Errorf("路由缺 handler: %s", key)
		}
	}
	// 抽查几条关键路径，防止手滑改名
	for _, key := range []string{
		"GET /get/post/postsInfo", "GET /get/flink/themeColor", "GET /get/sitemap/sitemapGen",
		"GET /flink/apply/status", "POST /access/user/login", "GET /access/user/verify",
		"POST /get/export/collections", "DELETE /update/draft/deleteRenderedHtmlCache",
		"GET /update/flink/dispatchCheckLatencyWorkflow", "POST /update/flink/pushFlinkStatus",
	} {
		if !seen[key] {
			t.Errorf("缺关键路由: %s", key)
		}
	}
}

func bodyKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
