package legacy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/LYXOfficial/ariablognext-backend-glue/internal/upstream"
)

// ---------- 友链（读 + 后台写）与友链申请 ----------
// 对应 glue.md §4.1/§4.3 的 flink 域。申请的校验/限流/判重全部在新后端，
// 这里只做转发与信封重排。

// flinks: GET /get/flink/flinks → GET /links/list
func flinks(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return passData(ctx, c, r, http.MethodGet, "/links/list", nil)
}

// flinkCount: GET /get/flink/flinkCount → GET /links/list?countOnly=true
// meta.count 是友链条目总数（不是分组数），与旧聚合语义一致。
func flinkCount(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return countFromList(ctx, c, r, "/links/list", nil)
}

// themeColor: GET /get/flink/themeColor?url= → GET /links/theme-color?url=
//
// 特例（glue.md §5.2）：
//   - 新接口 data={"color":"#xxx"}，旧接口 data 就是颜色字符串本身
//   - 必须透传 Cache-Control（Vercel CDN 靠它把同一头像 URL 挡在函数外）
//   - 错误必须是**对象** detail（前端 themeColor.ts 只认 detail.error），CORS 由 glue 的中间件统一补
func themeColor(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	q := url.Values{"url": {r.URL.Query().Get("url")}}
	status, raw, hdr, err := c.DoWithHeader(ctx, http.MethodGet, "/links/theme-color", q, nil, forwardHeaders(r))
	if err != nil {
		return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "头像图片下载失败（取色服务暂时不可用，可稍后重试）"}}
	}
	// Cache-Control 原样带回：命中同 URL 时让 CDN 直接回缓存
	if cc := hdr.Get("Cache-Control"); cc != "" {
		setRespHeader(ctx, "Cache-Control", cc)
	}
	env, decErr := decodeEnvelope(raw)
	if decErr != nil || (status >= 400 && env.Error == nil) {
		return failBody(status, pathOf(r), &errorInfo{Code: "upstream.failed", Message: "服务内部错误，请稍后重试", Details: jsonRaw(string(raw))})
	}
	if status >= 400 {
		return failBody(status, pathOf(r), env.Error)
	}
	var data struct {
		Color string `json:"color"`
	}
	if err := decodeInto(env.Data, &data); err != nil || data.Color == "" {
		return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "取色服务返回了无法解析的数据，请稍后重试"}}
	}
	return status, okEnvelope(map[string]any{"data": data.Color})
}

// addFlink: POST /update/flink/addFlink → POST /links/create
// 字段名两边一致（group/id/name/url/avatar/description/color/latency）。
// 旧后端是 update_one(..., upsert=True)：分组不存在会**静默建组**再塞链
// （update/flink/main.py addFlink）。新 /links/create 要求分组已存在
// （404 link.not_found），所以先保证组在、再转发创建；创建仍撞上
// link.not_found（查建之间组被删的竞态）就再补一轮，两轮后按旧契约回 success。
func addFlink(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	body, err := readBody(r)
	if err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体读取失败，请重试"}}
	}
	var b struct {
		Group string `json:"group"`
	}
	if err := json.Unmarshal(body, &b); err != nil || b.Group == "" {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求缺少 group 字段"}}
	}
	for attempt := 0; attempt < 2; attempt++ {
		if status, resp := ensureFlinkGroup(ctx, c, r, b.Group); status != 0 {
			return status, resp
		}
		status, raw, err := c.Do(ctx, http.MethodPost, "/links/create", nil, json.RawMessage(body), forwardHeaders(r))
		if err != nil {
			return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务暂时不可用，请稍后重试"}}
		}
		if status < 400 {
			if _, decErr := decodeEnvelope(raw); decErr != nil {
				return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务返回了无法解析的数据，请稍后重试"}}
			}
			return 200, okEnvelope(nil)
		}
		env, decErr := decodeEnvelope(raw)
		if decErr == nil && env.Error != nil && env.Error.Code == "link.not_found" {
			continue // 组在 ensure 与 create 之间被删，重走一轮
		}
		if decErr != nil || env.Error == nil {
			return failBody(status, pathOf(r), &errorInfo{Code: "upstream.failed", Message: "服务内部错误，请稍后重试", Details: jsonRaw(string(raw))})
		}
		return failBody(status, pathOf(r), env.Error)
	}
	// 两轮都被竞态打断：最后一次 ensure 已把组建回，按旧契约静默成功
	return 200, okEnvelope(nil)
}

