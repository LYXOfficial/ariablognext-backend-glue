package legacy

import (
	"net/http"
	"strings"
	"testing"
)

// ---------- tagInfo / categoryInfo：一次请求同时拿 data + totalCount ----------

func TestTagInfo_DataAndTotalCount(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		// 必须转调 /posts/list（不是 /tags/list），meta.count 一次拿到
		return okResp(`[{"slug":"a"},{"slug":"b"}]`, `{"page":1,"count":18,"hasMore":false}`)
	})
	r := legacyReq("GET", "/get/tag/tagInfo", "tag=博客&startl=0&endl=20", nil, nil)
	status, body := run(t, tagInfo, f.client(), r)
	if status != 200 {
		t.Fatalf("status=%d", status)
	}
	if f.last.Path != "/posts/list" {
		t.Errorf("tagInfo 必须转调 /posts/list，实际 %s", f.last.Path)
	}
	if f.last.Query.Get("tag") != "博客" {
		t.Errorf("tag 参数没透传: %v", f.last.Query)
	}
	if f.last.Query.Get("page") != "1" || f.last.Query.Get("pageSize") != "20" {
		t.Errorf("分页换算错误: %v", f.last.Query)
	}
	if body["totalCount"] != int64(18) {
		t.Errorf("totalCount = %v", body["totalCount"])
	}
	if _, ok := body["data"]; !ok {
		t.Error("缺 data")
	}
}

func TestCategoryInfo(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`[]`, `{"count":0}`)
	})
	r := legacyReq("GET", "/get/category/categoryInfo", "category=随笔&startl=0&endl=16", nil, nil)
	_, body := run(t, categoryInfo, f.client(), r)
	// 注意：categoryInfo 旧行为是空数组 + 200（只有 archiveInfo 是 404）
	if body["totalCount"] != int64(0) {
		t.Errorf("totalCount = %v", body["totalCount"])
	}
}

func TestTagCount_CategoryCount(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		if c.Path == "/tags/list" || c.Path == "/categories/list" {
			if c.Query.Get("countOnly") != "true" {
				t.Errorf("应带 countOnly=true: %v", c.Query)
			}
			return okResp(`null`, `{"count":42}`)
		}
		t.Fatalf("unexpected path %s", c.Path)
		return 0, "", nil
	})
	_, body := run(t, tagCount, f.client(), legacyReq("GET", "/get/tag/tagCount", "", nil, nil))
	if body["count"] != int64(42) {
		t.Errorf("tagCount = %v", body["count"])
	}
	_, body = run(t, categoryCount, f.client(), legacyReq("GET", "/get/category/categoryCount", "", nil, nil))
	if body["count"] != int64(42) {
		t.Errorf("categoryCount = %v", body["count"])
	}
}

// ---------- archiveInfo：空结果 404 是刻意的（glue.md §5.6） ----------

func TestArchiveInfo_EmptyIs404(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`[]`, `{"count":0}`)
	})
	r := legacyReq("GET", "/get/archive/archiveInfo", "year=1999&month=1&startl=0&endl=16", nil, nil)
	status, body := run(t, archiveInfo, f.client(), r)
	if status != 404 {
		t.Fatalf("空结果必须回 404（前端据此显示「这个月没有文章」），实际 %d", status)
	}
	detail, _ := body["detail"].(map[string]any)
	if detail == nil || detail["error"] == "" {
		t.Errorf("404 要带对象 detail: %v", body["detail"])
	}
	// year/month 要转成 /posts/list 的过滤参数
	if f.last.Query.Get("year") != "1999" || f.last.Query.Get("month") != "1" {
		t.Errorf("year/month 没透传: %v", f.last.Query)
	}
}

func TestArchiveInfo_NonEmpty(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`[{"slug":"a"}]`, `{"count":7}`)
	})
	_, body := run(t, archiveInfo, f.client(), legacyReq("GET", "/get/archive/archiveInfo", "year=2026&month=10", nil, nil))
	if body["totalCount"] != int64(7) {
		t.Errorf("totalCount = %v", body["totalCount"])
	}
}

// ---------- search 系列 ----------

