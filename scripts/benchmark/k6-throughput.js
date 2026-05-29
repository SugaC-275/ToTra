// k6 throughput benchmark — ToTra Gateway
//
// Ramps arrival rate to find maximum sustainable requests/second.
// Reports p99 latency at each load level to show where the gateway
// starts to degrade.
//
// Usage:
//   GATEWAY_URL=http://localhost:3000 API_KEY=your-key k6 run k6-throughput.js

import http from 'k6/http';
import { check } from 'k6';
import { Trend, Rate, Counter } from 'k6/metrics';

// ---------------------------------------------------------------------------
// Custom metrics
// ---------------------------------------------------------------------------

const reqDuration  = new Trend('req_duration_ms', true);
const errorRate    = new Rate('error_rate');
const droppedReqs  = new Counter('dropped_requests');

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

export const options = {
  scenarios: {
    ramp_up: {
      executor: 'ramping-arrival-rate',
      // Start at 10 RPS and ramp to find saturation point
      startRate: 10,
      timeUnit: '1s',
      preAllocatedVUs: 100,
      maxVUs: 600,
      stages: [
        { duration: '30s', target: 50  },   // 10 → 50 RPS
        { duration: '30s', target: 150 },   // 50 → 150 RPS
        { duration: '30s', target: 300 },   // 150 → 300 RPS
        { duration: '30s', target: 500 },   // 300 → 500 RPS (saturation probe)
        { duration: '30s', target: 150 },   // cool-down
        { duration: '30s', target: 50  },   // return to baseline
      ],
    },
  },

  thresholds: {
    // At all load levels, p99 should stay under 500ms
    'req_duration_ms': ['p(99)<500'],
    'error_rate': ['rate<0.05'],   // up to 5% errors allowed during saturation probe
    'http_req_failed': ['rate<0.05'],
  },
};

// ---------------------------------------------------------------------------
// Environment
// ---------------------------------------------------------------------------

const GATEWAY_URL = __ENV.GATEWAY_URL || 'http://localhost:3000';
const API_KEY     = __ENV.API_KEY     || 'test-key';
const MODEL       = __ENV.MODEL       || 'gpt-4o-mini';

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
// Default function
// ---------------------------------------------------------------------------

export default function () {
  const res = http.post(
    `${GATEWAY_URL}/v1/chat/completions`,
    chatPayload,
    { headers: authHeaders, timeout: '10s' },
  );

  const ok = check(res, {
    'status 200': (r) => r.status === 200,
    'has choices': (r) => {
      try { return Array.isArray(JSON.parse(r.body).choices); }
      catch { return false; }
    },
  });

  reqDuration.add(res.timings.duration);
  errorRate.add(!ok);

  if (res.status === 0) {
    // k6 marks dropped/timed-out requests as status 0
    droppedReqs.add(1);
  }
}

// ---------------------------------------------------------------------------
// Summary
// ---------------------------------------------------------------------------

export function handleSummary(data) {
  const fmt = (v) => v !== undefined ? `${v.toFixed(1)}ms` : 'n/a';
  const m   = (data.metrics['req_duration_ms'] || {}).values || {};

  const iters  = data.metrics['iterations']?.values?.count ?? 0;
  const elapsed = data.state?.testRunDurationMs ?? 0;
  const rps    = elapsed > 0 ? ((iters / elapsed) * 1000).toFixed(1) : 'n/a';

  const lines = [
    '=== ToTra Throughput Benchmark ===',
    '',
    `Total requests:  ${iters}`,
    `Actual RPS:      ${rps}`,
    '',
    'Latency percentiles across all load levels:',
    `  p50:  ${fmt(m['p(50)'])}`,
    `  p90:  ${fmt(m['p(90)'])}`,
    `  p95:  ${fmt(m['p(95)'])}`,
    `  p99:  ${fmt(m['p(99)'])}`,
    '',
    `Error rate:      ${((data.metrics['error_rate']?.values?.rate ?? 0) * 100).toFixed(2)}%`,
    `Dropped:         ${data.metrics['dropped_requests']?.values?.count ?? 0}`,
    '==================================',
  ].join('\n');

  return {
    stdout: lines,
    'results/throughput-summary.json': JSON.stringify(data, null, 2),
  };
}
