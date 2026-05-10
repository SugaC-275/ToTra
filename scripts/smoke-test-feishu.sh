#!/usr/bin/env bash
# smoke-test-feishu.sh — end-to-end smoke test for the Feishu webhook endpoint
#
# Usage:
#   export TOTRA_WEBHOOK_SECRET="your-32-char-secret-here"
#   export TOTRA_TENANT_ID="00000000-0000-0000-0000-000000000001"
#   export TOTRA_BASE_URL="http://localhost:8081"   # optional, default shown
#   bash scripts/smoke-test-feishu.sh
#
# Signature: HMAC-SHA256(key=WEBHOOK_SECRET, data=timestamp+body) → hex
# Headers:   X-Lark-Request-Timestamp, X-Lark-Signature

set -euo pipefail

BASE_URL="${TOTRA_BASE_URL:-http://localhost:8081}"
SECRET="${TOTRA_WEBHOOK_SECRET:?Set TOTRA_WEBHOOK_SECRET}"
TENANT_ID="${TOTRA_TENANT_ID:?Set TOTRA_TENANT_ID}"
ENDPOINT="${BASE_URL}/webhooks/feishu?tenant_id=${TENANT_ID}"

pass=0
fail=0

sign_and_post() {
  local label="$1"
  local body="$2"
  local ts
  ts=$(date +%s)
  local sig
  sig=$(printf '%s%s' "${ts}" "${body}" \
    | openssl dgst -sha256 -hmac "${SECRET}" \
    | sed 's/^.* //')

  local response
  response=$(curl -s -o /dev/null -w "%{http_code}:%{stdout}" \
    -X POST "${ENDPOINT}" \
    -H "Content-Type: application/json" \
    -H "X-Lark-Request-Timestamp: ${ts}" \
    -H "X-Lark-Signature: ${sig}" \
    -d "${body}" 2>/dev/null || true)

  # curl -w stdout trick doesn't work universally; capture body separately
  local http_code body_out
  body_out=$(curl -s \
    -X POST "${ENDPOINT}" \
    -H "Content-Type: application/json" \
    -H "X-Lark-Request-Timestamp: ${ts}" \
    -H "X-Lark-Signature: ${sig}" \
    -d "${body}")
  http_code=$(curl -s -o /dev/null -w "%{http_code}" \
    -X POST "${ENDPOINT}" \
    -H "Content-Type: application/json" \
    -H "X-Lark-Request-Timestamp: ${ts}" \
    -H "X-Lark-Signature: ${sig}" \
    -d "${body}")

  # For idempotency the third request will 409 if event already stored; treat as ok
  if echo "${body_out}" | grep -qE '"status"\s*:\s*"ok"' \
     || [ "${http_code}" = "409" ]; then
    echo "  PASS  ${label} (HTTP ${http_code})"
    pass=$((pass + 1))
  else
    echo "  FAIL  ${label} (HTTP ${http_code}) — ${body_out}"
    fail=$((fail + 1))
  fi
}

echo "ToTra Feishu Webhook Smoke Test"
echo "  endpoint : ${ENDPOINT}"
echo "  secret   : ${SECRET:0:4}****"
echo ""

# ── Test 1: task.completed ────────────────────────────────────────────────────
TASK_BODY=$(cat <<'JSON'
{
  "event": {
    "event_type": "task.completed",
    "task": {
      "task_id": "smoke-task-001",
      "summary": "Implement Feishu OAuth callback",
      "assignee": {
        "open_id": "ou_smoke_alice"
      }
    }
  }
}
JSON
)
sign_and_post "task.completed event" "${TASK_BODY}"

# ── Test 2: docs.created ─────────────────────────────────────────────────────
DOC_BODY=$(cat <<'JSON'
{
  "event": {
    "event_type": "docs.created",
    "doc": {
      "doc_token": "smoke-doc-001",
      "title": "API Design: ToTra Gateway",
      "creator": {
        "open_id": "ou_smoke_alice"
      }
    }
  }
}
JSON
)
sign_and_post "docs.created event" "${DOC_BODY}"

# ── Test 3: invalid signature ─────────────────────────────────────────────────
echo -n "  TEST  invalid signature → expect 401 ... "
ts=$(date +%s)
bad_response=$(curl -s -w "\n%{http_code}" \
  -X POST "${ENDPOINT}" \
  -H "Content-Type: application/json" \
  -H "X-Lark-Request-Timestamp: ${ts}" \
  -H "X-Lark-Signature: badsignature" \
  -d '{"event":{"event_type":"task.completed"}}')
http_code=$(echo "${bad_response}" | tail -1)
if [ "${http_code}" = "401" ]; then
  echo "PASS (HTTP 401)"
  pass=$((pass + 1))
else
  echo "FAIL (got HTTP ${http_code})"
  fail=$((fail + 1))
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Results: ${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ] && exit 0 || exit 1