func TestSearchPosts_QueryToKeyword(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`[{"slug":"a","context":"...命中..."}]`, `{"count":1}`)
	})
	_, body := run(t, searchPosts, f.client(), legacyReq("GET", "/get/post/searchPosts", "query=肥鱼", nil, nil))
	if f.last.Query.Get("keyword") != "肥鱼" || f.last.Query.Get("pageSize") != "100" {
		t.Errorf("query→keyword 换算错误: %v", f.last.Query)
	}
	if _, ok := body["data"]; !ok {
		t.Error("缺 data")
	}
}

func TestSearchPostsByTitle_PagingAndTitleOnly(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`[{"slug":"a"}]`, `{"count":3}`)
	})
	r := legacyReq("GET", "/get/post/searchPostsByTitle", "startl=40&endl=60&title=go", nil, nil)
	_, _ = run(t, searchPostsByTitle, f.client(), r)
	if f.last.Query.Get("keyword") != "go" || f.last.Query.Get("titleOnly") != "true" {
		t.Errorf("title→keyword/titleOnly 错误: %v", f.last.Query)
	}
	if f.last.Query.Get("page") != "3" || f.last.Query.Get("pageSize") != "20" {
		t.Errorf("dashboard 的 startl=40&endl=60 应换算 page=3&pageSize=20: %v", f.last.Query)
	}
}

func TestSearchPostsByTitleCount(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`null`, `{"count":3}`)
	})
	_, body := run(t, searchPostsByTitleCount, f.client(), legacyReq("GET", "/get/post/searchPostsByTitleCount", "title=go", nil, nil))
	if body["count"] != int64(3) {
		t.Errorf("count = %v", body["count"])
	}
	if f.last.Query.Get("titleOnly") != "true" || f.last.Query.Get("countOnly") != "true" {
		t.Errorf("参数缺失: %v", f.last.Query)
	}
}

// ---------- summary 两种语义 ----------

func TestPostSummary_PublicAndAdminRead(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"summary":"一篇摘要"}`, "")
	})
	// 公开 GET：data 是字符串本身
	_, body := run(t, postSummary, f.client(), legacyReq("GET", "/get/post/postSummary", "slug=a", nil, nil))
	if body["data"] != "一篇摘要" {
		t.Errorf("公开 summary 应取 data.summary: %v", body["data"])
	}
	// 后台只读 GET：同一个上游接口
	_, body = run(t, updatePostSummaryRead, f.client(), legacyReq("GET", "/update/post/summary", "slug=a", nil,
		map[string]string{"Authorization": "Bearer x"}))
	if body["data"] != "一篇摘要" {
		t.Errorf("后台 summary = %v", body["data"])
	}
	if f.last.Headers.Get("Authorization") != "Bearer x" {
		t.Error("后台 summary 的 Authorization 要透传")
	}
	// 后台只读必须带 generate=false（GO-007：绝不触发生成）；公开不带
	if f.last.Query.Get("generate") != "false" {
		t.Errorf("后台 summary 应带 generate=false: %v", f.last.Query)
	}
}

func TestPostSummary_PublicNoGenerateParam(t *testing.T) {
	// 公开接口保持「miss 生成一次」语义，不能带 generate=false（GO-007）
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"summary":null}`, "")
	})
	run(t, postSummary, f.client(), legacyReq("GET", "/get/post/postSummary", "slug=a", nil, nil))
	if _, ok := f.last.Query["generate"]; ok {
		t.Errorf("公开 summary 不应带 generate 参数: %v", f.last.Query)
	}
}

func TestPostSummary_NullSummary(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"summary":null}`, "")
	})
	_, body := run(t, postSummary, f.client(), legacyReq("GET", "/get/post/postSummary", "slug=a", nil, nil))
	if body["data"] != nil {
		t.Errorf("summary 缺失时 data 应为 null: %v", body["data"])
	}
}

// ---------- 写接口：body→query / 字段改名 ----------

func TestDeletePost_BodySlugToQuery(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"deleted":true}`, "")
	})
	r := legacyReq("DELETE", "/update/post/deletePost", "", []byte(`{"slug":"a"}`), nil)
	status, body := run(t, deletePost, f.client(), r)
	if status != 200 || body["message"] != "success" {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if f.last.Method != "DELETE" || f.last.Path != "/posts/delete" {
		t.Errorf("上游收到 %s %s", f.last.Method, f.last.Path)
	}
	if f.last.Query.Get("slug") != "a" {
		t.Errorf("slug 应搬到 query: %v", f.last.Query)
	}
}

