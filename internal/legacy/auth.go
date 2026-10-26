package legacy

import (
	"context"
	"net/http"

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
// （新后端负责写时间戳 + 通知博客刷缓存，胶水只转发）
func latestUpdateTimeWrite(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return callUpstream(ctx, c, r, http.MethodPost, "/site/update-time", nil, nil, detailObject,
		func(env *envelope) (int, any) { return 200, okEnvelope(nil) })
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
