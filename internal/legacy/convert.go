package legacy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/LYXOfficial/ariablognext-backend-glue/internal/upstream"
)

// Handler 是每条旧路径的转换函数。约定见包注释。
type Handler func(ctx context.Context, c *upstream.Client, r *http.Request) (int, any)

// Route 是一条旧路径。Auth 只是文档性标注（glue 不自己做鉴权判断，
// Authorization 原样透传，让新后端返 401），用来说明这条路径属于哪个鉴权域。
type Route struct {
	Method string
	Path   string
	Auth   bool
	Handle Handler
}

// ---------- 上游调用的公共封装 ----------

// callUpstream 调一次上游并按旧契约重排：
// 成功走 rewrite；失败把新信封转成 detail（kind 控制对象/字符串两种形态）。
func callUpstream(ctx context.Context, c *upstream.Client, r *http.Request,
	method, upstreamPath string, q url.Values, body any, kind detailKind,
	rewrite func(env *envelope) (int, any),
) (int, any) {
	return callUpstreamSilent(ctx, c, r, method, upstreamPath, q, body, kind, nil, rewrite)
}

// silentMatch 判断一个上游错误是否属于「旧契约会静默成功」的类型。
type silentMatch func(err *errorInfo) bool

// codeIn 按上游 error.code 匹配（not_found 类错误码）。
func codeIn(codes ...string) silentMatch {
	return func(err *errorInfo) bool {
		if err == nil {
			return false
		}
		for _, c := range codes {
			if err.Code == c {
				return true
			}
		}
		return false
	}
}

// callUpstreamSilent 与 callUpstream 相同，但上游错误命中 match 时改判为旧契约的
// 静默成功（200 {"message":"success"}）。
//
// 背景（sync GO-002）：旧后端的 update 类写接口 update_one 之后不看 matched_count，
// 目标不存在照样 {"message":"success"}，glue.md §4.3 的响应列同样只有 success。
// 新后端按自家契约回 404/400 not_found——不翻译回去的话，dashboard 会在
// 「文章刚被另一个标签页删掉」这类场景看到旧版没有的报错。
// 只有命中 match 的错误被吞，其它 4xx/5xx 照常透传，500 绝不吞。
func callUpstreamSilent(ctx context.Context, c *upstream.Client, r *http.Request,
	method, upstreamPath string, q url.Values, body any, kind detailKind,
	match silentMatch,
	rewrite func(env *envelope) (int, any),
) (int, any) {
	status, raw, upHdr, err := c.DoWithHeader(ctx, method, upstreamPath, q, body, forwardHeaders(r))
	if err != nil {
		// 连不上新后端：转成 502，文案说清楚是胶水这层的事
		return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务暂时不可用，请稍后重试"}}
	}
	// 304 协商缓存透传：上游（ETag 现算）说「没变」，把 ETag 递回客户端、body 为空。
	// 这是实时语义（上游每次都现算指纹），不是 TTL 缓存——数据一变 ETag 立刻变。
	if status == http.StatusNotModified {
		if etag := upHdr.Get("ETag"); etag != "" {
			setRespHeader(ctx, "ETag", etag)
		}
		return http.StatusNotModified, nil
	}
	if status >= 400 {
		env, decErr := decodeEnvelope(raw)
		if decErr == nil && match != nil && match(env.Error) {
			return 200, okEnvelope(nil)
		}
		if decErr != nil || env.Error == nil {
			// 非 JSON 或缺错误信封：把原文塞进 cause 位，别静默吞掉
			return failBody(status, pathOf(r), &errorInfo{
				Code:    "upstream.failed",
				Message: "服务内部错误，请稍后重试",
				Details: jsonRaw(string(raw)),
			})
		}
		// failBody 内部按「路径 + 状态码」决定 detail 是对象还是字符串
		return failBody(status, pathOf(r), env.Error)
	}
	env, err := decodeEnvelope(raw)
	if err != nil {
		return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务返回了无法解析的数据，请稍后重试"}}
	}
	// GET 200：把上游的 ETag/Cache-Control 递给客户端，协商缓存才能成立。
	// 裁剪/换名类的映射是上游数据的确定性函数——数据一变上游 ETag 必变，
	// 所以转发上游 ETag 不会引入任何过期风险（实时语义）。
	if method == http.MethodGet {
		if etag := upHdr.Get("ETag"); etag != "" {
			setRespHeader(ctx, "ETag", etag)
		}
		if cc := upHdr.Get("Cache-Control"); cc != "" {
			setRespHeader(ctx, "Cache-Control", cc)
		}
	}
	return rewrite(env)
}

