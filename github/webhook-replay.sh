#!/usr/bin/env bash
# 本地验 webhook：自己算 HMAC-SHA256 签名，往平台的 webhook 地址打一发假 GitHub 事件。
#
# 为什么要有这个：webhook 那条路 Go 单测覆盖不到——它要平台在跑、要真的收一个 HTTP 请求。
# 而让真 GitHub 打到本机又需要内网穿透。这个脚本把「GitHub 发过来」那一步换成本地 curl，
# 签名照真算，所以验签、分发、去重三段都是真的在跑。
#
#   ./webhook-replay.sh <平台webhook地址> <secret> [事件类型] [payload文件]
#
# 例：
#   ./webhook-replay.sh http://localhost:8088/hooks/whk_xxx s3cret issues
#   ./webhook-replay.sh http://localhost:8088/hooks/whk_xxx s3cret pull_request ./my.json
#
# 不给 payload 文件就用内置的样例（按事件类型选）。
#
# 验完去平台的「插件详情 → Webhook」tab 看日志：
#   · secret 对的那发 → 200，且「触发事件数」非 0
#   · secret 错的那发 → 401，触发事件数 0
# 两条都在，才算这条路是通的。
set -euo pipefail

URL="${1:?用法: $0 <平台webhook地址> <secret> [事件类型] [payload文件]}"
SECRET="${2:?缺 secret}"
EVENT="${3:-issues}"
FILE="${4:-}"

if [ -n "$FILE" ]; then
  BODY="$(cat "$FILE")"
else
  case "$EVENT" in
    ping)
      BODY='{"zen":"Keep it logically awesome.","hook_id":1}' ;;
    issues)
      BODY='{"action":"opened","repository":{"full_name":"acme/demo"},
"issue":{"number":101,"title":"联调用的 Issue","body":"replay","html_url":"https://github.com/acme/demo/issues/101",
"user":{"login":"alice","type":"User"},"labels":[{"name":"bug"}]}}' ;;
    issue_comment)
      BODY='{"action":"created","repository":{"full_name":"acme/demo"},
"issue":{"number":101,"title":"联调用的 Issue"},
"comment":{"id":9001,"body":"/deploy","html_url":"https://github.com/acme/demo/issues/101#issuecomment-9001",
"author_association":"MEMBER","user":{"login":"alice","type":"User"}}}' ;;
    pull_request)
      BODY='{"action":"opened","repository":{"full_name":"acme/demo"},
"pull_request":{"number":7,"title":"联调用的 PR","body":"replay","draft":false,
"user":{"login":"alice"},"base":{"ref":"main"},"head":{"ref":"feature","sha":"deadbeef"},
"labels":[],"html_url":"https://github.com/acme/demo/pull/7"}}' ;;
    pull_request_merged)
      EVENT=pull_request
      BODY='{"action":"closed","repository":{"full_name":"acme/demo"},
"pull_request":{"number":7,"title":"联调用的 PR","merged":true,"merge_commit_sha":"cafe1234",
"user":{"login":"alice"},"merged_by":{"login":"bob"},"base":{"ref":"main"},
"labels":[],"html_url":"https://github.com/acme/demo/pull/7"}}' ;;
    workflow_run)
      BODY='{"action":"completed","repository":{"full_name":"acme/demo"},
"workflow_run":{"id":555,"name":"CI","conclusion":"failure","head_branch":"main",
"head_sha":"deadbeef","event":"push","run_attempt":1,
"actor":{"login":"alice"},"html_url":"https://github.com/acme/demo/actions/runs/555"}}' ;;
    *)
      echo "内置样例只有 ping / issues / issue_comment / pull_request / pull_request_merged / workflow_run" >&2
      echo "别的类型请自己给 payload 文件（第 4 个参数）" >&2
      exit 2 ;;
  esac
fi

# 签名必须算在**将要发出去的那串字节**上，所以先定下 BODY 再签，中间不能再动它。
SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" -r | cut -d' ' -f1)"
DELIVERY="replay-$(date +%s)-$RANDOM"

send() {
  local sig="$1" label="$2"
  echo "── $label"
  curl -sS -o /dev/stderr -w '   HTTP %{http_code}\n' -X POST "$URL" \
    -H 'Content-Type: application/json' \
    -H "X-GitHub-Event: $EVENT" \
    -H "X-GitHub-Delivery: $DELIVERY" \
    -H "X-Hub-Signature-256: $sig" \
    --data-binary "$BODY"
  echo
}

echo "事件=$EVENT  投递id=$DELIVERY"
send "$SIG" "签名正确（应 200，且平台侧触发事件数 +1）"
send "sha256=0000000000000000000000000000000000000000000000000000000000000000" \
     "签名错误（应 401，且不触发任何事件）"

cat <<'TIP'
──
两发都打完了。接着去平台看：
  1. 插件详情 → Webhook tab：两条日志都该在（200 一条、401 一条）
  2. 用同一个 投递id 再打一次正确签名的那发 —— 平台应当**去重**，不再触发第二次
     （event_id 用的就是 X-GitHub-Delivery，GitHub 重投时也是同一个）
TIP
