package legacy

import (
	"encoding/json"
	"strings"
	"testing"
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
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"updated":true}`, "")
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
