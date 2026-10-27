#!/usr/bin/env bash
# 变更只走 develop 分支 + PR：拒绝任何直推受保护分支的提交。
# 由 develop 的 push 事件调用；同步流水线推回 develop 时也会走到这里。
set -euo pipefail

branch="${SYNC_BRANCH:-develop}"
incoming="${CNB_BRANCH:-}"

if [ "$incoming" = "$branch" ]; then
  cat <<'MSG'
[guard] 提交被拒绝：develop 是受保护分支，所有变更必须提 PR 合入。
[guard] 开一个分支（如 auto/xxx）推上去，提 PR；PR 合并后会自动同步到 GitHub。
MSG
  exit 1
fi

echo "[guard] ok: ${incoming}"