// ensureFlinkGroup 保证分组存在（旧 addFlink upsert 的前半段）。
// 返回 (0, nil) 表示组已在或已建好；否则把要回给客户端的错误原样带出。
func ensureFlinkGroup(ctx context.Context, c *upstream.Client, r *http.Request, group string) (int, any) {
	// 查组用列表接口（友链总量百级，一次拉全没问题）；列表是公开读，Authorization 透传无害
	status, raw, err := c.Do(ctx, http.MethodGet, "/links/list", nil, nil, forwardHeaders(r))
	if err != nil {
		return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务暂时不可用，请稍后重试"}}
	}
	if status >= 400 {
		env, decErr := decodeEnvelope(raw)
		if decErr != nil || env.Error == nil {
			return failBody(status, pathOf(r), &errorInfo{Code: "upstream.failed", Message: "服务内部错误，请稍后重试", Details: jsonRaw(string(raw))})
		}
		return failBody(status, pathOf(r), env.Error)
	}
	env, err := decodeEnvelope(raw)
	if err != nil {
		return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务返回了无法解析的数据，请稍后重试"}}
	}
	var groups []struct {
		Name string `json:"name"`
	}
	if err := decodeInto(env.Data, &groups); err != nil {
		return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务返回了无法解析的数据，请稍后重试"}}
	}
	for _, g := range groups {
		if g.Name == group {
			return 0, nil
		}
	}
	// 旧 upsert 建出的组只有 name；order/description 用新接口的缺省值
	st, raw, err := c.Do(ctx, http.MethodPost, "/links/groups/create", nil,
		map[string]any{"name": group, "order": 0, "description": ""}, forwardHeaders(r))
	if err != nil {
		return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务暂时不可用，请稍后重试"}}
	}
	if st >= 400 {
		// 并发下别的请求刚好把组建好了：409 当成功，继续塞链
		env, decErr := decodeEnvelope(raw)
		if decErr == nil && env.Error != nil && env.Error.Code == "link.group_conflict" {
			return 0, nil
		}
		if decErr != nil || env.Error == nil {
			return failBody(st, pathOf(r), &errorInfo{Code: "upstream.failed", Message: "服务内部错误，请稍后重试", Details: jsonRaw(string(raw))})
		}
		return failBody(st, pathOf(r), env.Error)
	}
	return 0, nil
}

// updateFlink: PUT /update/flink/updateFlink → PUT /links/update
// 友链不存在旧后端也是静默成功，翻译回 success（GO-002）。
func updateFlink(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return forwardBodySilent(ctx, c, r, http.MethodPut, "/links/update", codeIn("link.not_found"))
}

// deleteFlink: DELETE /update/flink/deleteFlink?group=&id= → DELETE /links/delete?group=&id=
// 旧接口就是 query 参数，直接透传。
func deleteFlink(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	q := url.Values{"group": {r.URL.Query().Get("group")}, "id": {r.URL.Query().Get("id")}}
	return callUpstream(ctx, c, r, http.MethodDelete, "/links/delete", q, nil, detailObject,
		func(env *envelope) (int, any) { return 200, okEnvelope(nil) })
}

// moveFlinkGroup: POST /update/flink/moveFlinkGroup → POST /links/move
// 旧字段 from_group/to_group/link_id 是 snake_case，新接口是
// fromGroup/toGroup/id——这里必须改名，别原样转发。
// 链接或源分组不存在旧后端也是静默成功，翻译回 success（GO-002）。
func moveFlinkGroup(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	body, err := readBody(r)
	if err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体读取失败，请重试"}}
	}
	var old struct {
		FromGroup string `json:"from_group"`
		ToGroup   string `json:"to_group"`
		LinkID    string `json:"link_id"`
	}
	if err := json.Unmarshal(body, &old); err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体格式不正确"}}
	}
	return callUpstreamSilent(ctx, c, r, http.MethodPost, "/links/move", nil, map[string]any{
		"fromGroup": old.FromGroup,
		"toGroup":   old.ToGroup,
		"id":        old.LinkID,
	}, detailObject, codeIn("link.not_found"), func(env *envelope) (int, any) { return 200, okEnvelope(nil) })
}

