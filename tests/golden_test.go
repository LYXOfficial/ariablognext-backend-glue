// 端到端快照（golden）测试：假上游 → glue 的 http.Handler → 旧格式逐字节比对。
//
// 与 internal/legacy 的单测互补：单测逐条断言字段，golden 从 HTTP 层进
// （mux 注册、方法、状态码、JSON 编码全走真链路），防止以后改路由表时
// 静默改坏对外契约。
package tests

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/LYXOfficial/ariablognext-backend-glue/internal/legacy"
	"github.com/LYXOfficial/ariablognext-backend-glue/internal/upstream"
)

var update = flag.Bool("update", false, "重新生成 testdata/legacy/*.json")

// goldenCase 描述一条快照：旧请求 + 上游回放的新格式响应 → 期望的旧格式输出。
type goldenCase struct {
	name   string
	method string
	path   string
	query  string
	body   string
	hdr    map[string]string
	// upstream 是假上游按任意路径返回的新格式响应体（含信封）
	upstream string
	upStatus int
	// wantStatus 缺省 200
	wantStatus int
}

var cases = []goldenCase{
	{name: "posts_info_page", method: "GET", path: "/get/post/postsInfo", query: "startl=32&endl=48",
		upstream: `{"ok":true,"data":[{"slug":"a"}],"meta":{"page":3,"pageSize":16,"count":83,"hasMore":true}}`},
	{name: "posts_info_full", method: "GET", path: "/get/post/postsInfo", query: "type=full",
		upstream: `{"ok":true,"data":[{"slug":"a","mdContent":"# x"}],"meta":{"pageSize":"full","count":83,"hasMore":false}}`},
	{name: "post_count", method: "GET", path: "/get/post/postCount",
		upstream: `{"ok":true,"data":null,"meta":{"count":83}}`},
	{name: "total_word_count", method: "GET", path: "/get/post/totalWordCount",
		upstream: `{"ok":true,"data":{"totalWordCount":152340}}`},
	{name: "post_slugs", method: "GET", path: "/get/post/postSlugs",
		upstream: `{"ok":true,"data":["a","b"]}`},
	{name: "post_navigation", method: "GET", path: "/get/post/postNavigation", query: "slug=x",
		upstream: `{"ok":true,"data":{"newer":{"slug":"n","title":"新"},"older":{"slug":"o","title":"旧"}}}`},
	{name: "post_summary", method: "GET", path: "/get/post/postSummary", query: "slug=a",
		upstream: `{"ok":true,"data":{"summary":"摘要文本"}}`},
	{name: "search_posts", method: "GET", path: "/get/post/searchPosts", query: "query=肥鱼",
		upstream: `{"ok":true,"data":[{"slug":"a","context":"...命中..."}],"meta":{"count":1}}`},
	{name: "tag_info", method: "GET", path: "/get/tag/tagInfo", query: "tag=博客&startl=0&endl=20",
		upstream: `{"ok":true,"data":[{"slug":"a"}],"meta":{"count":18}}`},
	{name: "tag_count", method: "GET", path: "/get/tag/tagCount",
		upstream: `{"ok":true,"data":null,"meta":{"count":42}}`},
	{name: "archives", method: "GET", path: "/get/archive/archives",
		upstream: `{"ok":true,"data":[{"year":2026,"month":10,"count":7}]}`},
	{name: "archive_info_empty_404", method: "GET", path: "/get/archive/archiveInfo", query: "year=1999&month=1", wantStatus: 404,
		upstream: `{"ok":true,"data":[],"meta":{"count":0}}`},
	{name: "speaks", method: "GET", path: "/get/speaks/speaks", query: "startl=0&endl=3",
		upstream: `{"ok":true,"data":[{"time":1,"content":"hi"}],"meta":{"count":5}}`},
	{name: "flinks", method: "GET", path: "/get/flink/flinks",
		upstream: `{"ok":true,"data":[{"name":"友达","links":[]}],"meta":{"count":27}}`},
	{name: "flink_count", method: "GET", path: "/get/flink/flinkCount",
		upstream: `{"ok":true,"data":null,"meta":{"count":27}}`},
	{name: "last_update_time", method: "GET", path: "/get/siteInfo/lastUpdateTime",
		upstream: `{"ok":true,"data":{"latestUpdateTime":1761400000}}`},
	{name: "apply_status", method: "GET", path: "/flink/apply/status", query: "email=a@b.c",
		upstream: `{"ok":true,"data":[{"id":"1","status":"pending"}]}`},
	{name: "post_by_slug_404", method: "GET", path: "/get/post/postBySlug", query: "slug=nope", wantStatus: 404, upStatus: 404,
		upstream: `{"ok":false,"error":{"code":"post.not_found","message":"没有找到这篇文章","details":null}}`},
	{name: "verify_401_string_detail", method: "GET", path: "/access/user/verify", wantStatus: 401, upStatus: 401,
		hdr:      map[string]string{"Authorization": "Bearer bad"},
		upstream: `{"ok":false,"error":{"code":"auth.invalid_token","message":"登录已过期","details":null}}`},
	{name: "login_success", method: "POST", path: "/access/user/login", body: `{"user":"a","password":"b"}`,
		upstream: `{"ok":true,"data":{"token":"jwt-abc"}}`},
	{name: "add_post_slug_conflict", method: "POST", path: "/update/post/addPost", body: `{"slug":"dup"}`, wantStatus: 409, upStatus: 409,
		hdr:      map[string]string{"Authorization": "Bearer tok"},
		upstream: `{"ok":false,"error":{"code":"post.slug_conflict","message":"文章 slug 已存在","details":null}}`},
	{name: "upload_image_nested", method: "POST", path: "/update/image/uploadImage", body: "RAW", wantStatus: 200,
		hdr:      map[string]string{"Authorization": "Bearer tok", "Content-Type": "multipart/form-data; boundary=X"},
		upstream: `{"ok":true,"data":{"url":"https://img.0v0.my/2026/10/27/abc.png"}}`},
	{name: "delete_image_deleted_is_url", method: "DELETE", path: "/update/image/deleteImage", query: "url=https://img.0v0.my/a.png",
		hdr:      map[string]string{"Authorization": "Bearer tok"},
		upstream: `{"ok":true,"data":{"deleted":true}}`},
	{name: "flink_apply_lift_booleans", method: "POST", path: "/flink/apply",
		body:     `{"name":"站","link":"https://x.example","avatar":"https://a/p.png","descr":"d","email":"a@b.c","color":"#f08","type":0,"targetUrl":""}`,
		upstream: `{"ok":true,"data":{"screeningTriggered":true,"mailSent":false}}`},
	{name: "approve_application", method: "POST", path: "/update/flink/approveApplication", body: `{"id":"66f0","group":"友达"}`,
		hdr:      map[string]string{"Authorization": "Bearer tok"},
		upstream: `{"ok":true,"data":{"mailSent":true}}`},
	{name: "push_flink_status", method: "POST", path: "/update/flink/pushFlinkStatus",
		body:     `{"data":{"linkStatus":[{"id":"a1","latency":32.5}]},"secret":"SEC"}`,
		upstream: `{"ok":true,"data":{"updated":1}}`},
	{name: "export_collections", method: "POST", path: "/get/export/collections", body: `{"secret":"SEC"}`,
		upstream: `{"ok":true,"data":{"collections":{"Posts":[{"_id":"x"}]}}}`},
	{name: "move_flink_group", method: "POST", path: "/update/flink/moveFlinkGroup", body: `{"from_group":"A","to_group":"B","link_id":"x1"}`,
		hdr:      map[string]string{"Authorization": "Bearer tok"},
		upstream: `{"ok":true,"data":{"moved":true}}`},
}

