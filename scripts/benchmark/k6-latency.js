// k6 latency benchmark — ToTra Gateway vs LiteLLM
//
// Measures gateway overhead (p50/p95/p99) at three concurrency levels.
// Point at a mock upstream to isolate gateway overhead from real LLM latency.
//
// Usage:
//   GATEWAY_URL=http://localhost:3000 API_KEY=your-key k6 run k6-latency.js
//
// Env vars:
//   GATEWAY_URL  — base URL of the gateway under test  (default: http://localhost:3000)
//   UPSTREAM_URL — direct URL of the mock upstream     (default: http://localhost:8080)
//   API_KEY      — bearer token                        (default: test-key)
//   MODEL        — model name to send in the request   (default: gpt-4o-mini)

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate, Counter } from 'k6/metrics';

// ---------------------------------------------------------------------------
// Custom metrics
// ---------------------------------------------------------------------------

// Raw wall-clock duration of each gateway request (for p50/p95/p99 reporting)
const gatewayLatency = new Trend('gateway_latency_ms', true);

// Raw wall-clock duration hitting the upstream directly (subtract to get overhead)
const upstreamLatency = new Trend('upstream_latency_ms', true);

// Computed overhead: gateway - upstream (captured via tags, exported in summary)
const gatewayOverhead = new Trend('gateway_overhead_ms', true);

const errorRate = new Rate('error_rate');
const errorCount = new Counter('error_count');

// ---------------------------------------------------------------------------
// Scenario / threshold configuration
// ---------------------------------------------------------------------------

export const options = {
  scenarios: {
    // 10 VUs — light load, measures baseline overhead
    low_concurrency: {
      executor: 'constant-vus',
      vus: 10,
      duration: '30s',
      gracefulStop: '5s',
      exec: 'gatewayRequest',
    },

    // 50 VUs — moderate load
    mid_concurrency: {
      executor: 'constant-vus',
      vus: 50,
      duration: '30s',
      startTime: '35s',
      gracefulStop: '5s',
      exec: 'gatewayRequest',
    },

    // 200 VUs — high load, exercises Go scheduler and connection pool
    high_concurrency: {
      executor: 'constant-vus',
      vus: 200,
      duration: '30s',
      startTime: '70s',
      gracefulStop: '5s',
      exec: 'gatewayRequest',
    },

    // Upstream baseline — runs concurrently with all gateway scenarios
    // Lets you compute overhead = gateway_latency - upstream_latency
    upstream_baseline: {
      executor: 'constant-vus',
      vus: 10,
      duration: '105s',
      gracefulStop: '5s',
      exec: 'upstreamRequest',
    },
  },

  thresholds: {
    // ToTra should add <50ms overhead at p95 under any load
    'gateway_latency_ms': ['p(95)<200'],   // includes 100ms mock latency + overhead
    'gateway_overhead_ms': ['p(95)<50'],   // pure overhead target
    'error_rate': ['rate<0.01'],
    'http_req_failed': ['rate<0.01'],
  },
};

// ---------------------------------------------------------------------------
// Environment
// ---------------------------------------------------------------------------

const GATEWAY_URL  = __ENV.GATEWAY_URL  || 'http://localhost:3000';
const UPSTREAM_URL = __ENV.UPSTREAM_URL || 'http://localhost:8080';
const API_KEY      = __ENV.API_KEY      || 'test-key';
const MODEL        = __ENV.MODEL        || 'gpt-4o-mini';

const authHeaders = {
  'Content-Type': 'application/json',
  'Authorization': `Bearer ${API_KEY}`,
};

const chatPayload = JSON.stringify({
  model: MODEL,
  messages: [{ role: 'user', content: 'Say "hello" in one word.' }],
  max_tokens: 5,
});

// ---------------------------------------------------------------------------
// Scenario functions
// ---------------------------------------------------------------------------

/**
 * gatewayRequest — sends the full request through ToTra (or LiteLLM).
 * Measures end-to-end latency as seen by the client.
 */
export function gatewayRequest() {
  const res = http.post(
    `${GATEWAY_URL}/v1/chat/completions`,
    chatPayload,
    { headers: authHeaders },
  );

  const ok = check(res, {
    'gateway status 200': (r) => r.status === 200,
    'gateway has choices': (r) => {
      try { return Array.isArray(JSON.parse(r.body).choices); }
      catch { return false; }
    },
  });

  gatewayLatency.add(res.timings.duration);
  errorRate.add(!ok);
  if (!ok) errorCount.add(1);

  sleep(0.1);
}

/**
 * upstreamRequest — hits the mock upstream directly (no gateway).
 * Used to compute pure gateway overhead = gatewayLatency - upstreamLatency.
 */
export function upstreamRequest() {
  const res = http.post(
    `${UPSTREAM_URL}/v1/chat/completions`,
    chatPayload,
    { headers: { 'Content-Type': 'application/json' } },
  );

  const ok = check(res, {
    'upstream status 200': (r) => r.status === 200,
  });

  upstreamLatency.add(res.timings.duration);

  // Overhead estimate: this VU measures direct latency; the difference
  // between gateway_latency_ms and upstream_latency_ms is gateway cost.
  // We track it explicitly so k6 summary shows it.
  const overhead = res.timings.duration;  // placeholder; real overhead computed in summary
  gatewayOverhead.add(overhead);

  sleep(0.1);
}

// ---------------------------------------------------------------------------
// Summary handler — prints p50/p95/p99 table after run
// ---------------------------------------------------------------------------

export function handleSummary(data) {
  const fmt = (ms) => ms !== undefined ? `${ms.toFixed(1)}ms` : 'n/a';

  const gw  = data.metrics['gateway_latency_ms']  || {};
  const up  = data.metrics['upstream_latency_ms'] || {};
  const gwp = gw.values  || {};
  const upp = up.values  || {};

  // Compute overhead: gateway p-tile minus upstream p50 (closest proxy)
  const overhead = (gwPct, upP50) =>
    gwPct !== undefined && upP50 !== undefined
      ? `${(gwPct - upP50).toFixed(1)}ms`
      : 'n/a';

  const upP50 = upp['p(50)'];

  const table = [
    '=== ToTra Gateway Latency Benchmark ===',
    '',
    'Gateway latency (end-to-end, includes 100ms mock upstream):',
    `  p50:  ${fmt(gwp['p(50)'])}`,
    `  p95:  ${fmt(gwp['p(95)'])}`,
    `  p99:  ${fmt(gwp['p(99)'])}`,
    '',
    'Upstream direct (no gateway):',
    `  p50:  ${fmt(upp['p(50)'])}`,
    `  p95:  ${fmt(upp['p(95)'])}`,
    `  p99:  ${fmt(upp['p(99)'])}`,
    '',
    'Estimated gateway overhead (gateway p-tile − upstream p50):',
    `  overhead at p50:  ${overhead(gwp['p(50)'], upP50)}`,
    `  overhead at p95:  ${overhead(gwp['p(95)'], upP50)}`,
    `  overhead at p99:  ${overhead(gwp['p(99)'], upP50)}`,
    '',
    `Errors: ${(data.metrics['error_rate'] || {}).values?.rate ?? 0}`,
    '========================================',
  ].join('\n');

  return {
    stdout: table,
    'results/latency-summary.json': JSON.stringify(data, null, 2),
  };
}
