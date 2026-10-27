package legacy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/LYXOfficial/ariablognext-backend-glue/internal/upstream"
)

// ---------- 标签 / 分类 / 归档 ----------
// 注意：tagInfo / categoryInfo / archiveInfo 的「列表 + 总数」都要转调
// /posts/list（不是 /tags/list），新接口的过滤参数直接支持，meta.count
// 一次拿到，旧契约的 totalCount 就有了（glue.md §4.1 的标注）。

// tags: GET /get/tag/tags → GET /tags/list → {message, data}
func tags(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return passData(ctx, c, r, http.MethodGet, "/tags/list", nil)
}

// tagCount: GET /get/tag/tagCount → GET /tags/list?countOnly=true
// meta.count 是去重后的标签种类数，与旧聚合语义一致。
func tagCount(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return countFromList(ctx, c, r, "/tags/list", nil)
}

// tagInfo: GET /get/tag/tagInfo?tag=&startl=&endl= → GET /posts/list?tag=&page=&pageSize=
// 响应 {message, data, totalCount:meta.count}，一次请求。
func tagInfo(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return listWithTotal(ctx, c, r, map[string]string{"tag": r.URL.Query().Get("tag")})
}

// categories: GET /get/category/categories → GET /categories/list
func categories(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return passData(ctx, c, r, http.MethodGet, "/categories/list", nil)
}

// categoryCount: GET /get/category/categoryCount → GET /categories/list?countOnly=true
func categoryCount(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return countFromList(ctx, c, r, "/categories/list", nil)
}

// categoryInfo: GET /get/category/categoryInfo?category=&startl=&endl= → /posts/list?category=
func categoryInfo(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return listWithTotal(ctx, c, r, map[string]string{"category": r.URL.Query().Get("category")})
}

// archives: GET /get/archive/archives → GET /archives/list
func archives(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return passData(ctx, c, r, http.MethodGet, "/archives/list", nil)
}

// archiveInfo: GET /get/archive/archiveInfo?year=&month=&startl=&endl=
// → GET /posts/list?year=&month=&page=&pageSize=
//
// 特例（glue.md §5.6）：区间内没有文章时旧接口返回 404（前端据此显示
// 「这个月没有文章」），必须照抄，不能顺手改成 200 空数组。
func archiveInfo(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return listWithTotal404OnEmpty(ctx, c, r, map[string]string{
		"year":  r.URL.Query().Get("year"),
		"month": r.URL.Query().Get("month"),
	}, "No posts found for the specified date range")
}

// listWithTotal 是「旧 {message, data, totalCount} ← 新一次 list」的公共实现。
func listWithTotal(ctx context.Context, c *upstream.Client, r *http.Request, extra map[string]string) (int, any) {
	q := pagingWith(r, extra)
	return callUpstream(ctx, c, r, http.MethodGet, "/posts/list", q, nil, detailObject,
		func(env *envelope) (int, any) {
			return 200, okEnvelope(map[string]any{
				"data":       jsonRaw(string(env.Data)),
				"totalCount": env.upCount(),
			})
		})
}

// listWithTotal404OnEmpty 与 listWithTotal 相同，但 data 为空时回 404。
// 仅 archiveInfo 用（旧契约如此）。
func listWithTotal404OnEmpty(ctx context.Context, c *upstream.Client, r *http.Request,
	extra map[string]string, notFoundText string,
) (int, any) {
	q := pagingWith(r, extra)
	return callUpstream(ctx, c, r, http.MethodGet, "/posts/list", q, nil, detailObject,
		func(env *envelope) (int, any) {
			var items []json.RawMessage
			if err := decodeInto(env.Data, &items); err != nil {
				return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务返回了无法解析的数据，请稍后重试"}}
			}
			if len(items) == 0 {
				return legacyError(404, notFoundText)
			}
			return 200, okEnvelope(map[string]any{
				"data":       jsonRaw(string(env.Data)),
				"totalCount": env.upCount(),
			})
		})
}

// ---------- 后台读 / 写（drafts） ----------

// draftsInfo: GET /get/draft/draftsInfo?startl=&endl= → GET /drafts/list
func draftsInfo(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return passData(ctx, c, r, http.MethodGet, "/drafts/list", paging(r))
}

// draftBySlug: GET /get/draft/draftBySlug?slug= → GET /drafts/detail?slug=
func draftBySlug(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	q := url.Values{"slug": {r.URL.Query().Get("slug")}}
	return passData(ctx, c, r, http.MethodGet, "/drafts/detail", q)
}

// draftSlugs: GET /get/draft/draftSlugs → GET /drafts/slugs
func draftSlugs(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return passData(ctx, c, r, http.MethodGet, "/drafts/slugs", nil)
}

