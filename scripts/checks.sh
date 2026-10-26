#!/usr/bin/env bash
# 静态守卫 · glue
# 用法：bash scripts/checks.sh
# 挂了就修代码，不要改脚本。

set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

fail=0
say() { printf '%s\n' "$*"; }
bad() { fail=1; say "  ✗ $*"; }
ok()  { say "  ✓ $*"; }
files() { find cmd internal -name '*.go' -not -path '*/vendor/*' 2>/dev/null; }

say "== 1. 格式 =="
unformatted=$(gofmt -l cmd internal 2>/dev/null)
if [ -n "$unformatted" ]; then
  bad "没跑 gofmt："; printf '%s\n' "$unformatted" | sed 's/^/      /'
else ok "gofmt 干净"; fi

say "== 2. 编译与静态检查 =="
if go build ./... 2>&1 | sed 's/^/      /'; then ok "go build 通过"; else bad "go build 失败"; fi
if go vet ./... 2>&1 | sed 's/^/      /'; then ok "go vet 通过"; else bad "go vet 失败"; fi

say "== 3. 不许连数据库 =="
hits=$(files | xargs -r grep -nHE 'mongo|postgres|sql\.Open|redis|database/sql' 2>/dev/null)
if [ -n "$hits" ]; then
  bad "glue 只做转发，不该有任何数据库依赖："; printf '%s\n' "$hits" | sed 's/^/      /'
else ok "没有任何数据库代码"; fi

say "== 4. 不许 import 后端仓库的 internal 包 =="
hits=$(files | xargs -r grep -nHE 'ariablognext-backend-go/internal' 2>/dev/null)
if [ -n "$hits" ]; then
  bad "两个仓库要能独立部署，不能互相 import："; printf '%s\n' "$hits" | sed 's/^/      /'
else ok "没有跨仓库 import"; fi

say "== 5. 不许 fmt.Print* 打日志 =="
hits=$(files | grep -v '_test\.go$' | xargs -r grep -nHE 'fmt\.Print(f|ln)?\(' 2>/dev/null)
if [ -n "$hits" ]; then
  bad "请用 log/slog："; printf '%s\n' "$hits" | sed 's/^/      /'
else ok "没有 fmt.Print 日志"; fi

say "== 6. 每个 internal 子包都要有测试 =="
missing=""
for d in $(find internal -type d | sort); do
  [ "$d" = "internal" ] && continue
  ls "$d"/*_test.go >/dev/null 2>&1 || missing="$missing $d"
done
if [ -n "$missing" ]; then bad "缺 *_test.go ：$missing"; else ok "子包测试齐全"; fi

say ""
if [ "$fail" -eq 0 ]; then say "全部通过 ✅"; exit 0; else say "存在违规 ❌"; exit 1; fi
