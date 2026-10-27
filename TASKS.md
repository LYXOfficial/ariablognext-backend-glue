# 施工清单 · ariablognext-backend-glue

> 你（下一个模型）要把这份清单从上到下做完。这是个小项目，但**每一条转换都是契约**，
> 差一个字段名前端就白屏。

---

## 0. 你在做什么

一个**过渡用的翻译层**：让 blog 和 dashboard 一行代码都不改，就能跑在新的 Go 后端上。

```
blog.yaria.top ─┐
                ├─> blognext-end.yaria.top ─> 【本项目】─> blognext-go.yaria.top
dashboard ──────┘        （旧域名）              （翻译）        （新后端）
```

- 实现旧后端的 **72 条路径**
- 把旧的 offset/limit 参数换算成新的 page/pageSize
- 把新信封 `{ok, data, meta | error}` 拼回旧的 `{message:"success", …}` / `{detail:{message:"fail",error}}`
- **不连数据库、不做业务校验、不缓存**

必读：

| 文件 | 作用 |
|---|---|
| `docs/glue.md` | 72 条逐条映射表（§4）+ 6 个特殊 case（§5）。**这是唯一事实来源** |
| `docs/testing.md` | 测试方案 |
| `../ariablognext-backend-go/docs/api-draft.md` | 新接口契约（你转调的目标）|
| `config.yml` | 配置 |

---

## 1. 十条禁令

1. **不要改 `docs/glue.md` 的映射表。** 觉得不对就停下来问。
2. **不要在新后端里加兼容分支。** 兼容只存在于本项目。
3. **不要连数据库。** 这个项目没有数据库连接。
4. **不要自己造业务逻辑。** 校验（限流、判重、XSS）全部在新后端做，这里只转发请求+重排响应。
5. **不要吞掉上游错误。** 上游 4xx/5xx 要透传状态码，并把 `error.message` 放进 `detail.error`。
6. **`detail` 的两种形态都要会**：对象 `{"message":"fail","error":"…"}` 和字符串 `"Invalid token"`。
   哪个路径用哪种，看 `docs/glue.md §4` 的标注，不要统一。
7. **图片上传的嵌套不许「顺手清理」**：必须是 `data.data.links.url`。
8. **不要 `git commit` / `git push`。**
9. **不要引入清单外的依赖**（这个项目只需要标准库 + `yaml.v3`）。
10. **卡住超过 20 分钟就停下来问。**

---

## 2. 命令

```bash
go mod tidy
go run ./cmd/server                  # 默认 127.0.0.1:2334（按 .env.dev → .env 加载）
bash scripts/checks.sh
go test ./...
python3 tests/smoke.py --base http://127.0.0.1:2334 --upstream http://127.0.0.1:2333
```

---

## 3. 目录

```
cmd/server/main.go        入口：读配置 → 建 upstream client → 起 http.Server
internal/config/          config.yml 加载（照抄后端那份的精简版）
internal/upstream/        调新后端的 HTTP 客户端
internal/legacy/          72 条路由 + 每条的参数换算与响应重排
  routes.go               路由表（一个文件列全部 72 条）
  posts.go                文章域
  taxonomy.go             标签/分类/归档
  links.go                友链 + 友链申请
  drafts.go               草稿
  speaks.go               说说
  site.go                 站点信息
  images.go               图床
  auth.go                 登录/校验
  misc.go                 sitemap / export / 已删接口
  envelope.go             信封重排的公共函数
tests/
  golden_test.go          假上游 → 逐条比对旧格式
  testdata/*.json         期望的旧格式响应
```

---

## 4. 阶段 G0 · 骨架

| 文件 | 内容 |
|---|---|
| `config.yml` | `upstream`（`{{ env.UPSTREAM }}`）、`listen`、`timeout`、`cors` |
| `internal/config/config.go` | 可以直接抄后端 `internal/config` 的精简版（去掉数据库/邮箱那些校验）|
| `internal/upstream/client.go` | `func (c *Client) Do(ctx, method, path string, q url.Values, body any, hdr map[string]string) (int, []byte, error)`，返回**原始字节**（方便单测喂假数据）|
| `internal/legacy/envelope.go` | `okBody(data any, extra map[string]any) map[string]any`、`failBody(errObj)` 等公共函数 |
| `internal/legacy/routes.go` | `type Route struct{ Method, Path string; Auth bool; Handle Handler }`，`func All() []Route` |
| `cmd/server/main.go` | `http.ServeMux` + 遍历 `legacy.All()` 注册；CORS；监听 `$PORT` |

**验收**：`go build ./...` 通过，`GET /healthz` 返回 `{"message":"success"}`。

---

## 5. 阶段 G1 · 只读 27 条（先做这个，风险最高）

按 `docs/glue.md §4.1` 的表格**逐行实现**。每写完一个域就跑对应测试。

### 换算要点（错一条就会白屏）

| 旧参数 | 新参数 | 规则 |
|---|---|---|
| `startl` / `endl` | `page` / `pageSize` | `limit = endl-startl`；`page = startl/limit + 1`；`endl` 缺省 → `pageSize=200`（新后端 maxPageSize，2026-10-27 从 100 上调，见 convert.go 注释） |
| `type=full` | `pageSize=full&withContent=true` | 全量，不要循环翻页 |
| `query` | `keyword` | |
| `title` | `keyword` + `titleOnly=true` | |
| `tag` / `category` / `year`+`month` | 同名（转调 `/posts/list`）| 注意这些**都要转调 `/posts/list`**，不是 `/tags/list` |
| `url`（themeColor）| `url` | |

### 响应重排

