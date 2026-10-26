// Package legacy 是胶水的核心：把旧后端的 72 条路径翻译到新后端。
//
// 唯一的正确性标准是 docs/glue.md §4 的映射表，逐行实现，不要发挥。
// 所有转换函数签名统一：
//
//	func(ctx context.Context, c *upstream.Client, r *http.Request) (int, any)
//
// 返回的 any 是**已经拼好的旧格式响应体**（map / string / 原始字节）。
package legacy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ---------- 新后端统一信封 ----------

// envelope 是新后端响应的最小解码形态：只取胶水需要的字段，
// data 保留原始字节，各条接口自己再按需解一层。
type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Meta  *metaInfo       `json:"meta"`
	Error *errorInfo      `json:"error"`
}

type metaInfo struct {
	Count *int64 `json:"count"` // 指针：countOnly 时 meta 只有 count，别把缺省 0 当真值
}

type errorInfo struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details"`
}

// decodeEnvelope 解析上游响应。上游返回非 JSON（比如网关 502 的 HTML）时
// 返回错误，调用方转成旧格式的对象型 detail，绝不 panic——
// 翻译不干净时前端白屏但状态码还是 200，宁可失败得响亮一点。
func decodeEnvelope(raw []byte) (*envelope, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("上游返回的不是 JSON: %w（原文前 120 字节: %.120s）", err, raw)
	}
	return &env, nil
}

// upCount 取 meta.count（没有时返回 0）。
func (e *envelope) upCount() int64 {
	if e.Meta == nil || e.Meta.Count == nil {
		return 0
	}
	return *e.Meta.Count
}

// ---------- 旧格式响应体 ----------

// okEnvelope 拼旧格式成功体：{"message":"success", ...extra}。
func okEnvelope(extra map[string]any) map[string]any {
	out := map[string]any{"message": "success"}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// ---------- 失败重排 ----------

// detailKind 决定失败体里 detail 的形态。旧接口（FastAPI）的 HTTPException
// detail 有两种：对象 {"message":"fail","error":"…"} 和裸字符串。
// 按路径区分，不能统一——前端按路径写死了解析方式（见 glue.md §3.2）。
type detailKind int

const (
	// detailObject 绝大多数路径：{"detail":{"message":"fail","error":"…"}}
	detailObject detailKind = iota
	// detailString 三个特例：verify 401、login 403、addPost/addDraft 409
	detailString
)

// stringDetailText 指定「哪条路径 + 哪个状态码」用字符串 detail 及其文案。
// 文案与旧后端逐字一致，前端依赖的是状态码，但照抄最稳妥。
func stringDetailText(path string, status int) (string, bool) {
	switch {
	case strings.HasSuffix(path, "/access/user/verify") && status == 401:
		return "Invalid token", true
	case strings.HasSuffix(path, "/access/user/login") && status == 403:
		return "Invalid credentials", true
	case (strings.HasSuffix(path, "/update/post/addPost") ||
		strings.HasSuffix(path, "/update/draft/addDraft")) && status == 409:
		return "slug already exists", true
	}
	return "", false
}

// failBody 把新后端的错误信封重排成旧格式。
//   - error.message 是给人看的中文，进 detail.error
//   - details.fields（字段校验）用「；」连成一句拼在后面——旧接口每次只回一句
//   - details.cause（内部错误原始报错）拼上，对齐旧接口把 str(e) 吐出来的行为
func failBody(status int, path string, errInfo *errorInfo) (int, any) {
	if text, ok := stringDetailText(path, status); ok {
		return status, map[string]any{"detail": text}
	}
	if errInfo == nil {
		// 上游失败但没带错误信封（理论上不该发生）：给一句能看的
		return status, map[string]any{"detail": map[string]any{"message": "fail", "error": "服务暂时不可用，请稍后重试"}}
	}

	errorText := errInfo.Message
	var details struct {
		Cause  string `json:"cause"`
		Fields []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"fields"`
	}
	if len(errInfo.Details) > 0 {
		if err := json.Unmarshal(errInfo.Details, &details); err == nil {
			if len(details.Fields) > 0 {
				msgs := make([]string, 0, len(details.Fields))
				for _, f := range details.Fields {
					if f.Message != "" {
						msgs = append(msgs, f.Message)
					}
				}
				if len(msgs) > 0 {
					errorText = errorText + "：" + strings.Join(msgs, "；")
				}
			}
			if details.Cause != "" {
				errorText = errorText + "：" + details.Cause
			}
		}
	}
	return status, map[string]any{
		"detail": map[string]any{"message": "fail", "error": errorText},
	}
}

// legacyError 直接用一段文案构造对象型失败（给 archiveInfo 空结果这类
// 上游 200 但旧契约要求 404 的场合）。
func legacyError(status int, text string) (int, any) {
	return status, map[string]any{
		"detail": map[string]any{"message": "fail", "error": text},
	}
}
