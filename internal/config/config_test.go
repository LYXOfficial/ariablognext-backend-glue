package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const minimalYAML = `
server:
  listen: ""
  timeout: 20s
upstream:
  base: "{{ env.UPSTREAM }}"
  timeout: 15s
  forwardHeaders: [Authorization, Content-Type, Accept]
cors:
  allowOrigins: ["*"]
`

func TestLoad_OK(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yml", minimalYAML)
	writeFile(t, dir, ".env.dev", "UPSTREAM=http://127.0.0.1:2333\n")

	t.Cleanup(func() { _ = os.Unsetenv("UPSTREAM") }) // loadEnvFiles 会写进程环境，测试完清掉
	cfg, err := Load(dir, dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Upstream.Base != "http://127.0.0.1:2333" {
		t.Errorf("base = %q", cfg.Upstream.Base)
	}
	if cfg.Server.Timeout != 20*time.Second || cfg.Upstream.Timeout != 15*time.Second {
		t.Errorf("timeout 默认值不对: %s / %s", cfg.Server.Timeout, cfg.Upstream.Timeout)
	}
	if addr := cfg.ListenAddr(); addr != "127.0.0.1:2334" {
		t.Errorf("ListenAddr 默认 = %q，想要 127.0.0.1:2334", addr)
	}
}

func TestLoad_MissingEnv(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yml", minimalYAML)
	// 不写 .env.dev，也不设环境变量 → 必须报错退出，而不是拿空地址跑
	if _, err := Load(dir, dir); err == nil {
		t.Fatal("UPSTREAM 缺失时应该报错")
	}
}

func TestLoad_PortOverriddenByEnv(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yml", minimalYAML)
	writeFile(t, dir, ".env.dev", "UPSTREAM=http://x\n")
	t.Setenv("PORT", "9099")
	t.Setenv("UPSTREAM", "http://x") // 防止其它用例经 .env 文件留下的值干扰

	cfg, err := Load(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if addr := cfg.ListenAddr(); addr != ":9099" {
		t.Errorf("PORT 注入时应监听 :9099，实际 %q", addr)
	}
}

func TestLoad_TimeoutRelationChecked(t *testing.T) {
	dir := t.TempDir()
	// server.timeout < upstream.timeout 属于配置错误，启动时就该拦下
	writeFile(t, dir, "config.yml", `
server: {timeout: 1s}
upstream: {base: "{{ env.UPSTREAM }}", timeout: 5s}
`)
	writeFile(t, dir, ".env.dev", "UPSTREAM=http://x\n")
	if _, err := Load(dir, dir); err == nil {
		t.Fatal("server.timeout <= upstream.timeout 应该报错")
	}
}

func TestLoad_EnvFileFallbackAndNoOverride(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yml", minimalYAML)
	// 只有 .env（没有 .env.dev）也要能加载；进程里已有的变量优先于文件
	writeFile(t, dir, ".env", "UPSTREAM=http://from-file:1\n")
	t.Setenv("UPSTREAM", "http://from-process:2")
	t.Cleanup(func() { _ = os.Unsetenv("UPSTREAM") })

	cfg, err := Load(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Upstream.Base != "http://from-process:2" {
		t.Errorf("进程环境变量应优先，实际 %q", cfg.Upstream.Base)
	}
}

func TestExpandEnvPlaceholders_Inline(t *testing.T) {
	t.Setenv("HOST_X", "1.2.3.4")
	got, err := expandEnvPlaceholders("http://{{ env.HOST_X }}:8080")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://1.2.3.4:8080" {
		t.Errorf("内联占位符替换失败: %q", got)
	}
	if _, err := expandEnvPlaceholders("{{ env.NOPE_MISSING"); err == nil {
		t.Error("未闭合占位符应报错")
	}
}
