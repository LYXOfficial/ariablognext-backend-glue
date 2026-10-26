package legacy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/LYXOfficial/ariablognext-backend-glue/internal/upstream"
)

// ---------- 图床 ----------
//
// 图床接口的特殊点是上传/重传的**三层嵌套**（glue.md §5.1）：
// 旧接口为了兼容 Vditor 的返回格式，响应是
//
//	{"message":"success","data":{"data":{"links":{"url":"…"}}}}
//
// dashboard 读 (await res.json()).data.data.links.url。这个嵌套是刻意的，
// 在胶水里显式构造，不要「顺手清理」。

// listImages: GET /update/image/listImages → GET /images/list
// 新 data={"images":[...]}，形状与旧响应一致，只换信封。
func listImages(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return passData(ctx, c, r, http.MethodGet, "/images/list", nil)
}

// uploadImage: POST /update/image/uploadImage（multipart: file）→ POST /images/upload
// multipart 原样透传（body 是原始字节，Content-Type 由客户端带 boundary）。
func uploadImage(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return imageUpload(ctx, c, r, http.MethodPost, "/images/upload")
}

// reuploadImage: PUT /update/image/reuploadImage（multipart: url+file）→ PUT /images/reupload
func reuploadImage(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	return imageUpload(ctx, c, r, http.MethodPut, "/images/reupload")
}

// imageUpload 转发 multipart 并把 data.url 包回三层嵌套。
func imageUpload(ctx context.Context, c *upstream.Client, r *http.Request, method, upstreamPath string) (int, any) {
	body, err := readBody(r)
	if err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体读取失败，请重试"}}
	}
	hdr := forwardHeaders(r)
	// multipart 的 Content-Type 必须带着 boundary 原样过去
	if ct := r.Header.Get("Content-Type"); ct != "" {
		hdr["Content-Type"] = ct
	}
	// 用带显式头的版本：callUpstream 会自己重算 forwardHeaders(r)，
	// multipart 的 Content-Type（含 boundary）就丢了
	return callUpstreamWithHeader(ctx, c, r, method, upstreamPath, nil, body, hdr, detailObject,
		func(env *envelope) (int, any) {
			var data struct {
				URL string `json:"url"`
			}
			if err := decodeInto(env.Data, &data); err != nil || data.URL == "" {
				return 502, map[string]any{"detail": map[string]any{"message": "fail", "error": "图床服务返回了无法解析的数据，请稍后重试"}}
			}
			return 200, okEnvelope(map[string]any{
				"data": map[string]any{
					"data": map[string]any{
						"links": map[string]any{"url": data.URL},
					},
				},
			})
		})
}

// rehostImageByUrl: POST /update/image/rehostImageByUrl（body {url}）→ POST /images/import
// 新 data={"url":…}，旧响应 data={"url":…}，只换信封。
func rehostImageByUrl(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	body, err := readBody(r)
	if err != nil {
		return 400, map[string]any{"detail": map[string]any{"message": "fail", "error": "请求体读取失败，请重试"}}
	}
	return callUpstream(ctx, c, r, http.MethodPost, "/images/import", nil, json.RawMessage(body), detailObject,
		func(env *envelope) (int, any) {
			return 200, okEnvelope(map[string]any{"data": jsonRaw(string(env.Data))})
		})
}

// deleteImage: DELETE /update/image/deleteImage?url= → DELETE /images/delete?url=
// 旧响应是 {"message","data":{"deleted":url}}——deleted 的值是**请求里的 url**，
// 新接口只回 {"deleted":true}，所以这里用查询参数补回旧形状。
func deleteImage(ctx context.Context, c *upstream.Client, r *http.Request) (int, any) {
	imageURL := r.URL.Query().Get("url")
	q := url.Values{"url": {imageURL}}
	return callUpstream(ctx, c, r, http.MethodDelete, "/images/delete", q, nil, detailObject,
		func(env *envelope) (int, any) {
			return 200, okEnvelope(map[string]any{"data": map[string]any{"deleted": imageURL}})
		})
}