// draftCount: GET /get/draft/draftCount → GET /drafts/list?countOnly=true
func draftCount(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return countFromList(ctx, c, r, "/drafts/list", nil)
}

// searchDraftsByTitle: GET /get/draft/searchDraftsByTitle?title=&startl=&endl=
// → GET /drafts/list?keyword=&titleOnly=true&page=&pageSize=
func searchDraftsByTitle(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	q := pagingWith(r, map[string]string{"keyword": r.URL.Query().Get("title")})
	return passData(ctx, c, r, http.MethodGet, "/drafts/list", q)
}

// searchDraftsByTitleCount: → GET /drafts/list?keyword=&countOnly=true
func searchDraftsByTitleCount(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return countFromList(ctx, c, r, "/drafts/list",
		map[string]string{"keyword": r.URL.Query().Get("title")})
}

// addDraft: POST /update/draft/addDraft → POST /drafts/create（409 撞 slug 是字符串 detail）
func addDraft(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return forwardBody(ctx, c, r, http.MethodPost, "/drafts/create")
}

// updateDraftInfo: PUT /update/draft/updateDraftInfo → PUT /drafts/update
// 草稿不存在旧后端也是静默成功，翻译回 success（GO-002）。
func updateDraftInfo(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return forwardBodySilent(ctx, c, r, http.MethodPut, "/drafts/update", codeIn("draft.not_found"))
}

// updateDraftMarkdown: PUT /update/draft/updateDraftMarkdown → PUT /drafts/content
func updateDraftMarkdown(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return forwardBodySilent(ctx, c, r, http.MethodPut, "/drafts/content", codeIn("draft.not_found"))
}

// deleteDraft: DELETE /update/draft/deleteDraft（body {slug}）→ DELETE /drafts/delete?slug=
func deleteDraft(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return slugDelete(ctx, c, r, "/drafts/delete")
}

// updatePostSummaryRead: GET /update/post/summary?slug=（后台只读缓存）
// → GET /posts/summary?slug=&generate=false：旧后台预览是「只读缓存，绝不生成」
// （GO-007），cacheOnly 让 glue 追加 generate=false，无缓存时旧形状 data:null。
func updatePostSummaryRead(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return summaryFrom(ctx, c, r, http.MethodGet, true)
}

// ---------- 说说 ----------

// speaks: GET /get/speaks/speaks?startl=&endl= → GET /speaks/list?page=&pageSize=
func speaks(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return passData(ctx, c, r, http.MethodGet, "/speaks/list", paging(r))
}

// speaksCount: GET /get/speaks/speaksCount → GET /speaks/list?countOnly=true
func speaksCount(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return countFromList(ctx, c, r, "/speaks/list", nil)
}

// newSpeaks: POST /update/speaks/newSpeaks → POST /speaks/create（body {time,content} 一致）
func newSpeaks(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return forwardBody(ctx, c, r, http.MethodPost, "/speaks/create")
}

// updateSpeaks: PUT /update/speaks/updateSpeaks → PUT /speaks/update
// 旧后端对不存在的说说也是静默成功；新后端把它归进参数校验错误
// （validation.invalid_field，GO-002/GO-004）。
// 判据优先锚定结构：details.fields 里有 field=="time"（契约 §0.2 固定形态，
// go 侧有 TestSpeakUpdateNotFoundShapeIsStable 钉住）；message 含「说说不存在」
// 只作兜底（或关系，避免单点失效）。其它校验错误照常透传。
func updateSpeaks(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return forwardBodySilent(ctx, c, r, http.MethodPut, "/speaks/update",
		func(err *errorInfo) bool {
			if err == nil || err.Code != "validation.invalid_field" {
				return false
			}
			var d struct {
				Fields []struct {
					Field string `json:"field"`
				} `json:"fields"`
			}
			if json.Unmarshal(err.Details, &d) == nil {
				for _, f := range d.Fields {
					if f.Field == "time" {
						return true
					}
				}
			}
			return strings.Contains(err.Message, "说说不存在")
		})
}

// deleteSpeaks: DELETE /update/speaks/deleteSpeaks（body {time}）→ DELETE /speaks/delete?time=
// 旧接口 time 在 body 里，新接口走 query。
func deleteSpeaks(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	body, err := readBody(r)
	if err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体读取失败，请重试"}}
	}
	var b struct {
		Time int64 `json:"time"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求缺少 time 字段"}}
	}
	q := url.Values{"time": {formatInt(b.Time)}}
	return callUpstream(ctx, c, r, http.MethodDelete, "/speaks/delete", q, nil, detailObject,
		func(env *envelope) (int, any) { return 200, okEnvelope(nil) })
}

func formatInt(n int64) string {
	return strconv.FormatInt(n, 10)
}
