// k6 load test — ToTra Gateway
//
// Usage:
//   k6 run k6.js -e GATEWAY_URL=http://localhost:8080 -e API_KEY=your-key
//
// Scenarios:
//   baseline     – 10 VUs for 2 minutes (normal load)
//   stress       – ramp to 100 VUs over 5 minutes (stress test)
//   health_steady – 5 VUs throughout (gateway-only latency baseline)
//
// Thresholds:
//   P95 response time  < 200 ms  (chat completions)
//   Gateway latency P95 < 50 ms  (/health probe)
//   Error rate          < 1 %

import http from "k6/http";
import { check, sleep } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";

// ---------------------------------------------------------------------------
// Custom metrics
// ---------------------------------------------------------------------------

const chatP95       = new Trend("chat_duration_ms", true);
const healthP95     = new Trend("health_duration_ms", true);
const authFailures  = new Counter("auth_failures_total");
const quotaHits     = new Counter("quota_hits_total");
const errorRate     = new Rate("error_rate");

// ---------------------------------------------------------------------------
// Scenario / threshold configuration
// ---------------------------------------------------------------------------

export const options = {
  scenarios: {
    // Normal steady-state traffic
    baseline: {
      executor: "constant-vus",
      vus: 10,
      duration: "2m",
      exec: "chatCompletion",
      startTime: "0s",
    },

    // Gateway self health — runs for the full test duration
    health_steady: {
      executor: "constant-vus",
      vus: 5,
      duration: "7m",   // covers baseline + stress window
      exec: "healthCheck",
      startTime: "0s",
    },

    // Ramp to 100 VUs and back down
    stress: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "1m",  target: 50  },
        { duration: "2m",  target: 100 },
        { duration: "1m",  target: 50  },
        { duration: "1m",  target: 0   },
      ],
      exec: "chatCompletion",
      startTime: "2m",  // starts after baseline finishes
    },
  },

  thresholds: {
    // Overall P95 for chat completions (includes LLM round-trip)
    chat_duration_ms:   ["p(95)<200"],
    // Gateway-only overhead measured via /health
    health_duration_ms: ["p(95)<50"],
    // Global HTTP error rate
    error_rate:         ["rate<0.01"],
    // k6 built-in: keep a check on raw HTTP failures too
    http_req_failed:    ["rate<0.01"],
  },
};

// ---------------------------------------------------------------------------
// Environment
// ---------------------------------------------------------------------------

const BASE_URL = __ENV.GATEWAY_URL || "http://localhost:8080";
const API_KEY  = __ENV.API_KEY     || "test-key";
const MODEL    = __ENV.MODEL       || "gpt-4o-mini";

const authHeaders = {
  "Authorization": `Bearer ${API_KEY}`,
  "Content-Type": "application/json",
};

// ---------------------------------------------------------------------------
// Scenario functions
// ---------------------------------------------------------------------------

/**
 * healthCheck — probes /health to measure pure gateway overhead.
 * No auth, no upstream LLM call.
 */
export function healthCheck() {
  const res = http.get(`${BASE_URL}/health`);
  const ok = check(res, {
    "health 200": (r) => r.status === 200,
    "health body ok": (r) => {
      try { return JSON.parse(r.body).status === "ok"; }
      catch { return false; }
    },
  });

  healthP95.add(res.timings.duration);
  errorRate.add(!ok);

  sleep(1);
}

/**
 * chatCompletion — happy-path chat request through the full middleware stack.
 */
export function chatCompletion() {
  const payload = JSON.stringify({
    model: MODEL,
    messages: [
      { role: "user", content: "Reply with exactly one word: OK" },
    ],
    max_tokens: 5,
  });

  const res = http.post(`${BASE_URL}/v1/chat/completions`, payload, {
    headers: authHeaders,
  });

  const ok = check(res, {
    "chat 200": (r) => r.status === 200,
    "chat has choices": (r) => {
      try { return Array.isArray(JSON.parse(r.body).choices); }
      catch { return false; }
    },
  });

  chatP95.add(res.timings.duration);
  errorRate.add(!ok);

  sleep(0.5);
}

/**
 * authFailure — confirms that an invalid API key gets a 401.
 * Counts results in a dedicated counter rather than the main error rate
 * because a 401 here is the *expected* behaviour.
 */
export function authFailure() {
  const payload = JSON.stringify({
    model: MODEL,
    messages: [{ role: "user", content: "hello" }],
  });

  const res = http.post(`${BASE_URL}/v1/chat/completions`, payload, {
    headers: {
      "Authorization": "Bearer invalid-key-000",
      "Content-Type": "application/json",
    },
  });

  const got401 = check(res, {
    "auth rejected 401": (r) => r.status === 401,
  });

  if (got401) {
    authFailures.add(1);
  } else {
    errorRate.add(1); // unexpected response — count as real error
  }

  sleep(1);
}

/**
 * quotaExceeded — fires requests rapidly to a single tenant and expects 429s
 * once the rate limiter / quota middleware kicks in.
 * Does NOT count 429 as an error (it is the intended gateway behaviour).
 */
export function quotaExceeded() {
  const payload = JSON.stringify({
    model: MODEL,
    messages: [{ role: "user", content: "ping" }],
    max_tokens: 1,
  });

  let hits = 0;
  for (let i = 0; i < 20; i++) {
    const res = http.post(`${BASE_URL}/v1/chat/completions`, payload, {
      headers: authHeaders,
    });

    check(res, {
      "quota: 429 or 200": (r) => r.status === 429 || r.status === 200,
    });

    if (res.status === 429) {
      hits++;
    }
  }

  quotaHits.add(hits);
  sleep(2);
}