```go
// 新：{"ok":true,"data":[...],"meta":{"count":83,"hasMore":true,...}}
// 旧：{"message":"success","data":[...]}
//     {"message":"success","data":[...],"totalCount":83}      ← archiveInfo/tagInfo/categoryInfo
//     {"message":"success","count":83}                        ← postCount/tagCount/speaksCount/...
//     {"message":"success","time":1761400000}                 ← lastUpdateTime
//     {"message":"success","jwt":"..."}                       ← login
```

**必须一次请求拿到 data 和 count**：新后端的 `list` 默认就返回 `meta.count`，不要再打一次 count 接口。

### 三个特例（`docs/glue.md §5`）

1. **`themeColor`**：错误必须是 `{"detail":{"message":"fail","error":"中文"}}`（对象，不是字符串），
   并且**透传 `Cache-Control`**、保留 CORS 头
2. **`postSummary`**：公开接口，新后端 `/posts/summary` 也是公开的，直接转
3. **`navigation`**：`previous` ← `data.newer`，`next` ← `data.older`（**方向是反的**）

### 验收

```bash
go test ./internal/legacy -run TestPosts -v
python3 tests/smoke.py --base http://127.0.0.1:2334 --upstream http://127.0.0.1:2333
```

---

## 6. 阶段 G2 · 后台读写

按 `docs/glue.md §4.2 / §4.3`。注意：

| 点 | 要求 |
|---|---|
| `Authorization` | **原样透传**，不要自己判断 token 是否有效（让新后端返 401）|
| `login` 失败 | 旧的是 `{"detail":"Invalid credentials"}`（**字符串**），保持 |
| `verify` 失败 | 同上，字符串 `"Invalid token"` |
| `addPost` 撞 slug | 旧的是 409 + `{"detail":"slug already exists"}`（字符串），保持 |
| 图片上传 | `{"message":"success","data":{"data":{"links":{"url":"…"}}}}` |
| `reuploadImage` | 同上嵌套 |
| `rehostImageByUrl` | `{"message":"success","data":{"url":"…"}}`（**没有**三层嵌套）|
| `refreshSummary` | `{"message":"success","data":"<摘要文本>"}`（data 是字符串，不是对象）|
| `approve/reject` | 拼 `{"decision":"approved"}` / `{"decision":"rejected"}`，返回 `{"message":"success","mailSent":bool}` |
| `pushFlinkStatus` | body `{data:{linkStatus:[…]}, secret}` → 新接口 `{items:[…]}` + `X-Internal-Token` 头 |
| `screening` | body 里的 `secret` → `X-Internal-Token` 头 |

---

## 7. 阶段 G3 · 特殊与已删

| 旧路径 | 处理 |
|---|---|
| `GET /get/sitemap/sitemapGen` | 直通 `/sitemap`，**原样返回 XML，不要 JSON 包装**，`Content-Type: application/xml` |
| `POST /get/export/collections` | 转 `/maintenance/export`，把 `data.collections` 提到顶层 |
| `DELETE /update/post/deleteRenderedHtmlCache` | 不转发，直接 `{"message":"success"}` |
| `DELETE /update/post/deleteAllRenderedHtmlCache` | 同上 |
| `DELETE /update/draft/deleteRenderedHtmlCache` | 同上 |

---

## 8. 测试要求（重点，别偷懒）

### 8.1 转换函数单测（`internal/legacy/*_test.go`）

每个域的转换都要有表驱动测试，**输入是新格式 JSON 字符串，期望是旧格式 JSON 字符串**：

```go
cases := []struct{ name, in, want string }{ … }
// 用 json.Unmarshal 后再用 reflect.DeepEqual 比，避免字符串顺序问题
```

必须覆盖的错误分支：

| 场景 | 期望 |
|---|---|
| 上游 404 + `post.not_found` | 输出 `{"detail":{"message":"fail","error":"没有找到这篇文章"}}` + 404 |
| 上游 401 + `auth.invalid_token` | 输出 `{"detail":"Invalid token"}` + 401（字符串形态）|
| 上游 500 + `details.cause` | 输出 `{"detail":{"message":"fail","error":"…"}}`，**包含 cause 内容** |
| 上游返回非 JSON | 输出 502，不要 panic |

### 8.2 端到端快照（`tests/golden_test.go`）

用 `httptest` 起一个**假上游**，对每个旧路径返回固定的新格式 JSON；
然后请求 glue，把结果与 `tests/testdata/<路径>.json` 比对。

```bash
go test ./tests -run TestGolden -update   # 首次生成
go test ./tests -run TestGolden           # 平时跑
```

### 8.3 冒烟（`tests/smoke.py`）

对**真实运行**的 glue + 真实新后端跑，检查 27 条公开 GET 的旧格式形状：

- 必须有 `message == "success"`
- 必须有 `data`（或 `count` / `time` / `totalCount`）
- **不许出现** `ok` / `requestId` / `error` 这些新信封字段（说明没翻译干净）
- `/get/post/postBySlug?slug=不存在` → 404 + `detail.error` 是中文

---

## 9. 通用验收

```bash
bash scripts/checks.sh      # 见脚本注释，主要检查：没有直连数据库、没有 import 后端 internal 包
go test ./...
go vet ./...
gofmt -l cmd internal
```

---

## 10. 交付前检查表

- [ ] `legacy.All()` 返回 **72** 条，且与 `docs/glue.md §4` 逐条核对过
- [ ] 27 条公开 GET 的冒烟全过
- [ ] 图片上传的三层嵌套与旧接口逐字节一致
- [ ] `navigation` 的 `previous`/`next` 方向正确（这是最容易搞反的一处）
- [ ] `sitemap` 返回 XML 而不是 JSON
- [ ] 三个已删接口返回 `{"message":"success"}` 而不是 404
- [ ] 没有 `git commit`
