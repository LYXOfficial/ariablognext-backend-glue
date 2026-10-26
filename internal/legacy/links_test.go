package legacy

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---------- themeColor：Cache-Control 透传 + 对象 detail ----------

func TestThemeColor_Success(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		if c.Query.Get("url") != "https://a.example/av.png" {
			t.Errorf("url 没透传: %v", c.Query)
		}
		return 200, `{"ok":true,"data":{"color":"#c95d7f"}}`,
			map[string]string{"Content-Type": "application/json", "Cache-Control": "public, max-age=86400"}
	})
	r := legacyReq("GET", "/get/flink/themeColor", "url=https://a.example/av.png", nil, nil)
	ctx, respHdr := withRespHeader(r.Context())
	status, raw := themeColor(ctx, f.client(), r)
	body := raw.(map[string]any)
	if status != 200 || body["data"] != "#c95d7f" {
		t.Fatalf("status=%d body=%v", status, body)
	}
	// 旧后端靠 Cache-Control 让 CDN 把同一头像 URL 挡在函数外，必须透传
	if respHdr.Get("Cache-Control") != "public, max-age=86400" {
		t.Errorf("Cache-Control 没透传: %v", respHdr)
	}
}

func TestThemeColor_ErrorIsObjectDetail(t *testing.T) {
	// glue.md §5.2：前端 themeColor.ts 只认 detail.error（对象形态）
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return errResp(502, "image.unreachable", "头像图片下载失败（对方返回 HTTP 404，可检查链接）", "null")
	})
	r := legacyReq("GET", "/get/flink/themeColor", "url=https://a.example/x.png", nil, nil)
	ctx, _ := withRespHeader(r.Context())
	status, raw := themeColor(ctx, f.client(), r)
	if status != 502 {
		t.Fatalf("status=%d", status)
	}
	body := raw.(map[string]any)
	detail, ok := body["detail"].(map[string]any)
	if !ok || detail["error"] == "" {
		t.Errorf("themeColor 错误必须是对象 detail: %#v", body["detail"])
	}
}

// ---------- flink 写接口 ----------

func TestMoveFlinkGroup_RenameFields(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"moved":true}`, "")
	})
	r := legacyReq("POST", "/update/flink/moveFlinkGroup", "",
		[]byte(`{"from_group":"A","to_group":"B","link_id":"x1"}`), nil)
	_, _ = run(t, moveFlinkGroup, f.client(), r)
	var sent map[string]any
	if err := json.Unmarshal(f.last.Body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent["fromGroup"] != "A" || sent["toGroup"] != "B" || sent["id"] != "x1" {
		t.Errorf("snake_case 必须改名成 camelCase: %v", sent)
	}
	if _, exists := sent["from_group"]; exists {
		t.Errorf("旧字段名不能带过去: %v", sent)
	}
}

func TestUpdateGroup_RenameOldName(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"updated":true}`, "")
	})
	r := legacyReq("PUT", "/update/flink/updateGroup", "",
		[]byte(`{"old_name":"A","name":"B","order":1,"description":"d"}`), nil)
	_, _ = run(t, updateGroup, f.client(), r)
	var sent map[string]any
	_ = json.Unmarshal(f.last.Body, &sent)
	if sent["oldName"] != "A" {
		t.Errorf("old_name 应改名 oldName: %v", sent)
	}
}

func TestDispatchCheckLatencyWorkflow_GETtoPOST(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"dispatched":true}`, "")
	})
	r := legacyReq("GET", "/update/flink/dispatchCheckLatencyWorkflow", "", nil,
		map[string]string{"Authorization": "Bearer tok"})
	status, body := run(t, dispatchCheckLatencyWorkflow, f.client(), r)
	if status != 200 || body["message"] != "success" {
		t.Fatalf("status=%d body=%v", status, body)
	}
	if f.last.Method != "POST" || f.last.Path != "/links/check" {
		t.Errorf("旧 GET 要转成 POST /links/check，实际 %s %s", f.last.Method, f.last.Path)
	}
	if f.last.Headers.Get("Authorization") != "Bearer tok" {
		t.Error("Authorization 要透传（让新后端返 401）")
	}
}

// ---------- 申请：合并 decide / 顶层布尔 ----------

func TestFlinkApply_LiftBooleans(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"screeningTriggered":true,"mailSent":false}`, "")
	})
	bodyJSON := `{"name":"站","link":"https://x.example","avatar":"https://a/p.png","descr":"d","email":"a@b.c","color":"#f08","type":0,"targetUrl":""}`
	r := legacyReq("POST", "/flink/apply", "", []byte(bodyJSON), nil)
	status, body := run(t, flinkApply, f.client(), r)
	if status != 200 {
		t.Fatalf("status=%d", status)
	}
	if body["screeningTriggered"] != true || body["mailSent"] != false {
		t.Errorf("两个布尔要提到顶层: %v", body)
	}
	if _, ok := body["data"]; ok {
		t.Error("旧 flink/apply 响应没有 data 键")
	}
	// body 字段名一致，原样转发
	if !strings.Contains(string(f.last.Body), `"targetUrl":""`) {
		t.Errorf("body 应原样转发: %s", f.last.Body)
	}
}

