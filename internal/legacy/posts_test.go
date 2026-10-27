package legacy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/LYXOfficial/ariablognext-backend-glue/internal/upstream"
)

// ---------- postsInfo：分页换算（错一条就会白屏） ----------

func TestPostsInfo_PagingConversion(t *testing.T) {
	// 前端只用的两种写法都要换算对（glue.md §3.3）
	cases := []struct {
		name            string
		query           string
		wantPage        string
		wantPageSize    string
		wantExtraParams map[string]string
	}{
		{"首页 6 篇", "startl=0&endl=6", "1", "6", nil},
		{"第 3 页 16 篇", "startl=32&endl=48", "3", "16", nil},
		{"CardNewestPosts", "startl=0&endl=6", "1", "6", nil},
		{"endl 缺省走 100", "startl=0", "1", "100", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
				return okResp(`[{"slug":"a"}]`, `{"page":1,"pageSize":6,"count":83,"hasMore":true}`)
			})
			r := legacyReq("GET", "/get/post/postsInfo", tc.query, nil, nil)
			status, body := run(t, postsInfo, f.client(), r)
			if status != 200 || body["message"] != "success" {
				t.Fatalf("status=%d body=%v", status, body)
			}
			if got := f.last.Query.Get("page"); got != tc.wantPage {
				t.Errorf("page = %q, want %q", got, tc.wantPage)
			}
			if got := f.last.Query.Get("pageSize"); got != tc.wantPageSize {
				t.Errorf("pageSize = %q, want %q", got, tc.wantPageSize)
			}
			if got := f.last.Query.Get("withContent"); got != "" {
				t.Errorf("普通分页不该带 withContent，实际 %q", got)
			}
		})
	}
}

func TestPostsInfo_TypeFull(t *testing.T) {
	// type=full → pageSize=full&withContent=true，一次拿完不翻页（glue.md §5.5）
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`[{"slug":"a","mdContent":"# hi"}]`, `{"pageSize":"full","count":83}`)
	})
	r := legacyReq("GET", "/get/post/postsInfo", "type=full", nil, nil)
	status, body := run(t, postsInfo, f.client(), r)
	if status != 200 {
		t.Fatal(status)
	}
	if f.last.Query.Get("pageSize") != "full" || f.last.Query.Get("withContent") != "true" {
		t.Errorf("full 换算错误: %v", f.last.Query)
	}
	if f.last.Query.Get("page") != "" {
		t.Errorf("full 不该带 page: %v", f.last.Query)
	}
	if _, ok := body["data"]; !ok {
		t.Error("缺 data")
	}
}

func TestPostsInfo_DataPassedThrough(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`[{"slug":"a","plainContent":"截断的"}]`, `{"count":1}`)
	})
	_, body := run(t, postsInfo, f.client(), legacyReq("GET", "/get/post/postsInfo", "startl=0&endl=6", nil, nil))
	raw, _ := json.Marshal(body["data"])
	if string(raw) != `[{"slug":"a","plainContent":"截断的"}]` {
		t.Errorf("data 应原样透传: %s", raw)
	}
}

// ---------- 计数类 ----------

func TestCountFromList_CountOnly(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		// countOnly=true 时新后端 data 为 null，只有 meta.count
		return okResp(`null`, `{"count":83}`)
	})
	status, body := run(t, postCount, f.client(), legacyReq("GET", "/get/post/postCount", "", nil, nil))
	if status != 200 || body["count"] != int64(83) {
		t.Errorf("count = %v (status %d)", body["count"], status)
	}
	if f.last.Query.Get("countOnly") != "true" {
		t.Errorf("countOnly 没传: %v", f.last.Query)
	}
	if _, ok := body["data"]; ok {
		t.Error("旧 postCount 不该有 data 键")
	}
	if _, ok := body["meta"]; ok {
		t.Error("meta 是新信封字段，不能泄漏")
	}
}

func TestTotalWordCount(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"totalWordCount":152340}`, "")
	})
	_, body := run(t, totalWordCount, f.client(), legacyReq("GET", "/get/post/totalWordCount", "", nil, nil))
	if body["count"] != int64(152340) {
		t.Errorf("count = %v", body["count"])
	}
}

func TestLastUpdateTime(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"latestUpdateTime":1761400000}`, "")
	})
	_, body := run(t, lastUpdateTime, f.client(), legacyReq("GET", "/get/siteInfo/lastUpdateTime", "", nil, nil))
	if body["time"] != int64(1761400000) {
		t.Errorf("time = %v", body["time"])
	}
}

