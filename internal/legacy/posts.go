package legacy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/LYXOfficial/ariablognext-backend-glue/internal/upstream"
)

// ---------- 文章域（公开读） ----------
// 对应 glue.md §4.1 的 postsInfo / postBySlug / postCount / totalWordCount /
// postSlugs / postNavigation / relatedPosts / postSummary / search* 七类。

// postsInfo: GET /get/post/postsInfo → GET /posts/list
func postsInfo(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	var q url.Values
	if r.URL.Query().Get("type") == "full" {
		// 旧 type=full 是 RSS 全量：新接口一次拿完，不要循环翻页（glue.md §5.5）
		q = url.Values{"pageSize": {"full"}, "withContent": {"true"}}
	} else {
		q = paging(r)
	}
	return callUpstream(ctx, c, r, http.MethodGet, "/posts/list", q, nil, detailObject,
		func(env *envelope) (int, any) {
			return 200, okEnvelope(map[string]any{"data": jsonRaw(string(env.Data))})
		})
}

// postBySlug: GET /get/post/postBySlug?slug= → GET /posts/detail?slug=
// 上游 404 的中文 message 会经 failBody 进 detail.error，前端直接展示它。
func postBySlug(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	q := url.Values{"slug": {r.URL.Query().Get("slug")}}
	return callUpstream(ctx, c, r, http.MethodGet, "/posts/detail", q, nil, detailObject,
		func(env *envelope) (int, any) {
			return 200, okEnvelope(map[string]any{"data": jsonRaw(string(env.Data))})
		})
}

// postCount: GET /get/post/postCount → GET /posts/list?countOnly=true
func postCount(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return countFromList(ctx, c, r, "/posts/list", nil)
}

// countFromList 是「旧 {message,count} ← 新 meta.count」的公共实现。
// 一次 countOnly 请求就够，不要再打一遍列表。
func countFromList(ctx context.Context, c *upstream.Client, r *http.Request,
	listPath string, extra map[string]string,
) (int, any) {
	return callUpstream(ctx, c, r, http.MethodGet, listPath, countOnlyQ(extra), nil, detailObject,
		func(env *envelope) (int, any) {
			return 200, okEnvelope(map[string]any{"count": env.upCount()})
		})
}

// totalWordCount: GET /get/post/totalWordCount → GET /posts/statistics
// 新接口 data={"totalWordCount":N}，旧接口顶层叫 count。
func totalWordCount(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return callUpstream(ctx, c, r, http.MethodGet, "/posts/statistics", nil, nil, detailObject,
		func(env *envelope) (int, any) {
			var data struct {
				TotalWordCount int64 `json:"totalWordCount"`
			}
			if err := decodeInto(env.Data, &data); err != nil {
				return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务返回了无法解析的数据，请稍后重试"}}
			}
			return 200, okEnvelope(map[string]any{"count": data.TotalWordCount})
		})
}

// postSlugs: GET /get/post/postSlugs → GET /posts/slugs
func postSlugs(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return passData(ctx, c, r, http.MethodGet, "/posts/slugs", nil)
}

// passData 是「旧 {message,data} ← 新 data」的公共实现（不碰字段）。
func passData(ctx context.Context, c *upstream.Client, r *http.Request,
	method, upstreamPath string, q url.Values,
) (int, any) {
	return callUpstream(ctx, c, r, method, upstreamPath, q, nil, detailObject,
		func(env *envelope) (int, any) {
			return 200, okEnvelope(map[string]any{"data": jsonRaw(string(env.Data))})
		})
}

// postNavigation: GET /get/post/postNavigation?slug= → GET /posts/navigation?slug=
//
// 方向是反的（glue.md §5.3）：旧接口的 previous 是「更新」的那篇、next 是
// 「更旧」的那篇；新接口 newer / older 语义正确。映射：
// previous ← data.newer、next ← data.older。
func postNavigation(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	q := url.Values{"slug": {r.URL.Query().Get("slug")}}
	return callUpstream(ctx, c, r, http.MethodGet, "/posts/navigation", q, nil, detailObject,
		func(env *envelope) (int, any) {
			var data struct {
				Newer json.RawMessage `json:"newer"`
				Older json.RawMessage `json:"older"`
			}
			if err := decodeInto(env.Data, &data); err != nil {
				return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务返回了无法解析的数据，请稍后重试"}}
			}
			return 200, okEnvelope(map[string]any{
				"previous": nullJSON(data.Newer),
				"next":     nullJSON(data.Older),
			})
		})
}

// nullJSON 把 "null"/空字节统一成 nil，map 里输出 JSON null。
func nullJSON(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return jsonRaw(string(raw))
}

// relatedPosts: GET /get/post/relatedPosts?slug= → GET /posts/related?slug=&limit=6
// 无 tag 时新后端返回 []（旧接口是 404），按映射表输出 []。
func relatedPosts(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	q := url.Values{"slug": {r.URL.Query().Get("slug")}, "limit": {"6"}}
	return passData(ctx, c, r, http.MethodGet, "/posts/related", q)
}

// postSummary: GET /get/post/postSummary?slug= → GET /posts/summary?slug=
// 新接口 data={"summary":"…"}，旧接口 data 就是字符串本身。
// 公开语义允许「缓存 miss 生成一次」，与旧公开接口一致（不加 generate）。
func postSummary(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return summaryFrom(ctx, c, r, http.MethodGet, false)
}