func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }

// forwardHeaders 挑出要透传给上游的请求头（Authorization 等）。
func forwardHeaders(r *http.Request) map[string]string {
	hdr := map[string]string{}
	if v := r.Header.Get("Authorization"); v != "" {
		hdr["Authorization"] = v
	}
	// If-None-Match：协商缓存的验证头（上游 ETag 现算，304 永远反映最新数据）
	if v := r.Header.Get("If-None-Match"); v != "" {
		hdr["If-None-Match"] = v
	}
	// Content-Type 不在这里带：upstream.Do 会按 body 形态决定
	// （JSON / multipart 的值完全不同），带过去反而会覆盖错。
	return hdr
}

func pathOf(r *http.Request) string { return r.URL.Path }

// ---------- 旧查询参数的读取 ----------

// queryInt 读一个 int 查询参数；缺省返回 def。
func queryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// paging 把旧的 startl/endl（offset/limit 混用）换算成新接口的 page/pageSize。
//
// 旧前端只有两种写法：?startl=0&endl=6（最新 6 篇）、?startl=32&endl=48（第 3 页）。
// 换算规则（glue.md §3.3 / TASKS G1）：limit = endl-startl；page = startl/limit + 1；
// endl 缺省时按 pageSize=200 处理。limit 兜底 16（旧前端未带 endl 的防御值）。
func paging(r *http.Request) url.Values {
	q := url.Values{}
	startl := queryInt(r, "startl", 0)
	endl, hasEndl := -1, false
	if raw := r.URL.Query().Get("endl"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			endl, hasEndl = n, true
		}
	}

	limit := 0
	if hasEndl && endl > startl {
		limit = endl - startl
	}
	if limit <= 0 {
		// endl 缺省 / 不合法：旧接口的 endl 缺省 = 返回到结尾，这里用新后端的
		// maxPageSize=200 一页兜底。若文章数将来超过 200，这里会静默截断——
		// 到时候要么循环翻页拼接，要么调大两侧上限，别只改一边。
		q.Set("page", strconv.Itoa(startl/200+1))
		q.Set("pageSize", "200")
		return q
	}
	q.Set("page", strconv.Itoa(startl/limit+1))
	q.Set("pageSize", strconv.Itoa(limit))
	return q
}

// pagingWith 在 paging 的基础上追加固定参数（tag / year / keyword …）。
func pagingWith(r *http.Request, extra map[string]string) url.Values {
	q := paging(r)
	for k, v := range extra {
		q.Set(k, v)
	}
	return q
}

// countOnlyQ 构造 countOnly=true 的查询串（只要数量，data 为 null）。
func countOnlyQ(extra map[string]string) url.Values {
	q := url.Values{"countOnly": {"true"}}
	for k, v := range extra {
		q.Set(k, v)
	}
	return q
}

// decodeInto 把上游 data 解到目标结构。data 为 null 时不算错误
// （新契约允许 data 键存在但为 null）。
func decodeInto(raw json.RawMessage, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return json.Unmarshal(raw, target)
}

// readBody 读取请求体（写接口转调时要用原样字节或解码后重组）。
func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

// ---------- 响应头回传 ----------
//
// Handler 的签名只返回 (int, any)，但 themeColor 必须透传 Cache-Control。
// 用 context 携带一个响应头集合：server 包裹层创建，转换函数往里写，
// 包裹层在写响应体前统一套上。

type respHeaderKey struct{}

func withRespHeader(ctx context.Context) (context.Context, http.Header) {
	hdr := http.Header{}
	return context.WithValue(ctx, respHeaderKey{}, hdr), hdr
}

func setRespHeader(ctx context.Context, key, value string) {
	if hdr, ok := ctx.Value(respHeaderKey{}).(http.Header); ok {
		hdr.Set(key, value)
	}
}
