// 路由注册 + HTTP 适配层：把转换函数的 (int, any) 返回值写进 http.ResponseWriter。
package legacy

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/LYXOfficial/ariablognext-backend-glue/internal/upstream"
)

// Options 控制 HTTP 适配层的行为。
type Options struct {
	// true 时在响应头带 X-Upstream-Status，方便和旧后端逐条比对
	EchoUpstreamStatus bool
}

// adapter 持有上游客户端与调试开关，给每条路由生成响应写出函数。
type adapter struct {
	client *upstream.Client
	echo   bool
}

// NewHTTPHandler 把 72 条路由注册到 ServeMux 并包上响应写出逻辑。
// 调用方（cmd/server）拿到的已经是完整的应用 handler，只需要再套 CORS/超时。
func NewHTTPHandler(client *upstream.Client, opts Options) http.Handler {
	a := &adapter{client: client, echo: opts.EchoUpstreamStatus}
	mux := http.NewServeMux()
	for _, route := range all {
		r := route // 显式拷贝，避免任何循环变量复用的歧义
		mux.HandleFunc(r.Path, a.writeResponse(r))
	}
	return mux
}

// writeResponse 包一条路由：跑转换函数 → 按返回值类型写响应。
// 返回 []byte（sitemap 的 XML）时原样写出、不做 JSON 编码；其余按旧契约编码。
func (a *adapter) writeResponse(route Route) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 方法对不上回 405：路径在但方法错是前端 bug，别让 mux 默认 404 混淆排查
		if r.Method != route.Method {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx, respHeader := withRespHeader(r.Context())
		status, body := route.Handle(ctx, a.client, r)

		if a.echo {
			// 转换函数没有把上游状态带出来（签名只有 int+any），
			// 这里回写胶水自己的状态；真正的比对走 tests/smoke.py 的双端 diff
			w.Header().Set("X-Glue-Status", "1")
		}
		for key, values := range respHeader {
			for _, v := range values {
				w.Header().Add(key, v)
			}
		}

		switch b := body.(type) {
		case []byte:
			// 原样直通的响应（sitemap XML）：Content-Type 由转换函数透传
			if w.Header().Get("Content-Type") == "" {
				w.Header().Set("Content-Type", "application/octet-stream")
			}
			w.WriteHeader(status)
			_, _ = w.Write(b)
		default:
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(status)
			// 字符串 detail（{"detail":"Invalid token"}）同样由 Encoder 正确编码
			if err := json.NewEncoder(w).Encode(body); err != nil {
				// 头已发出，只能记日志
				slog.Error("写响应失败", "path", r.URL.Path, "error", err)
			}
		}
	}
}