// summaryFrom 处理两种 postSummary 语义（glue.md §5.4）：
// 公开 GET 转调 GET /posts/summary（缓存 miss 生成一次）；后台只读 GET 加
// generate=false——旧后台预览是「只读缓存，绝不生成」（GO-007），不分开的话
// dashboard 打开编辑弹窗会静默等最长 90s 并烧一次 SiliconFlow 额度。
// 上游无缓存时 data.summary=null，重排成旧形状 data:null。
func summaryFrom(ctx context.Context, c *upstream.Client, r *http.Request, method string, cacheOnly bool) (int, any) {
	q := url.Values{"slug": {r.URL.Query().Get("slug")}}
	if cacheOnly {
		q.Set("generate", "false")
	}
	return callUpstream(ctx, c, r, method, "/posts/summary", q, nil, detailObject,
		func(env *envelope) (int, any) {
			var data struct {
				Summary *string `json:"summary"`
			}
			if err := decodeInto(env.Data, &data); err != nil {
				return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务返回了无法解析的数据，请稍后重试"}}
			}
			if data.Summary == nil {
				return 200, okEnvelope(map[string]any{"data": nil})
			}
			return 200, okEnvelope(map[string]any{"data": *data.Summary})
		})
}

// searchPosts: GET /get/post/searchPosts?query= → GET /posts/list?keyword=&pageSize=100
func searchPosts(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	q := url.Values{"keyword": {r.URL.Query().Get("query")}, "pageSize": {"100"}}
	return passData(ctx, c, r, http.MethodGet, "/posts/list", q)
}

// searchPostsByTitle: GET /get/post/searchPostsByTitle?title=&startl=&endl=
// → GET /posts/list?keyword=&titleOnly=true&page=&pageSize=
// 旧接口带分页（dashboard 用 startl/endl 翻页搜索结果），照抄。
func searchPostsByTitle(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	q := pagingWith(r, map[string]string{"keyword": r.URL.Query().Get("title"), "titleOnly": "true"})
	return passData(ctx, c, r, http.MethodGet, "/posts/list", q)
}

// searchPostsByTitleCount: GET /get/post/searchPostsByTitleCount?title=
// → GET /posts/list?keyword=&titleOnly=true&countOnly=true
func searchPostsByTitleCount(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return countFromList(ctx, c, r, "/posts/list",
		map[string]string{"keyword": r.URL.Query().Get("title"), "titleOnly": "true"})
}

// ---------- 后台写（posts / drafts 共用的形态） ----------

// writePostBody 是 addPost / updatePostInfo 的请求体：字段名与新接口一致，
// 原样转发即可；这里声明出来只是为了「解码-再编码」时保留未知字段的宽容度。
type slugBody struct {
	Slug string `json:"slug"`
}

// addPost: POST /update/post/addPost → POST /posts/create
// slug 撞车时新后端回 post.slug_conflict(409)，旧契约是字符串 detail。
func addPost(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return forwardBody(ctx, c, r, http.MethodPost, "/posts/create")
}

// updatePostInfo: PUT /update/post/updatePostInfo → PUT /posts/update
// 文章不存在旧后端也是静默成功（update_one 不看 matched_count），翻译回 success。
func updatePostInfo(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return forwardBodySilent(ctx, c, r, http.MethodPut, "/posts/update", codeIn("post.not_found"))
}

// updatePostMarkdown: PUT /update/post/updatePostMarkdown → PUT /posts/content
func updatePostMarkdown(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return forwardBodySilent(ctx, c, r, http.MethodPut, "/posts/content", codeIn("post.not_found"))
}

// deletePost: DELETE /update/post/deletePost（body {slug}）→ DELETE /posts/delete?slug=
// 旧接口 slug 在 JSON body 里，新接口走 query——胶水做 body→query 搬运。
func deletePost(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return slugDelete(ctx, c, r, "/posts/delete")
}

// slugDelete 把 body 里的 slug 搬到 query 再删除。
func slugDelete(ctx context.Context, c *upstream.Client, r *http.Request, upstreamPath string) (int, any) {
	body, err := readBody(r)
	if err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体读取失败，请重试"}}
	}
	var b slugBody
	if err := json.Unmarshal(body, &b); err != nil || b.Slug == "" {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求缺少 slug 字段"}}
	}
	q := url.Values{"slug": {b.Slug}}
	return callUpstream(ctx, c, r, http.MethodDelete, upstreamPath, q, nil, detailObject,
		func(env *envelope) (int, any) { return 200, okEnvelope(nil) })
}

// refreshSummary: POST /update/post/refreshSummary（后台，强制重生成）
// → POST /posts/summary，body {slug} 两边一致。
// 旧响应 {"message","data":摘要字符串}；新 data={"summary":"…"}。
func refreshSummary(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return callUpstream(ctx, c, r, http.MethodPost, "/posts/summary", nil, mustJSONBody(r), detailObject,
		func(env *envelope) (int, any) {
			var data struct {
				Summary *string `json:"summary"`
			}
			if err := decodeInto(env.Data, &data); err != nil || data.Summary == nil {
				return 200, okEnvelope(map[string]any{"data": nil})
			}
			return 200, okEnvelope(map[string]any{"data": *data.Summary})
		})
}

// mustJSONBody 把请求体原样作为 JSON body 传给 upstream（body 为 any 时客户端会编码）。
func mustJSONBody(r *http.Request) any {
	body, err := readBody(r)
	if err != nil || len(body) == 0 {
		return nil
	}
	return json.RawMessage(body)
}

// deleteRenderedHtmlCache 系：功能已删、无人调用（glue.md §4.4），
// 不转发，直接回旧格式 success。
func deleteRenderedHtmlCache(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return 200, okEnvelope(nil)
}
