// Package config 加载 glue 的唯一配置入口 config.yml。
//
// 与后端同一套约定：敏感值写成 {{ env.X }}，启动时解析，缺了就报错退出。
// 环境变量文件按 .env.dev → .env 的顺序找（后者是 Vercel CLI 的本地约定），
// 只用来补 UPSTREAM 这一个变量，不会覆盖进程里已有的环境变量。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	glue "github.com/LYXOfficial/ariablognext-backend-glue"
	"gopkg.in/yaml.v3"
)

// Config 是 config.yml 的运行时形态。
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Upstream UpstreamConfig `yaml:"upstream"`
	CORS     CORSConfig     `yaml:"cors"`
	Debug    DebugConfig    `yaml:"debug"`
}

type ServerConfig struct {
	// 留空表示由 PORT 环境变量决定（Vercel 注入），再退化到本地默认 127.0.0.1:2334
	Listen  string        `yaml:"listen"`
	Timeout time.Duration `yaml:"timeout"`
}

type UpstreamConfig struct {
	// 新后端的 base 地址，不带结尾斜杠
	Base    string        `yaml:"base"`
	Timeout time.Duration `yaml:"timeout"`
	// 透传给上游的请求头白名单：避免把浏览器的 Host / Cookie 等一起带过去
	ForwardHeaders []string `yaml:"forwardHeaders"`
}

type CORSConfig struct {
	AllowOrigins     []string `yaml:"allowOrigins"`
	AllowCredentials bool     `yaml:"allowCredentials"`
	AllowHeaders     []string `yaml:"allowHeaders"`
	AllowMethods     []string `yaml:"allowMethods"`
}

type DebugConfig struct {
	// true 时响应头带 X-Upstream-Status，方便和旧后端逐条比对
	EchoUpstreamStatus bool `yaml:"echoUpstreamStatus"`
}

// Load 读 envFileDir 下的 .env.dev / .env，再解析 dir/config.yml。
// 两个目录通常都是同一个（进程工作目录），拆开只是为了测试方便。
//
// dir/config.yml 不存在时回退到编进二进制的默认配置（根目录 embed.go）：
// Vercel 的 Go 函数包里没有仓库文件，线上全靠 embed + 环境变量；
// 本地磁盘上有文件则以磁盘为准（改配置不用重新编译）。
func Load(dir, envFileDir string) (*Config, error) {
	loadEnvFiles(envFileDir)

	raw, err := os.ReadFile(filepath.Join(dir, "config.yml"))
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("读 config.yml 失败: %w", err)
		}
		raw = glue.ConfigYAML // 编译期拷贝，见根目录 embed.go
	}
	expanded, err := expandEnvPlaceholders(string(raw))
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("解析 config.yml 失败: %w", err)
	}
	applyDefaults(&cfg)

	if cfg.Upstream.Base == "" {
		return nil, fmt.Errorf("upstream.base 为空：请在 .env.dev（或环境变量）里配置 UPSTREAM")
	}
	if cfg.Server.Timeout <= cfg.Upstream.Timeout {
		// 整体处理时限必须大于上游时限，否则上游还没超时、外层先掐断了
		return nil, fmt.Errorf("server.timeout(%s) 必须大于 upstream.timeout(%s)",
			cfg.Server.Timeout, cfg.Upstream.Timeout)
	}
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Timeout <= 0 {
		cfg.Server.Timeout = 20 * time.Second
	}
	if cfg.Upstream.Timeout <= 0 {
		cfg.Upstream.Timeout = 15 * time.Second
	}
	if len(cfg.CORS.AllowOrigins) == 0 {
		cfg.CORS.AllowOrigins = []string{"*"}
	}
	if len(cfg.CORS.AllowMethods) == 0 {
		cfg.CORS.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	}
	if len(cfg.CORS.AllowHeaders) == 0 {
		cfg.CORS.AllowHeaders = []string{"*"}
	}
	cfg.Upstream.Base = strings.TrimRight(cfg.Upstream.Base, "/")
}

// ListenAddr 决定监听地址：config 显式配置 > PORT（Vercel 注入）> 本地默认。
func (c *Config) ListenAddr() string {
	if c.Server.Listen != "" {
		return c.Server.Listen
	}
	if port := os.Getenv("PORT"); port != "" {
		return ":" + port
	}
	return "127.0.0.1:2334"
}

// expandEnvPlaceholders 把 {{ env.X }} 替换成环境变量 X 的值。
// 与后端不同，这里允许「内联占位符」（base 是整串 URL，不带密码），
// 所以逐段替换字符串而不是先替换再解析 YAML。
// 注释行（首个非空白字符是 #）不参与替换：config.yml 的头注里就有
// {{ env.X }} 示例，替换器不能把说明文字当成真配置。
func expandEnvPlaceholders(text string) (string, error) {
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "#") {
			b.WriteString(line)
			b.WriteString("\n")
			continue
		}
		expanded, err := expandLine(line)
		if err != nil {
			return "", err
		}
		b.WriteString(expanded)
		b.WriteString("\n")
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

// expandLine 替换单行内的 {{ env.X }}。
func expandLine(line string) (string, error) {
	out := line
	for {
		start := strings.Index(out, "{{ env.")
		if start < 0 {
			return out, nil
		}
		rest := out[start+len("{{ env."):]
		end := strings.Index(rest, "}}")
		if end < 0 {
			return "", fmt.Errorf("config.yml 里有未闭合的 {{ env.X }} 占位符")
		}
		name := strings.TrimSpace(rest[:end])
		value := os.Getenv(name)
		if value == "" {
			return "", fmt.Errorf("环境变量 %s 未设置（config.yml 引用了它）", name)
		}
		out = out[:start] + value + rest[end+len("}}"):]
	}
}

// loadEnvFiles 按 .env.dev → .env 顺序加载 KEY=VALUE。
// 已存在的进程环境变量优先（不覆盖），这样 Vercel / 命令行注入的值赢。
func loadEnvFiles(dir string) {
	for _, name := range []string{".env.dev", ".env"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "export ")
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			value = strings.Trim(strings.TrimSpace(value), `"'`)
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, value)
			}
		}
	}
}
