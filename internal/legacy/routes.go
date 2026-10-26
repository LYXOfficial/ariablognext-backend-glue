package legacy

// 路由表：72 条旧路径，一条不落（glue.md §4.1–§4.4）。
// 逐行对应映射表；Method/Path 是旧后端的对外契约，改了前端就 404。

// All 返回全部路由。mux 注册时按数组顺序，不排序、不分组——
// 就是要和 docs/glue.md §4 的表格一行一行对得上，方便 review 与 diff。
func All() []Route {
	return all
}

var all = []Route{
	// ===== §4.1 公开读（blog 用，27 条） =====
	{Method: "GET", Path: "/get/post/postsInfo", Handle: postsInfo},
	{Method: "GET", Path: "/get/post/postBySlug", Handle: postBySlug},
	{Method: "GET", Path: "/get/post/postCount", Handle: postCount},
	{Method: "GET", Path: "/get/post/totalWordCount", Handle: totalWordCount},
	{Method: "GET", Path: "/get/post/postSlugs", Handle: postSlugs},
	{Method: "GET", Path: "/get/post/postNavigation", Handle: postNavigation},
	{Method: "GET", Path: "/get/post/relatedPosts", Handle: relatedPosts},
	{Method: "GET", Path: "/get/post/postSummary", Handle: postSummary},
	{Method: "GET", Path: "/get/post/searchPosts", Handle: searchPosts},
	{Method: "GET", Path: "/get/post/searchPostsByTitle", Handle: searchPostsByTitle},
	{Method: "GET", Path: "/get/post/searchPostsByTitleCount", Handle: searchPostsByTitleCount},
	{Method: "GET", Path: "/get/tag/tags", Handle: tags},
	{Method: "GET", Path: "/get/tag/tagCount", Handle: tagCount},
	{Method: "GET", Path: "/get/tag/tagInfo", Handle: tagInfo},
	{Method: "GET", Path: "/get/category/categories", Handle: categories},
	{Method: "GET", Path: "/get/category/categoryCount", Handle: categoryCount},
	{Method: "GET", Path: "/get/category/categoryInfo", Handle: categoryInfo},
	{Method: "GET", Path: "/get/archive/archives", Handle: archives},
	{Method: "GET", Path: "/get/archive/archiveInfo", Handle: archiveInfo},
	{Method: "GET", Path: "/get/speaks/speaks", Handle: speaks},
	{Method: "GET", Path: "/get/speaks/speaksCount", Handle: speaksCount},
	{Method: "GET", Path: "/get/flink/flinks", Handle: flinks},
	{Method: "GET", Path: "/get/flink/flinkCount", Handle: flinkCount},
	{Method: "GET", Path: "/get/flink/themeColor", Handle: themeColor},
	{Method: "GET", Path: "/get/siteInfo/lastUpdateTime", Handle: lastUpdateTime},
	{Method: "GET", Path: "/get/sitemap/sitemapGen", Handle: sitemapGen},
	{Method: "GET", Path: "/flink/apply/status", Handle: applyStatus},

	// ===== §4.2 后台读（dashboard 用，11 条） =====
	{Method: "GET", Path: "/get/draft/draftsInfo", Auth: true, Handle: draftsInfo},
	{Method: "GET", Path: "/get/draft/draftBySlug", Auth: true, Handle: draftBySlug},
	{Method: "GET", Path: "/get/draft/draftSlugs", Auth: true, Handle: draftSlugs},
	{Method: "GET", Path: "/get/draft/draftCount", Auth: true, Handle: draftCount},
	{Method: "GET", Path: "/get/draft/searchDraftsByTitle", Auth: true, Handle: searchDraftsByTitle},
	{Method: "GET", Path: "/get/draft/searchDraftsByTitleCount", Auth: true, Handle: searchDraftsByTitleCount},
	{Method: "GET", Path: "/update/post/summary", Auth: true, Handle: updatePostSummaryRead},
	{Method: "GET", Path: "/update/flink/applications", Auth: true, Handle: listApplications},
	{Method: "GET", Path: "/update/image/listImages", Auth: true, Handle: listImages},
	// 旧的是 GET（触发 GitHub Actions），新后端是 POST /links/check，方法在胶水里换
	{Method: "GET", Path: "/update/flink/dispatchCheckLatencyWorkflow", Auth: true, Handle: dispatchCheckLatencyWorkflow},
	{Method: "GET", Path: "/access/user/verify", Auth: true, Handle: verify},

	// ===== §4.3 写（dashboard + 友链申请，31 条） =====
	{Method: "POST", Path: "/access/user/login", Handle: login},
	{Method: "POST", Path: "/update/post/addPost", Auth: true, Handle: addPost},
	{Method: "PUT", Path: "/update/post/updatePostInfo", Auth: true, Handle: updatePostInfo},
	{Method: "PUT", Path: "/update/post/updatePostMarkdown", Auth: true, Handle: updatePostMarkdown},
	{Method: "DELETE", Path: "/update/post/deletePost", Auth: true, Handle: deletePost},
	{Method: "POST", Path: "/update/draft/addDraft", Auth: true, Handle: addDraft},
	{Method: "PUT", Path: "/update/draft/updateDraftInfo", Auth: true, Handle: updateDraftInfo},
	{Method: "PUT", Path: "/update/draft/updateDraftMarkdown", Auth: true, Handle: updateDraftMarkdown},
	{Method: "DELETE", Path: "/update/draft/deleteDraft", Auth: true, Handle: deleteDraft},
	{Method: "POST", Path: "/update/speaks/newSpeaks", Auth: true, Handle: newSpeaks},
	{Method: "PUT", Path: "/update/speaks/updateSpeaks", Auth: true, Handle: updateSpeaks},
	{Method: "DELETE", Path: "/update/speaks/deleteSpeaks", Auth: true, Handle: deleteSpeaks},
	{Method: "POST", Path: "/update/siteInfo/latestUpdateTime", Auth: true, Handle: latestUpdateTimeWrite},
	{Method: "POST", Path: "/update/post/refreshSummary", Auth: true, Handle: refreshSummary},
	{Method: "POST", Path: "/update/flink/addFlink", Auth: true, Handle: addFlink},
	{Method: "PUT", Path: "/update/flink/updateFlink", Auth: true, Handle: updateFlink},
	{Method: "DELETE", Path: "/update/flink/deleteFlink", Auth: true, Handle: deleteFlink},
	{Method: "POST", Path: "/update/flink/addGroup", Auth: true, Handle: addGroup},
	{Method: "PUT", Path: "/update/flink/updateGroup", Auth: true, Handle: updateGroup},
	{Method: "DELETE", Path: "/update/flink/deleteGroup", Auth: true, Handle: deleteGroup},
	{Method: "POST", Path: "/update/flink/moveFlinkGroup", Auth: true, Handle: moveFlinkGroup},
	{Method: "POST", Path: "/update/flink/approveApplication", Auth: true, Handle: approveApplication},
	{Method: "POST", Path: "/update/flink/rejectApplication", Auth: true, Handle: rejectApplication},
	{Method: "POST", Path: "/update/flink/pushFlinkStatus", Handle: pushFlinkStatus},
	{Method: "POST", Path: "/flink/apply", Handle: flinkApply},
	{Method: "POST", Path: "/flink/screening", Handle: flinkScreening},
	{Method: "POST", Path: "/update/image/uploadImage", Auth: true, Handle: uploadImage},
	{Method: "PUT", Path: "/update/image/reuploadImage", Auth: true, Handle: reuploadImage},
	{Method: "POST", Path: "/update/image/rehostImageByUrl", Auth: true, Handle: rehostImageByUrl},
	{Method: "DELETE", Path: "/update/image/deleteImage", Auth: true, Handle: deleteImage},
	{Method: "POST", Path: "/get/export/collections", Handle: exportCollections},

	// ===== §4.4 直接回 success（功能已删，3 条） =====
	{Method: "DELETE", Path: "/update/post/deleteRenderedHtmlCache", Auth: true, Handle: deleteRenderedHtmlCache},
	{Method: "DELETE", Path: "/update/post/deleteAllRenderedHtmlCache", Auth: true, Handle: deleteRenderedHtmlCache},
	{Method: "DELETE", Path: "/update/draft/deleteRenderedHtmlCache", Auth: true, Handle: deleteRenderedHtmlCache},
}