// ---------- navigation：方向是反的 ----------

func TestPostNavigation_ReverseMapping(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"newer":{"slug":"new-post"},"older":null}`, "")
	})
	_, body := run(t, postNavigation, f.client(), legacyReq("GET", "/get/post/postNavigation", "slug=x", nil, nil))
	// previous/next 是上游字段的原样透传（RawMessage），反序列化后比对
	prevRaw, _ := json.Marshal(body["previous"])
	var prev map[string]any
	_ = json.Unmarshal(prevRaw, &prev)
	if prev == nil || prev["slug"] != "new-post" {
		t.Errorf("previous 应等于 newer: %v", body["previous"])
	}
	next, ok := body["next"]
	if !ok || next != nil {
		t.Errorf("next 应为 null（来自 older）: %v", next)
	}
}

// ---------- 失败重排 ----------

func TestFailBody_ObjectDetail(t *testing.T) {
	// postBySlug 404：detail 是对象，error 透传上游中文
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return errResp(404, "post.not_found", "没有找到这篇文章", "null")
	})
	status, body := run(t, postBySlug, f.client(), legacyReq("GET", "/get/post/postBySlug", "slug=nope", nil, nil))
	if status != 404 {
		t.Errorf("状态码要透传，实际 %d", status)
	}
	detail, _ := body["detail"].(map[string]any)
	if detail == nil || detail["message"] != "fail" || detail["error"] != "没有找到这篇文章" {
		t.Errorf("detail 形状错误: %v", body["detail"])
	}
	if _, ok := body["error"]; ok {
		t.Error("error 是新信封字段，不能泄漏")
	}
}

func TestFailBody_StringDetail_Verify(t *testing.T) {
	// verify 401 → {"detail":"Invalid token"}（字符串，不是对象）
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return errResp(401, "auth.invalid_token", "登录已过期，请重新登录", "null")
	})
	status, body := run(t, verify, f.client(), legacyReq("GET", "/access/user/verify", "", nil,
		map[string]string{"Authorization": "Bearer bad"}))
	if status != 401 {
		t.Fatalf("status=%d", status)
	}
	if body["detail"] != "Invalid token" {
		t.Errorf(`detail 应为字符串 "Invalid token"，实际 %#v`, body["detail"])
	}
	if f.last.Headers.Get("Authorization") != "Bearer bad" {
		t.Error("Authorization 必须原样透传")
	}
}

func TestFailBody_StringDetail_Login(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return errResp(403, "auth.locked", "用户名或密码不正确", "null")
	})
	_, body := run(t, login, f.client(), legacyReq("POST", "/access/user/login", "",
		[]byte(`{"user":"a","password":"b"}`), nil))
	if body["detail"] != "Invalid credentials" {
		t.Errorf(`403 应回 "Invalid credentials"，实际 %#v`, body["detail"])
	}
}

