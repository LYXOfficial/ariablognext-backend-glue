package legacy

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/LYXOfficial/ariablognext-backend-glue/internal/upstream"
)

// ---------- 站点信息 ----------

// lastUpdateTime: GET /get/siteInfo/lastUpdateTime → GET /site/info
// 新 data={"latestUpdateTime":N}，旧顶层叫 time。
func lastUpdateTime(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return callUpstream(ctx, c, r, http.MethodGet, "/site/info", nil, nil, detailObject,
		func(env *envelope) (int, any) {
			var data struct {
				LatestUpdateTime int64 `json:"latestUpdateTime"`
			}
			if err := decodeInto(env.Data, &data); err != nil {
				return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务返回了无法解析的数据，请稍后重试"}}
			}
			return 200, okEnvelope(map[string]any{"time": data.LatestUpdateTime})
		})
}

// latestUpdateTimeWrite: POST /update/siteInfo/latestUpdateTime → POST /site/update-time
//
// 旧后端（update/siteInfo/main.py）的行为是：忽略请求体、永远写「当前时间戳」，
// 成功后同步 GET https://blog.yaria.top/refreshCache/siteinfo 通知博客刷站点信息缓存
// （任何失败吞掉），最后返回 {"message":"success"}（无 data/time）。
//
// refreshCache 这个耦合必须留在 glue：新后端不发这个通知，而旧前端依赖它刷新
// 站点信息缓存。这是过渡期的兼容义务，将来重构新 API 时应该改前端去除，而不是
// 在这里「顺手清理」。转发时丢弃请求体也是同一原因——新接口支持 {"time":N} 覆写，
// 但旧契约里调用方带什么 body 都被忽略，不能让 dashboard 哪天带了 body 就悄悄
// 偏离旧语义。
func latestUpdateTimeWrite(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	// body 永远转发 `{}`：旧后端忽略请求体、永远写当前时间，所以旧调用方的
	// body 必须丢弃（不能透出 {"time":N} 覆写语义）；但新后端对「完全无 body」
	// 会回 400（真机实测），至少要有一个 JSON 对象才走缺省分支——`{}` 恰好
	// 同时满足这两条。
	status, body := callUpstream(ctx, c, r, http.MethodPost, "/site/update-time", nil, map[string]any{}, detailObject,
		func(env *envelope) (int, any) { return 200, okEnvelope(nil) })
	// 旧后端是「写库成功才调 refreshCache」，这里同样只在转发成功后通知；
	// fire-and-forget：博客侧抖动绝不能拖慢/搞挂后台的保存动作
	if status == 200 {
		go notifyRefreshCache()
	}
	return status, body
}

// refreshCache 的目标与客户端拆成包级变量，单测替换后就不会打真实公网。
var (
	refreshCacheURL    = "https://blog.yaria.top/refreshCache/siteinfo"
	refreshCacheClient = &http.Client{Timeout: 5 * time.Second}
)

// notifyRefreshCache 同步执行一次缓存刷新通知。任何失败（网络错误、非 2xx、
// 超时）都吞掉——旧后端对它的态度就是「尽力而为」，绝不让它影响主响应。
// 做成变量是为了让单测替换掉（不能真的打 blog.yaria.top）。
var notifyRefreshCache = func() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, refreshCacheURL, nil)
	if err != nil {
		return
	}
	resp, err := refreshCacheClient.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// ---------- 登录 / 校验 ----------
// 失败时 detail 是**字符串**（旧 FastAPI 行为），由 stringDetailText 按
// 「路径 + 状态码」决定，其它状态码仍是对象——login 500、verify 500 都该
// 保持对象形态。

// login: POST /access/user/login → POST /auth/login（body {user,password} 一致）
// 成功：{message, jwt:data.token}。
func login(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return callUpstream(ctx, c, r, http.MethodPost, "/auth/login", nil, mustJSONBody(r), detailString,
		func(env *envelope) (int, any) {
			var data struct {
				Token string `json:"token"`
			}
			if err := decodeInto(env.Data, &data); err != nil || data.Token == "" {
				return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "接口服务返回了无法解析的数据，请稍后重试"}}
			}
			return 200, okEnvelope(map[string]any{"jwt": data.Token})
		})
}

// verify: GET /access/user/verify → GET /auth/verify
// 成功只有 {message:"success"}；401 → {"detail":"Invalid token"}（字符串）。
// Authorization 原样透传，glue 不自己判断 token 是否有效。
func verify(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return callUpstream(ctx, c, r, http.MethodGet, "/auth/verify", nil, nil, detailString,
		func(env *envelope) (int, any) { return 200, okEnvelope(nil) })
}