func TestGolden(t *testing.T) {
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.upStatusOr200())
				_, _ = w.Write([]byte(tc.upstream))
			}))
			defer fake.Close()

			client := upstream.New(fake.URL, 5_000_000_000, []string{"Authorization", "Content-Type"})
			srv := httptest.NewServer(legacy.NewHTTPHandler(client, legacy.Options{}))
			defer srv.Close()

			var bodyReader *bytes.Reader
			if tc.body == "RAW" {
				// 上传用例不关心 multipart 细节（单测已覆盖），只要是个非空体
				bodyReader = bytes.NewReader([]byte("--X\r\n--X--\r\n"))
			} else {
				bodyReader = bytes.NewReader([]byte(tc.body))
			}
			req, err := http.NewRequest(tc.method, srv.URL+tc.path+"?"+tc.query, bodyReader)
			if err != nil {
				t.Fatal(err)
			}
			for k, v := range tc.hdr {
				req.Header.Set(k, v)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatusOr200() {
				t.Errorf("状态码 = %d，想要 %d", resp.StatusCode, tc.wantStatusOr200())
			}
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(resp.Body)
			got := canonical(t, buf.Bytes())

			legacyPath := filepath.Join("testdata", "legacy", tc.name+".json")
			if *update {
				if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(legacyPath, got, 0o600); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(legacyPath)
			if err != nil {
				t.Fatalf("缺 golden 文件（先跑 go test ./tests -update 生成）: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("输出与 golden 不一致\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

func (c goldenCase) upStatusOr200() int {
	if c.upStatus != 0 {
		return c.upStatus
	}
	return 200
}

func (c goldenCase) wantStatusOr200() int {
	if c.wantStatus != 0 {
		return c.wantStatus
	}
	return 200
}

// canonical 把 JSON 规范化（排序键、紧凑），让比对只关心结构不关心空格。
func canonical(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("响应不是 JSON: %q", raw)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	pretty := new(bytes.Buffer)
	_ = json.Indent(pretty, out, "", " ")
	return pretty.Bytes()
}

// TestSitemapRawPassThrough 是 golden 里的二进制特例：XML 不做 JSON 规范化。
func TestSitemapRawPassThrough(t *testing.T) {
	xml := `<?xml version="1.0" encoding="utf-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"></urlset>`
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sitemap" {
			t.Errorf("上游收到 %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(xml))
	}))
	defer fake.Close()

	client := upstream.New(fake.URL, 5_000_000_000, nil)
	srv := httptest.NewServer(legacy.NewHTTPHandler(client, legacy.Options{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/get/sitemap/sitemapGen")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	if buf.String() != xml {
		t.Errorf("XML 应逐字节一致:\n%s", buf.String())
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/xml" {
		t.Errorf("Content-Type = %q", ct)
	}
}
