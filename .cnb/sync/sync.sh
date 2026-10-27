#!/usr/bin/env bash
# 本仓库 <-> belem/chinese-poetry-api 的 develop 双向同步
#
# 用法: sync.sh push|pull|both
#   push  本仓库(CNB) → GitHub
#   pull  GitHub → 本仓库(CNB)
#   both  先 pull 再 push（手动对齐用）
#
# 设计要点：
# - 只维护 develop 一个分支，两侧都只做 fast-forward，分叉就失败退出，绝不强推。
# - 分支级校验（禁止直推 develop）在调用方用 if 完成，这里只管搬代码。
set -euo pipefail

MODE="${1:-push}"
BRANCH="${SYNC_BRANCH:-develop}"
GH_SLUG="${GITHUB_API_REPO:-belem/chinese-poetry-api}"
WORK="${WORK_DIR:-${PWD}/.sync-work}"

log() { printf '[sync] %s\n' "$*"; }

PAT="${GITHUB_API_TOKEN:-${GITHUB_PAT:-${GITHUB_TOKEN:-}}}"

gh_url() {
  if [ -n "$PAT" ]; then
    # PAT 只出现在本次命令的 URL 里，不落 .git/config、不进日志
    printf 'https://x-access-token:%s@github.com/%s.git' "$PAT" "$GH_SLUG"
  else
    printf 'https://github.com/%s.git' "$GH_SLUG"
  fi
}

require_pat() {
  if [ -z "$PAT" ]; then
    log "没有 GITHUB_API_TOKEN，跳过与 GitHub 的同步"
    log "预期来源：密钥仓库 ${SECRETS_SLUG:-?}#${SECRETS_FILE:-?} 经 .cnb.yml 的 imports 注入"
    exit 0
  fi
  log "凭据已就绪（长度 ${#PAT}，值不打印）"
}

gh_head() {
  git ls-remote "$(gh_url)" "refs/heads/${BRANCH}" | cut -f1 | head -1
}

do_push() {
  require_pat
  log "CNB → GitHub ${GH_SLUG}#${BRANCH}"
  local head remote_head
  head=$(git rev-parse HEAD)
  remote_head=$(gh_head)
  if [ "$head" = "$remote_head" ]; then
    log "已一致，无需推送"
    return 0
  fi
  # 非快进会直接报错，避免把 GitHub 侧的人工提交冲掉
  git push --quiet "$(gh_url)" "HEAD:refs/heads/${BRANCH}"
  log "推送完成 ${head:0:8}"
}

do_pull() {
  require_pat
  log "GitHub → CNB ${GH_SLUG}#${BRANCH}"
  rm -rf "$WORK"
  git init --quiet "$WORK"
  git -C "$WORK" fetch --quiet --depth=1 "$(gh_url)" "$BRANCH"
  local gh_sha local_sha
  gh_sha=$(git -C "$WORK" rev-parse FETCH_HEAD)
  rm -rf "$WORK"

  git fetch --quiet origin "$BRANCH"
  local_sha=$(git rev-parse HEAD)

  if [ "$gh_sha" = "$local_sha" ]; then
    log "已一致，无需拉取"
    return 0
  fi

  if git merge-base --is-ancestor "$local_sha" "$gh_sha"; then
    log "CNB 落后，快进到 ${gh_sha:0:8}"
    git merge --ff-only --quiet "$gh_sha"
    git push --quiet origin "HEAD:refs/heads/${BRANCH}"
    log "已更新到 ${gh_sha:0:8}"
  else
    log "CNB 侧有 GitHub 没有的提交，跳过拉取（交给 push 环节）"
  fi
}

case "$MODE" in
  push)
    # push 事件只在「PR 合入 develop」时放行（guard.sh 已把关），这正是要同步的那一次。
    # 同步本身推的是 GitHub，不会再触发本仓库的 push，不存在自激。
    do_push
    ;;
  pull) do_pull ;;
  both) do_pull; do_push ;;
  *) log "未知模式: $MODE（可选 push / pull / both）"; exit 2 ;;
esac
