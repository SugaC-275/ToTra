# ToTra Gateway — k6 Load Tests

## Install k6

**macOS**
```bash
brew install k6
```

**Linux**
```bash
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg \
    --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" \
    | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update && sudo apt-get install k6
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `GATEWAY_URL` | `http://localhost:8080` | Base URL of the running gateway |
| `API_KEY` | `test-key` | Valid API key for chat requests |
| `MODEL` | `gpt-4o-mini` | Model name configured for your tenant |

## Running the Tests

### Full test suite (baseline + stress, ~7 min total)
```bash
k6 run k6.js \
  -e GATEWAY_URL=http://localhost:8080 \
  -e API_KEY=sk-... \
  -e MODEL=gpt-4o-mini
```

### Baseline only (10 VUs, 2 min)
```bash
k6 run k6.js --scenario baseline \
  -e GATEWAY_URL=http://localhost:8080 \
  -e API_KEY=sk-...
```

### Stress only (ramp to 100 VUs)
```bash
k6 run k6.js --scenario stress \
  -e GATEWAY_URL=http://localhost:8080 \
  -e API_KEY=sk-...
```

### Health probe only (gateway latency, no LLM)
```bash
k6 run k6.js --scenario health_steady \
  -e GATEWAY_URL=http://localhost:8080
```

## Thresholds

| Metric | Threshold | What it measures |
|---|---|---|
| `chat_duration_ms p(95)` | < 200 ms | End-to-end chat latency incl. LLM |
| `health_duration_ms p(95)` | < 50 ms | Gateway-only overhead (`/health`) |
| `error_rate` | < 1 % | Non-2xx, non-429 responses |
| `http_req_failed` | < 1 % | k6 built-in transport errors |

A non-zero exit code from k6 means at least one threshold was breached.

## Custom Metrics

| Metric | Type | Description |
|---|---|---|
| `chat_duration_ms` | Trend | Duration of `/v1/chat/completions` requests |
| `health_duration_ms` | Trend | Duration of `/health` requests |
| `auth_failures_total` | Counter | Expected 401 responses from `authFailure` scenario |
| `quota_hits_total` | Counter | 429 responses from `quotaExceeded` scenario |
| `error_rate` | Rate | Proportion of unexpected failures |

## Connecting to Grafana (via InfluxDB)

k6 can stream results to InfluxDB in real time. The Grafana k6 dashboard
(ID `2587`) visualises all built-in and custom metrics.

```bash
# Start InfluxDB + Grafana (Docker)
docker run -d --name influxdb -p 8086:8086 influxdb:1.8
docker run -d --name grafana  -p 3000:3000 grafana/grafana

# Import dashboard 2587 in Grafana, then run:
k6 run k6.js \
  --out influxdb=http://localhost:8086/k6 \
  -e GATEWAY_URL=http://localhost:8080 \
  -e API_KEY=sk-...
```

Open `http://localhost:3000` and select the k6 dashboard.

## CI Integration

```yaml
# .github/workflows/load-test.yml (example)
- name: Run k6 smoke test
  uses: grafana/k6-action@v0.3.1
  with:
    filename: scripts/load-test/k6.js
    flags: >
      --scenario health_steady
      -e GATEWAY_URL=${{ secrets.GATEWAY_URL }}
      -e API_KEY=${{ secrets.LOAD_TEST_KEY }}
```
