#!/usr/bin/env python3
"""旧契约冒烟测试（只用标准库）。

验证 glue 输出的是**旧格式**：有 message，没有 ok/requestId/error。
这是最容易出错的地方——翻译不干净时前端会白屏，但状态码还是 200。

用法：
    python3 tests/smoke.py                                  # glue 在 2334，上游在 2333
    python3 tests/smoke.py --base https://blognext-end.yaria.top
    python3 tests/smoke.py --only posts                     # 只跑某个域

退出码：0 全过；1 有失败。
"""
from __future__ import annotations

import argparse
import json
import sys
import urllib.error
import urllib.parse
import urllib.request

TIMEOUT = 20

# 旧只读接口：(路径, query, 期望出现的顶层键, 期望状态码)
READS = [
    ("/get/post/postsInfo", {"startl": 0, "endl": 3}, "data", 200),
    ("/get/post/postsInfo", {"type": "full"}, "data", 200),
    ("/get/post/postCount", {}, "count", 200),
    ("/get/post/totalWordCount", {}, "count", 200),
    ("/get/post/postSlugs", {}, "data", 200),
    ("/get/archive/archives", {}, "data", 200),
    ("/get/tag/tags", {}, "data", 200),
    ("/get/tag/tagCount", {}, "count", 200),
    ("/get/category/categories", {}, "data", 200),
    ("/get/category/categoryCount", {}, "count", 200),
    ("/get/speaks/speaks", {"startl": 0, "endl": 3}, "data", 200),
    ("/get/speaks/speaksCount", {}, "count", 200),
    ("/get/flink/flinks", {}, "data", 200),
    ("/get/flink/flinkCount", {}, "count", 200),
    ("/get/siteInfo/lastUpdateTime", {}, "time", 200),
]

NEEDS_PARAM = [
    # 旧接口在缺参数时是 422（FastAPI 默认）或 400；这里只要求「不是 2xx 且不是 500」
    ("/get/post/postBySlug", {}, 404),
    ("/get/archive/archiveInfo", {"year": 1999, "month": 1}, 404),
]

FORBIDDEN_KEYS = ("ok", "requestId", "error")


class Fail(Exception):
    pass


def get(base, path, query):
    url = base.rstrip("/") + path
    if query:
        url += "?" + urllib.parse.urlencode(query)
    req = urllib.request.Request(url, headers={"Accept": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=TIMEOUT) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def check_legacy_shape(method_path, status, raw, want_key, want_status):
    if want_status and status != want_status:
        raise Fail(f"期望状态码 {want_status}，实际 {status}")
    try:
        obj = json.loads(raw.decode("utf-8"))
    except Exception:
        raise Fail(f"不是 JSON：{raw[:100]!r}")
    leaked = [k for k in FORBIDDEN_KEYS if k in obj]
    if leaked:
        raise Fail(f"响应里出现了新信封字段 {leaked}，说明没翻译干净")
    if obj.get("message") != "success":
        raise Fail(f'message 不是 "success"：{obj.get("message")!r}')
    if want_key and want_key not in obj:
        raise Fail(f"缺少顶层键 {want_key}，实际键：{list(obj.keys())}")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://127.0.0.1:2334")
    ap.add_argument("--upstream", default="", help="可选：同时检查上游是否活着")
    ap.add_argument("--only", default="", help="按路径关键字过滤")
    args = ap.parse_args()

    if args.upstream:
        try:
            with urllib.request.urlopen(args.upstream.rstrip("/") + "/health", timeout=5) as r:
                if r.status != 200:
                    print(f"⚠️  上游 /health 返回 {r.status}")
        except Exception as e:
            print(f"⚠️  上游不可达：{e}（glue 的转发测试可能全挂）")

    fails, total = [], 0

    def run(name, fn):
        nonlocal total
        total += 1
        try:
            fn()
            print(f"  ✓ {name}")
        except Fail as e:
            fails.append(f"{name}: {e}")
            print(f"  ✗ {name}\n      {e}")

    print(f"旧契约冒烟 → {args.base}\n[1] 只读接口")
    for path, q, key, want in READS:
        if args.only and args.only not in path:
            continue
        def t(path=path, q=q, key=key, want=want):
            st, raw = get(args.base, path, q)
            check_legacy_shape(path, st, raw, key, want)
        run(f"GET {path} {q or ''}", t)

    print("[2] 参数缺失/不存在")
    for path, q, want in NEEDS_PARAM:
        def t(path=path, q=q, want=want):
            st, raw = get(args.base, path, q)
            if 200 <= st < 300:
                raise Fail(f"期望非 2xx，实际 {st}")
            if st >= 500:
                raise Fail(f"期望 4xx，实际 {st}")
            obj = json.loads(raw.decode("utf-8"))
            if "detail" not in obj:
                raise Fail("旧格式的错误响应必须有 detail 字段")
        run(f"GET {path} → {want}", t)

    print()
    if fails:
        print(f"失败 {len(fails)} / {total} ❌")
        for f in fails:
            print("  -", f)
        return 1
    print(f"全部通过 {total} / {total} ✅")
    return 0


if __name__ == "__main__":
    sys.exit(main())