func TestDeleteSpeaks_BodyTimeToQuery(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"deleted":true}`, "")
	})
	r := legacyReq("DELETE", "/update/speaks/deleteSpeaks", "", []byte(`{"time":1700000000}`), nil)
	_, _ = run(t, deleteSpeaks, f.client(), r)
	if f.last.Query.Get("time") != "1700000000" {
		t.Errorf("time 应搬到 query: %v", f.last.Query)
	}
}

func TestDeleteRenderedHtmlCache_DirectSuccess(t *testing.T) {
	// 功能已删：不转发（假上游不会收到任何请求），直接回 success
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		t.Error("deleteRenderedHtmlCache 不应转发到上游")
		return 500, `{}`, nil
	})
	status, body := run(t, deleteRenderedHtmlCache, f.client(), legacyReq("DELETE", "/update/post/deleteRenderedHtmlCache", "slug=a", nil, nil))
	if status != 200 || body["message"] != "success" {
		t.Errorf("应直接回 success: %d %v", status, body)
	}
	if f.last != nil {
		t.Error("上游收到了不该转发的请求")
	}
}

func TestRefreshSummary(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		if c.Method != "POST" || c.Path != "/posts/summary" {
			t.Errorf("上游收到 %s %s", c.Method, c.Path)
		}
		return okResp(`{"summary":"新摘要"}`, "")
	})
	r := legacyReq("POST", "/update/post/refreshSummary", "", []byte(`{"slug":"a"}`), nil)
	status, body := run(t, refreshSummary, f.client(), r)
	if status != 200 || body["data"] != "新摘要" {
		t.Errorf("status=%d data=%v", status, body["data"])
	}
	if !strings.Contains(string(f.last.Body), `"slug":"a"`) {
		t.Errorf("body 应原样转发: %s", f.last.Body)
	}
}

// ---------- 写接口「目标不存在」静默成功（GO-002） ----------

func TestUpdateDraftInfo_MissingTargetSilentSuccess(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return errResp(404, "draft.not_found", "没有找到这篇草稿", "null")
	})
	r := legacyReq("PUT", "/update/draft/updateDraftInfo", "", []byte(`{"slug":"gone","title":"t"}`), nil)
	status, body := run(t, updateDraftInfo, f.client(), r)
	if status != 200 || body["message"] != "success" {
		t.Fatalf("not_found 应静默成功: status=%d body=%v", status, body)
	}
}

func TestUpdateSpeaks_MissingSilent_OtherValidationPasses(t *testing.T) {
	// 判据三条(GO-004):① 结构 details.fields[].field=="time"(文案可变)→静默;
	// ② message 含「说说不存在」兜底 → 静默;③ 其它字段的校验错误 → 透传
	i := 0
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		i++
		switch i {
		case 1: // 结构判据:message 已改但 details.fields 锚定 time
			return 400, `{"ok":false,"requestId":"t","error":{"code":"validation.invalid_field","message":"该时间戳没有对应的说说","details":{"fields":[{"field":"time","message":"不存在"}]}}}`, map[string]string{"Content-Type": "application/json"}
		case 2: // 文案兜底:details 缺失但 message 命中
			return errResp(400, "validation.invalid_field", "该时间戳的说说不存在", "null")
		default: // 其它字段错误透传
			return errResp(400, "validation.invalid_field", "content 不能为空", "null")
		}
	})
	req := func() *http.Request {
		return legacyReq("PUT", "/update/speaks/updateSpeaks", "", []byte(`{"time":1,"content":"x"}`), nil)
	}
	status, body := run(t, updateSpeaks, f.client(), req())
	if status != 200 || body["message"] != "success" {
		t.Fatalf("结构判据应静默成功: status=%d body=%v", status, body)
	}
	status, body = run(t, updateSpeaks, f.client(), req())
	if status != 200 || body["message"] != "success" {
		t.Fatalf("文案兜底应静默成功: status=%d body=%v", status, body)
	}
	status, body = run(t, updateSpeaks, f.client(), req())
	if status != 400 {
		t.Fatalf("其它校验错误应透传: status=%d", status)
	}
	if _, ok := body["detail"]; !ok {
		t.Error("透传错误要带 detail")
	}
}
