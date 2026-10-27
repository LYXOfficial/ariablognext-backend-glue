package legacy

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/LYXOfficial/ariablognext-backend-glue/internal/upstream"
)

// ---------- 杂项：sitemap / export ----------

// sitemapGen: GET /get/sitemap/sitemapGen → GET /sitemap
// XML **原样直通**（glue.md §3.5）：不改 Content-Type、不包信封——
// 它是给爬虫的文档不是 API。
func sitemapGen(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	status, raw, hdr, err := c.DoWithHeader(ctx, http.MethodGet, "/sitemap", nil, nil, forwardHeaders(r))
	if err != nil {
		return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "站点地图服务暂时不可用，请稍后重试"}}
	}
	setRespHeader(ctx, "Content-Type", hdr.Get("Content-Type"))
	// 原始字节（不是 map）：mux 层遇到 []byte 直接写出、不再 JSON 编码
	if status >= 400 {
		// 旧后端的 sitemap 是自己生成的，失败只会是数据库抖动，返回的是
		// detail 对象——绝不能把上游的新信封 JSON 原样透给爬虫
		return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "站点地图暂时不可用，请稍后重试"}}
	}
	return 200, raw
}

// exportCollections: POST /get/export/collections（body {secret}）
// → POST /maintenance/export（X-Internal-Token 头）。
// 调用方是 ariablognext-autobackup。响应把新 data.collections 提到顶层，
// 内部结构与旧接口完全一致。
func exportCollections(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	body, err := readBody(r)
	if err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体读取失败，请重试"}}
	}
	var old struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(body, &old); err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体格式不正确"}}
	}
	hdr := forwardHeaders(r)
	hdr["X-Internal-Token"] = old.Secret
	// 这条要扫全库，耗时远超普通接口（旧后端也因此单独放宽）；
	// 上游有 120s 超时预算，这里的超时由 upstream client 的总时限兜底
	return callUpstreamWithHeader(ctx, c, r, http.MethodPost, "/maintenance/export", nil, nil, hdr, detailObject,
		func(env *envelope) (int, any) {
			var data struct {
				Collections json.RawMessage `json:"collections"`
			}
			if err := decodeInto(env.Data, &data); err != nil {
				return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务返回了无法解析的数据，请稍后重试"}}
			}
			return 200, okEnvelope(map[string]any{"collections": jsonRaw(string(data.Collections))})
		})
}
