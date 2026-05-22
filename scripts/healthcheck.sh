#!/usr/bin/env bash
# healthcheck.sh — verify docker compose is fully healthy and all endpoints respond
#
# Usage:
#   bash scripts/healthcheck.sh
#   bash scripts/healthcheck.sh --no-start  # skip docker compose up, just probe

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NO_START="${1:-}"
PASS=0; FAIL=0

log()  { echo "  $*"; }
ok()   { echo "  PASS  $1"; PASS=$((PASS+1)); }
fail() { echo "  FAIL  $1 — $2"; FAIL=$((FAIL+1)); }

# ── 1. Start services ───────────────────────────────────────────────────────
if [ "${NO_START}" != "--no-start" ]; then
  echo "Starting docker compose..."
  docker-compose -f "${ROOT}/docker-compose.yml" up -d --wait 2>&1 | tail -5
fi

# ── 2. Wait helper ──────────────────────────────────────────────────────────
wait_for() {
  local label="$1" url="$2" attempts=0
  until curl -sf "${url}" > /dev/null 2>&1; do
    attempts=$((attempts+1))
    [ ${attempts} -ge 30 ] && fail "${label}" "timed out after 30s" && return
    sleep 1
  done
  ok "${label} reachable"
}

echo ""
echo "ToTra Docker Compose Health Check"
echo ""

# Load port defaults
source "${ROOT}/.env" 2>/dev/null || true
ADMIN_PORT="${ADMIN_PORT:-8081}"
GATEWAY_PORT="${GATEWAY_PORT:-8080}"

# ── 3. Probe endpoints ──────────────────────────────────────────────────────
wait_for "admin  /health" "http://localhost:${ADMIN_PORT}/health"
wait_for "gateway /health" "http://localhost:${GATEWAY_PORT}/health"

# ── 4. Admin login smoke test ───────────────────────────────────────────────
echo -n "  TEST  admin login ... "
LOGIN=$(curl -sf -X POST "http://localhost:${ADMIN_PORT}/api/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@acme.com","password":"totra-emp-dev-admin-key"}' 2>/dev/null || echo "")
if echo "${LOGIN}" | grep -q '"token"'; then
  echo "PASS"
  PASS=$((PASS+1))
else
  echo "FAIL — ${LOGIN}"
  FAIL=$((FAIL+1))
fi

# ── 5. Feishu smoke test (if secret is set) ─────────────────────────────────
if [ -n "${TOTRA_WEBHOOK_SECRET:-}" ]; then
  TOTRA_BASE_URL="http://localhost:${ADMIN_PORT}" \
  TOTRA_TENANT_ID="00000000-0000-0000-0000-000000000001" \
  bash "${ROOT}/scripts/smoke-test-feishu.sh"
else
  log "Skipping feishu smoke test (TOTRA_WEBHOOK_SECRET not set)"
fi

# ── 6. Summary ──────────────────────────────────────────────────────────────
echo ""
echo "Results: ${PASS} passed, ${FAIL} failed"
[ "${FAIL}" -eq 0 ] && exit 0 || exit 1
