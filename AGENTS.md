# AGENTS.md

给在这个仓库里干活的 AI / 新同学。**先读 `TASKS.md`。**

---

## 1. 这个仓库是什么

**过渡用的翻译层。** 让 blog / dashboard 一行代码都不改，就能跑在新的 Go 后端上。

- 占用旧域名 `blognext-end.yaria.top`（前端 `backEndUrl` 指向它）
- 收到旧路径 → 换算参数 → 转发给 `blognext-go.yaria.top` → 把新信封拼回旧格式
- **不连数据库、不做业务校验、不缓存**
- 迁移完成后整个仓库归档

---

## 2. 唯一的正确性标准

`docs/glue.md §4` 的 72 条映射表。**逐行实现，不要自己发挥。**

任何一条转换错了，前端的表现是**白屏**，而 HTTP 状态码还是 200——所以测试必须断言响应体，
不能只看 `res.ok`。

---

## 3. 硬性约束

`bash scripts/checks.sh` 会检查：

| 约束 | 原因 |
|---|---|
| 不许出现任何数据库代码 | 这个项目不该有状态 |
| 不许 import `ariablognext-backend-go/internal` | 两个仓库要能独立部署 |
| 不许 `fmt.Print*` | 日志走 `log/slog` |
| 每个 `internal/` 子包都要有 `*_test.go` | 转换逻辑必须有测试 |

---

## 4. 编码约定

- 注释中文，写**为什么**（尤其是「这个字段为什么这么拼」）
- 旧格式的错误体有两种：对象和字符串。**按路径区分**，不要统一（见 `docs/glue.md §3.2`）
- 环境变量放在 `.env.dev`（不进 git），只有 `UPSTREAM` 一个；启动按 `.env.dev` → `.env` 找
- 上游的 `Authorization` **原样透传**，不要自己判断 token
- 不要「顺手清理」旧接口的怪异结构（图片上传的三层嵌套是刻意的）
- 所有的转换函数签名统一：`func(ctx context.Context, c *upstream.Client, r *http.Request) (int, any)`，
  `any` 是已经拼好的旧格式响应体

---

## 5. 验证

```bash
bash scripts/checks.sh && go test ./...
python3 tests/smoke.py --base http://127.0.0.1:2334 --upstream http://127.0.0.1:2333
```

---

## 6. 不要做的事

- ❌ 改 `docs/glue.md` 的映射表（有疑问就停下来问）
- ❌ 在 glue 里实现业务逻辑（判重、限流、XSS 校验都在新后端）
- ❌ `git commit` / `git push`
- ❌ 引入标准库与 `yaml.v3` 之外的依赖
- ❌ 把这个项目「优化」成通用网关——它是要被删掉的