// addGroup: POST /update/flink/addGroup → POST /links/groups/create（{name,order,description} 一致）
// 注意：重名时旧后端 insert_one 会**真的插入一条重复分组**（然后列表里出现两个同名组），
// 新后端回 409 link.group_conflict。这条不映射回静默成功——假装建成功但实际没建，
// 比让 dashboard 看到「分组名已存在」更糟；正常流程 dashboard 本来就不允许重名。
func addGroup(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return forwardBody(ctx, c, r, http.MethodPost, "/links/groups/create")
}

// updateGroup: PUT /update/flink/updateGroup → PUT /links/groups/update
// 旧字段 old_name → 新字段 oldName。组不存在旧后端也是静默成功（GO-002）。
func updateGroup(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return renameBody(ctx, c, r, http.MethodPut, "/links/groups/update",
		map[string]string{"old_name": "oldName"}, codeIn("link.not_found"))
}

// deleteGroup: DELETE /update/flink/deleteGroup?name= → DELETE /links/groups/delete?name=
func deleteGroup(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	q := url.Values{"name": {r.URL.Query().Get("name")}}
	return callUpstream(ctx, c, r, http.MethodDelete, "/links/groups/delete", q, nil, detailObject,
		func(env *envelope) (int, any) { return 200, okEnvelope(nil) })
}

// dispatchCheckLatencyWorkflow: GET /update/flink/dispatchCheckLatencyWorkflow
// → POST /links/check。方法变化由胶水内部消化（旧前端发的是 GET）。
func dispatchCheckLatencyWorkflow(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return callUpstream(ctx, c, r, http.MethodPost, "/links/check", nil, nil, detailObject,
		func(env *envelope) (int, any) { return 200, okEnvelope(nil) })
}

// ---------- 友链申请（公开 + 后台 + 内部回写） ----------

// applyStatus: GET /flink/apply/status?email=&name= → GET /links/applications/status
func applyStatus(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	q := url.Values{"email": {r.URL.Query().Get("email")}, "name": {r.URL.Query().Get("name")}}
	return passData(ctx, c, r, http.MethodGet, "/links/applications/status", q)
}

// flinkApply: POST /flink/apply → POST /links/applications/apply
// body 字段一致（name/link/avatar/descr/email/color/type/targetUrl）。
// 响应要把新 data 里的两个布尔提到顶层：{message, screeningTriggered, mailSent}。
// 为什么公开：友链申请本来就是访客可用的功能，鉴权/限流都在新后端。
func flinkApply(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return callUpstream(ctx, c, r, http.MethodPost, "/links/applications/apply", nil, mustJSONBody(r), detailObject,
		func(env *envelope) (int, any) {
			var data struct {
				ScreeningTriggered bool `json:"screeningTriggered"`
				MailSent           bool `json:"mailSent"`
			}
			if err := decodeInto(env.Data, &data); err != nil {
				return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务返回了无法解析的数据，请稍后重试"}}
			}
			return 200, okEnvelope(map[string]any{
				"screeningTriggered": data.ScreeningTriggered,
				"mailSent":           data.MailSent,
			})
		})
}

// listApplications: GET /update/flink/applications → GET /links/applications/list
// 新接口 data 就是申请数组（createdAt 升序），与旧响应形状一致。
func listApplications(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return passData(ctx, c, r, http.MethodGet, "/links/applications/list", nil)
}

// approveApplication: POST /update/flink/approveApplication
// → POST /links/applications/decide（补 decision:"approved"，可选字段挪进 overrides）
// 响应 {message, mailSent}。
func approveApplication(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return decideApplication(ctx, c, r, "approved")
}

