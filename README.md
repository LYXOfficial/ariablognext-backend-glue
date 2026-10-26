# ariablognext-backend-glue

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**过渡用的兼容层**：让 AriablogNext 的 blog 与 dashboard 在迁移到 Go 后端的过程中
**一行代码都不用改**。

> ⚠️ **当前状态：脚手架，业务代码尚未实现。** 进度见 `TASKS.md`。

---

## 它解决什么问题

新后端（`ariablognext-backend-go`）用的是全新的接口风格：

```
旧:  GET /get/post/postsInfo?startl=0&endl=16     →  {"message":"success","data":[…]}
新:  GET /posts/list?page=1&pageSize=16           →  {"ok":true,"data":[…],"meta":{…}}
```

前端暂时不动，于是中间加一层：

```
blog.yaria.top ─┐
                ├─> blognext-end.yaria.top ─> 【本项目】 ─> blognext-go.yaria.top
dashboard ──────┘        （旧域名）            翻译旧→新→旧      （新后端）
```

- **只有翻译，没有业务**：不连数据库、不做校验、不缓存
- **接管旧域名**，所以前端的 `backEndUrl` 一个字符都不用改
- 迁移期间可以按域灰度：前端先把某个域改成直连新后端，胶水里对应路由就下线
- 全部迁完后本项目归档（打 tag `final`，作为旧契约的可执行记录）

---

## 目录

```
cmd/server/          入口
internal/config/     config.yml 加载
internal/upstream/   调新后端的客户端（返回原始字节，方便单测）
internal/legacy/     72 条路由 + 每条的参数换算与响应重排
  routes.go          路由表
  envelope.go        信封重排公共函数
  posts.go draft.go taxonomy.go links.go speaks.go site.go images.go auth.go misc.go
config.yml           唯一配置入口
docs/glue.md         ★ 72 条逐条映射表（唯一事实来源）
docs/testing.md      测试方案
tests/smoke.py       旧契约冒烟测试
```

---

## 开发

```bash
go mod tidy
go run ./cmd/server   # 监听 127.0.0.1:2334（读 .env.dev 里的 UPSTREAM，默认指向本机 2333）

bash scripts/checks.sh
go test ./...
python3 tests/smoke.py --base http://127.0.0.1:2334 --upstream http://127.0.0.1:2333
```

---

## 文档

| 文件 | 内容 |
|---|---|
| [`TASKS.md`](TASKS.md) | 施工清单 |
| [`docs/glue.md`](docs/glue.md) | 72 条映射表 + 6 个特殊 case + 迁移路线 |
| [`docs/testing.md`](docs/testing.md) | 测试方案 |
| [`AGENTS.md`](AGENTS.md) | 工作守则 |

## License

[MIT](LICENSE)