func TestLogin_SuccessJWT(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"token":"jwt-token"}`, "")
	})
	_, body := run(t, login, f.client(), legacyReq("POST", "/access/user/login", "",
		[]byte(`{"user":"a","password":"b"}`), nil))
	if body["jwt"] != "jwt-token" {
		t.Errorf("jwt = %v", body["jwt"])
	}
	if _, ok := body["data"]; ok {
		t.Error("旧 login 响应没有 data 键")
	}
}

func TestFailBody_StringDetail_SlugConflict(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return errResp(409, "post.slug_conflict", "文章 slug 已存在", "null")
	})
	status, body := run(t, addPost, f.client(), legacyReq("POST", "/update/post/addPost", "",
		[]byte(`{"slug":"dup","title":"t"}`), nil))
	if status != 409 || body["detail"] != "slug already exists" {
		t.Errorf("409 应回字符串 detail，实际 %d %v", status, body["detail"])
	}
}

func TestFailBody_InternalErrorContainsCause(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return errResp(500, "internal.error", "服务内部错误，请稍后重试", `{"cause":"server selection timeout"}`)
	})
	status, body := run(t, postBySlug, f.client(), legacyReq("GET", "/get/post/postBySlug", "slug=x", nil, nil))
	if status != 500 {
		t.Fatalf("status=%d", status)
	}
	detail, _ := body["detail"].(map[string]any)
	if detail == nil {
		t.Fatal("500 应是对象 detail")
	}
	errText, _ := detail["error"].(string)
	if errText == "" || !strings.Contains(errText, "server selection timeout") {
		t.Errorf("500 的 detail.error 应包含 details.cause 内容: %q", errText)
	}
}

func TestFailBody_ValidationFieldsJoined(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return errResp(400, "validation.invalid_field", "字段格式不对",
			`{"fields":[{"field":"link","message":"必须以 http:// 或 https:// 开头"},{"field":"color","message":"颜色格式不正确"}]}`)
	})
	_, body := run(t, flinkApply, f.client(), legacyReq("POST", "/flink/apply", "", []byte(`{}`), nil))
	detail, _ := body["detail"].(map[string]any)
	errText, _ := detail["error"].(string)
	if !strings.Contains(errText, "；") || !strings.Contains(errText, "颜色格式不正确") {
		t.Errorf("多字段错误应用「；」连接: %q", errText)
	}
}

func TestUpstreamNonJSON_NoPanic(t *testing.T) {
	// 网关返回 HTML 时不能 panic，也不能把新信封字段漏出去
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return 200, "<html>bad gateway</html>", map[string]string{"Content-Type": "text/html"}
	})
	status, body := run(t, postsInfo, f.client(), legacyReq("GET", "/get/post/postsInfo", "startl=0&endl=6", nil, nil))
	if status != 502 {
		t.Errorf("非 JSON 上游应回 502，实际 %d", status)
	}
	if _, hasMsg := body["message"]; hasMsg {
		t.Errorf("失败时不该有 message=success: %v", body)
	}
}

func TestUpstreamNetworkDown(t *testing.T) {
	// 指向不存在端口的 client：502 + 旧格式 detail
	c := upstream.New("http://127.0.0.1:1", time.Second, nil)
	status, body := run(t, postsInfo, c, legacyReq("GET", "/get/post/postsInfo", "startl=0&endl=6", nil, nil))
	if status != 502 {
		t.Errorf("status=%d", status)
	}
	if detail, ok := body["detail"].(map[string]any); !ok || detail["message"] != "fail" {
		t.Errorf("detail 形状错误: %v", body["detail"])
	}
}

// ---------- 写接口「目标不存在」静默成功（GO-002） ----------

func TestUpdatePostInfo_MissingTargetSilentSuccess(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return errResp(404, "post.not_found", "没有找到这篇文章", "null")
	})
	r := legacyReq("PUT", "/update/post/updatePostInfo", "", []byte(`{"slug":"gone","title":"t"}`),
		map[string]string{"Authorization": "Bearer tok"})
	status, body := run(t, updatePostInfo, f.client(), r)
	if status != 200 || body["message"] != "success" {
		t.Fatalf("not_found 应静默成功: status=%d body=%v", status, body)
	}
	if _, ok := body["detail"]; ok {
		t.Error("静默成功不应带 detail")
	}
}

func TestUpdatePostInfo_OtherErrorsPassThrough(t *testing.T) {
	// 500 之类的错误绝不能被吞
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return errResp(500, "internal.error", "炸了", "null")
	})
	r := legacyReq("PUT", "/update/post/updatePostInfo", "", []byte(`{"slug":"x","title":"t"}`), nil)
	status, body := run(t, updatePostInfo, f.client(), r)
	if status != 500 {
		t.Fatalf("status=%d", status)
	}
	if _, ok := body["detail"]; !ok {
		t.Error("500 要带 detail")
	}
}

// 搜索结果裁剪：旧契约单条只有 slug/title/publishTime/context，
// 新接口的完整字段（plainContent/description 等）不能照单转发——
// 33 条命中会从 10KB 涨到 34KB（实测），中国访客的搜索响应拖大 3.4 倍。
func TestSearchPostsTrimsToLegacyShape(t *testing.T) {
	payload := `{"ok":true,"data":[{"slug":"a","title":"T","publishTime":123,"context":"ctx","plainContent":"很长的一段","description":"d"}]}`
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return 200, payload, map[string]string{"Content-Type": "application/json"}
	})
	status, body := run(t, searchPosts, f.client(), legacyReq("GET", "/get/post/searchPosts", "query=x", nil, nil))
	if status != 200 {
		t.Fatalf("status=%d", status)
	}
	raw, _ := json.Marshal(body["data"])
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil || len(items) != 1 {
		t.Fatalf("data 应为 1 条：%s", raw)
	}
	for _, k := range []string{"slug", "title", "publishTime", "context"} {
		if _, ok := items[0][k]; !ok {
			t.Errorf("旧契约字段 %s 缺失：%v", k, items[0])
		}
	}
	for _, k := range []string{"plainContent", "description"} {
		if _, ok := items[0][k]; ok {
			t.Errorf("新字段 %s 不应出现在旧契约搜索结果里：%v", k, items[0])
		}
	}
}