// rejectApplication: POST /update/flink/rejectApplication
// → POST /links/applications/decide（decision:"rejected"，body 只带 id/reason）
func rejectApplication(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return decideApplication(ctx, c, r, "rejected")
}

// decideApplication 把旧的 approve/reject 两条合并到新 decide。
// 旧 body：{id, group, name?, link?, avatar?, descr?, color?} / {id, reason?}。
// 新 body：{id, decision, group?, overrides?, reason?}。
func decideApplication(ctx context.Context, c *upstream.Client, r *http.Request, decision string) (int, any) {
	body, err := readBody(r)
	if err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体读取失败，请重试"}}
	}
	var old struct {
		ID     string  `json:"id"`
		Group  string  `json:"group"`
		Name   *string `json:"name"`
		Link   *string `json:"link"`
		Avatar *string `json:"avatar"`
		Descr  *string `json:"descr"`
		Color  *string `json:"color"`
		Reason *string `json:"reason"`
	}
	if err := json.Unmarshal(body, &old); err != nil || old.ID == "" {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求缺少 id 字段"}}
	}

	newBody := map[string]any{"id": old.ID, "decision": decision}
	// approve 允许审批时改内容：有值（含空串覆盖）才进 overrides，语义与旧后端
	// 「None 则沿用申请原值」一致
	if decision == "approved" {
		overrides := map[string]any{}
		if old.Name != nil {
			overrides["name"] = *old.Name
		}
		if old.Link != nil {
			overrides["link"] = *old.Link
		}
		if old.Avatar != nil {
			overrides["avatar"] = *old.Avatar
		}
		if old.Descr != nil {
			overrides["descr"] = *old.Descr
		}
		if old.Color != nil {
			overrides["color"] = *old.Color
		}
		newBody["group"] = old.Group
		if len(overrides) > 0 {
			newBody["overrides"] = overrides
		}
	}
	if old.Reason != nil {
		newBody["reason"] = *old.Reason
	}

	return callUpstream(ctx, c, r, http.MethodPost, "/links/applications/decide", nil, newBody, detailObject,
		func(env *envelope) (int, any) {
			var data struct {
				MailSent bool `json:"mailSent"`
			}
			_ = decodeInto(env.Data, &data)
			return 200, okEnvelope(map[string]any{"mailSent": data.MailSent})
		})
}

// flinkScreening: POST /flink/screening → POST /links/applications/screening
// 初筛回写由 GitHub Actions runner 调用。旧鉴权头是 x-screening-secret，
// 新接口统一 X-Internal-Token；body（id/verdict/backlink/summary/reason/model）一致。
// 响应旧格式只有 {message:"success"}。
func flinkScreening(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	body, err := readBody(r)
	if err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体读取失败，请重试"}}
	}
	// 兼容两种密钥位置：旧调用方放头里；万一把 secret 放 body（同 pushFlinkStatus
	// 的习惯）也接受，避免 workflow 改造时两边改不齐
	secret := r.Header.Get("x-screening-secret")
	if secret == "" {
		var probe struct {
			Secret string `json:"secret"`
		}
		_ = json.Unmarshal(body, &probe)
		secret = probe.Secret
	}
	hdr := forwardHeaders(r)
	hdr["X-Internal-Token"] = secret
	status, raw, err := c.Do(ctx, http.MethodPost, "/links/applications/screening", nil, json.RawMessage(body), hdr)
	if err != nil {
		return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务暂时不可用，请稍后重试"}}
	}
	if status >= 400 {
		env, decErr := decodeEnvelope(raw)
		if decErr != nil || env.Error == nil {
			return failBody(status, pathOf(r), &errorInfo{Code: "upstream.failed", Message: "服务内部错误，请稍后重试", Details: jsonRaw(string(raw))})
		}
		return failBody(status, pathOf(r), env.Error)
	}
	return 200, okEnvelope(nil)
}