func TestApproveApplication_DecideShape(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"mailSent":true}`, "")
	})
	r := legacyReq("POST", "/update/flink/approveApplication", "",
		[]byte(`{"id":"66f0","group":"友达","name":"改过的名字"}`),
		map[string]string{"Authorization": "Bearer tok"})
	status, body := run(t, approveApplication, f.client(), r)
	if status != 200 || body["mailSent"] != true {
		t.Fatalf("status=%d body=%v", status, body)
	}
	var sent map[string]any
	_ = json.Unmarshal(f.last.Body, &sent)
	if sent["id"] != "66f0" || sent["decision"] != "approved" {
		t.Errorf("decision 必须补 approved: %v", sent)
	}
	ov, _ := sent["overrides"].(map[string]any)
	if ov == nil || ov["name"] != "改过的名字" {
		t.Errorf("有值的字段要进 overrides: %v", sent)
	}
	if _, has := ov["link"]; has {
		t.Error("没提供的字段不该进 overrides（旧后端语义是沿用申请原值）")
	}
	if sent["group"] != "友达" {
		t.Errorf("group = %v", sent["group"])
	}
}

func TestRejectApplication_DecideShape(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"mailSent":false}`, "")
	})
	r := legacyReq("POST", "/update/flink/rejectApplication", "", []byte(`{"id":"66f0","reason":"内容不符"}`), nil)
	_, body := run(t, rejectApplication, f.client(), r)
	if body["mailSent"] != false {
		t.Errorf("mailSent = %v", body["mailSent"])
	}
	var sent map[string]any
	_ = json.Unmarshal(f.last.Body, &sent)
	if sent["decision"] != "rejected" || sent["reason"] != "内容不符" {
		t.Errorf("reject 的 decide body 错误: %v", sent)
	}
}

// ---------- 服务间接口：secret 挪头 ----------

func TestPushFlinkStatus_SecretToHeader(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"updated":12}`, "")
	})
	body := `{"data":{"linkStatus":[{"id":"a1","latency":32.5},{"id":"b2","latency":0}]},"secret":"TOPSECRET"}`
	r := legacyReq("POST", "/update/flink/pushFlinkStatus", "", []byte(body), nil)
	status, respBody := run(t, pushFlinkStatus, f.client(), r)
	if status != 200 || respBody["message"] != "success" {
		t.Fatalf("status=%d body=%v", status, respBody)
	}
	if f.last.Headers.Get("X-Internal-Token") != "TOPSECRET" {
		t.Errorf("secret 应挪到 X-Internal-Token 头: %v", f.last.Headers)
	}
	var sent struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(f.last.Body, &sent)
	if len(sent.Items) != 2 || sent.Items[0]["id"] != "a1" {
		t.Errorf("linkStatus 应转成 items: %+v", sent)
	}
	// secret 不能再留在 body 里
	if strings.Contains(string(f.last.Body), "TOPSECRET") {
		t.Errorf("secret 不能随 body 转发: %s", f.last.Body)
	}
}

func TestFlinkScreening_HeaderRename(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"updated":true}`, "")
	})
	body := `{"id":"66f0","verdict":"pass","backlink":"found","summary":"s","reason":"","model":"m"}`
	r := legacyReq("POST", "/flink/screening", "", []byte(body),
		map[string]string{"X-Screening-Secret": "SEC"})
	status, respBody := run(t, flinkScreening, f.client(), r)
	if status != 200 || respBody["message"] != "success" {
		t.Fatalf("status=%d body=%v", status, respBody)
	}
	if f.last.Headers.Get("X-Internal-Token") != "SEC" {
		t.Errorf("x-screening-secret 应改名为 X-Internal-Token: %v", f.last.Headers)
	}
	if !strings.Contains(string(f.last.Body), `"verdict":"pass"`) {
		t.Errorf("body 应原样转发: %s", f.last.Body)
	}
}

func TestFlinkScreening_SecretInBodyFallback(t *testing.T) {
	// workflow 改造期的兜底：secret 放 body 里也能取到
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`{"updated":true}`, "")
	})
	body := `{"id":"66f0","verdict":"review","backlink":"","summary":"","reason":"","model":"","secret":"SEC"}`
	r := legacyReq("POST", "/flink/screening", "", []byte(body), nil)
	_, _ = run(t, flinkScreening, f.client(), r)
	if f.last.Headers.Get("X-Internal-Token") != "SEC" {
		t.Errorf("body secret 应挪到头: %v", f.last.Headers)
	}
}

func TestFlinkScreening_Upstream401ObjectDetail(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return errResp(401, "auth.invalid_token", "回写密钥不正确", "null")
	})
	r := legacyReq("POST", "/flink/screening", "", []byte(`{"id":"x","verdict":"pass"}`),
		map[string]string{"X-Screening-Secret": "wrong"})
	_, body := run(t, flinkScreening, f.client(), r)
	detail, ok := body["detail"].(map[string]any)
	if !ok || detail["error"] != "回写密钥不正确" {
		t.Errorf("screening 401 应是对象 detail: %#v", body["detail"])
	}
}

// ---------- 申请状态 / 列表 ----------

func TestApplyStatus_PassThrough(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`[{"id":"1","status":"pending"}]`, "")
	})
	r := legacyReq("GET", "/flink/apply/status", "email=a@b.c&name=站", nil, nil)
	_, body := run(t, applyStatus, f.client(), r)
	if f.last.Query.Get("email") != "a@b.c" || f.last.Query.Get("name") != "站" {
		t.Errorf("参数没透传: %v", f.last.Query)
	}
	if _, ok := body["data"]; !ok {
		t.Error("缺 data")
	}
}

func TestListApplications(t *testing.T) {
	f := newFakeUpstream(t, func(c *capture) (int, string, map[string]string) {
		return okResp(`[{"id":"1"}]`, `{"count":1}`)
	})
	r := legacyReq("GET", "/update/flink/applications", "", nil,
		map[string]string{"Authorization": "Bearer tok"})
	_, body := run(t, listApplications, f.client(), r)
	if _, ok := body["data"]; !ok {
		t.Error("缺 data")
	}
	if _, ok := body["totalCount"]; ok {
		t.Error("旧 applications 接口没有 totalCount")
	}
}
