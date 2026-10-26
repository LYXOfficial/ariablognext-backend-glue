// glue 服务入口：读配置 → 建上游客户端 → 注册 72 条旧路径 → 起 HTTP 服务。
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/LYXOfficial/ariablognext-backend-glue/internal/config"
	"github.com/LYXOfficial/ariablognext-backend-glue/internal/legacy"
	"github.com/LYXOfficial/ariablognext-backend-glue/internal/upstream"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load(".", ".")
	if err != nil {
		slog.Error("配置加载失败，退出", "error", err)
		os.Exit(1)
	}

	client := upstream.New(cfg.Upstream.Base, cfg.Upstream.Timeout, cfg.Upstream.ForwardHeaders)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		// 探活专用（旧后端没有这个路径）；返回旧格式风格，保持全家统一
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "success"})
	})
	// 72 条旧路径全部由 legacy 包注册
	mux.Handle("/", legacy.NewHTTPHandler(client, legacy.Options{
		EchoUpstreamStatus: cfg.Debug.EchoUpstreamStatus,
	}))

	var handler http.Handler = mux
	handler = withCORS(handler, cfg.CORS)
	// 整体处理时限必须盖过上游超时（config.Load 已校验大小关系）
	handler = withTimeout(handler, cfg.Server.Timeout)

	srv := &http.Server{
		Addr:              cfg.ListenAddr(),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("glue 启动", "addr", srv.Addr, "upstream", cfg.Upstream.Base)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("服务退出", "error", err)
		os.Exit(1)
	}
}

// withCORS 与旧后端 FastAPI CORSMiddleware 行为对齐：全放开（前端在别的域名上）。
func withCORS(next http.Handler, cfg config.CORSConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if len(cfg.AllowOrigins) == 1 && cfg.AllowOrigins[0] == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				for _, allowed := range cfg.AllowOrigins {
					if allowed == origin ||
						(strings.HasPrefix(allowed, "*.") && strings.HasSuffix(origin, strings.TrimPrefix(allowed, "*"))) {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						break
					}
				}
			}
			if cfg.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			// 预检请求：不进业务层，直接回 204
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(cfg.AllowMethods, ", "))
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.AllowHeaders, ", "))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withTimeout 给整个请求兜一个处理时限（含上游调用），防止慢上游拖死连接。
func withTimeout(next http.Handler, d time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