// pushFlinkStatus: POST /update/flink/pushFlinkStatus → POST /links/latency
// 旧 body {data:{linkStatus:[{id,latency}]}, secret}：secret 在 body 明文；
// 新接口 items:[...] + X-Internal-Token 头。胶水做形状转换 + secret 挪头。
// 响应旧格式只有 {message:"success"}。
func pushFlinkStatus(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	body, err := readBody(r)
	if err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体读取失败，请重试"}}
	}
	var old struct {
		Data struct {
			LinkStatus []struct {
				ID      string      `json:"id"`
				Latency interface{} `json:"latency"` // 数值透传，不假设精度
			} `json:"linkStatus"`
		} `json:"data"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(body, &old); err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体格式不正确"}}
	}
	items := make([]map[string]any, 0, len(old.Data.LinkStatus))
	for _, item := range old.Data.LinkStatus {
		items = append(items, map[string]any{"id": item.ID, "latency": item.Latency})
	}
	hdr := forwardHeaders(r)
	hdr["X-Internal-Token"] = old.Secret
	return callUpstreamWithHeader(ctx, c, r, http.MethodPost, "/links/latency", nil,
		map[string]any{"items": items}, hdr, detailObject,
		func(env *envelope) (int, any) { return 200, okEnvelope(nil) })
}

// ---------- 公共：写接口转发 ----------

// forwardBody 把 JSON body 原样转发（字段名一致的写接口都用它），
// 成功统一回旧格式 {message:"success"}。
func forwardBody(ctx context.Context, c *upstream.Client, r *http.Request,
	method, upstreamPath string,
) (int, any) {
	return forwardBodySilent(ctx, c, r, method, upstreamPath, nil)
}

// forwardBodySilent 同 forwardBody，但上游错误命中 match 时按旧契约静默成功
// （见 callUpstreamSilent 的注释，GO-002）。
func forwardBodySilent(ctx context.Context, c *upstream.Client, r *http.Request,
	method, upstreamPath string, match silentMatch,
) (int, any) {
	body, err := readBody(r)
	if err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体读取失败，请重试"}}
	}
	return callUpstreamSilent(ctx, c, r, method, upstreamPath, nil, json.RawMessage(body), detailObject, match,
		func(env *envelope) (int, any) { return 200, okEnvelope(nil) })
}

// renameBody 按 mapping 把旧字段名改成新字段名后转发（值原样保留）。
// 只用于 moveFlinkGroup / updateGroup 这类 snake_case → camelCase 的差异。
// match 非 nil 时，「目标不存在」类错误按旧契约静默成功。
func renameBody(ctx context.Context, c *upstream.Client, r *http.Request,
	method, upstreamPath string, mapping map[string]string, match silentMatch,
) (int, any) {
	body, err := readBody(r)
	if err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体读取失败，请重试"}}
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体格式不正确"}}
	}
	renamed := map[string]json.RawMessage{}
	for key, value := range raw {
		if newName, ok := mapping[key]; ok {
			renamed[newName] = value
		} else {
			renamed[key] = value
		}
	}
	return callUpstreamSilent(ctx, c, r, method, upstreamPath, nil, renamed, detailObject, match,
		func(env *envelope) (int, any) { return 200, okEnvelope(nil) })
}

// callUpstreamWithHeader 与 callUpstream 相同，但允许显式指定透传头
// （X-Internal-Token 这类服务间鉴权头不在 forwardHeaders 白名单里）。
func callUpstreamWithHeader(ctx context.Context, c *upstream.Client, r *http.Request,
	method, upstreamPath string, q url.Values, body any, hdr map[string]string, kind detailKind,
	rewrite func(env *envelope) (int, any),
) (int, any) {
	status, raw, err := c.Do(ctx, method, upstreamPath, q, body, hdr)
	if err != nil {
		return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务暂时不可用，请稍后重试"}}
	}
	if status >= 400 {
		env, decErr := decodeEnvelope(raw)
		if decErr != nil || env.Error == nil {
			return failBody(status, pathOf(r), &errorInfo{Code: "upstream.failed", Message: "服务内部错误，请稍后重试", Details: jsonRaw(string(raw))})
		}
		return failBody(status, pathOf(r), env.Error)
	}
	env, err := decodeEnvelope(raw)
	if err != nil {
		return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务返回了无法解析的数据，请稍后重试"}}
	}
	return rewrite(env)
}
