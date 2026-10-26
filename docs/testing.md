# 测试方案 · glue

> 这个项目的全部价值就是「翻译正确」，所以测试的重点只有一个：
> **给定一份新格式响应，输出的旧格式必须逐字节对得上**。

## 1. 三层

```
第 1 层  静态守卫  bash scripts/checks.sh
第 2 层  单元测试  go test ./...            转换函数，不需要网络
第 3 层  冒烟      python3 tests/smoke.py   需要 glue + 新后端都在跑
```

## 2. 转换函数单测（最重要）

每个域一个 `*_test.go`，表驱动：

```go
cases := []struct {
    name     string
    upstream string          // 新后端会返回的 JSON（原样）
    want     string          // 旧接口应该返回的 JSON
    wantCode int
}{ … }
```

必须覆盖：

| 用例 | 检查点 |
|---|---|
| 列表 + count | `meta.count` → 旧接口的 `count` 或 `totalCount` |
| `countOnly` 的上游形态（`data:null`）| 旧接口只输出 `{message,count}` |
| 分页换算 | `startl=32&endl=48` → 上游收到 `page=3&pageSize=16`（用一个记录请求的假 client 断言）|
| `type=full` | 上游收到 `pageSize=full&withContent=true` |
| 上游 404 | 状态码 404 + `{"detail":{"message":"fail","error":"…"}}` |
| 上游 401 | `{"detail":"Invalid token"}`（**字符串**）|
| 上游 500 | 502/500 + `detail.error` 里包含 `details.cause` 的内容 |
| 上游返回非 JSON | 502，不 panic |
| 图片上传 | `{"message":"success","data":{"data":{"links":{"url":"…"}}}}` |
| navigation | `previous ← newer`、`next ← older` |

## 3. 端到端快照

`tests/golden_test.go`：

1. 用 `httptest` 起假上游，按路径返回 `tests/testdata/upstream/<name>.json`
2. 起 glue 的 `http.Handler`
3. 请求旧路径，和 `tests/testdata/legacy/<name>.json` 比对

```bash
go test ./tests -run TestGolden -update
go test ./tests -run TestGolden
```

## 4. 冒烟

`tests/smoke.py` 对真实运行的两个服务跑，检查 27 条公开 GET：

- `message == "success"`
- 有 `data` 或 `count` / `time` / `totalCount`
- **不含** `ok` / `requestId` / `error`（出现说明没翻译干净）
- `/get/post/postBySlug?slug=不存在` → 404 且 `detail.error` 是中文

```bash
python3 tests/smoke.py --base http://127.0.0.1:2334 --upstream http://127.0.0.1:2333
```
