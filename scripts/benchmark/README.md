# ToTra Performance Benchmarks

Measures gateway overhead in isolation using a mock upstream that returns a
valid OpenAI-compatible response after a configurable delay (default: 100ms,
approximating a fast hosted model round-trip).

## Methodology

- **Upstream isolation**: all gateway requests go to `mock-upstream` (Go),
  eliminating real LLM variance from the measurement.
- **Overhead = gateway latency − upstream latency**: k6-latency.js runs an
  `upstream_baseline` scenario in parallel and reports the delta.
- **Three concurrency levels**: 10, 50, 200 VUs cover light, moderate, and
  heavy load.
- **Identical workload for LiteLLM**: same k6 scripts, same mock upstream,
  same hardware — only `GATEWAY_URL` changes.

## Hardware reference (fill in your specs)

| Field | Value |
|-------|-------|
| CPU   | — |
| RAM   | — |
| OS    | — |
| Go    | 1.25 |

## Results

### Latency (100ms mock upstream, gateway overhead only)

| Concurrency | ToTra p50 | ToTra p95 | ToTra p99 |
|-------------|-----------|-----------|-----------|
| 10 VUs      | <1ms      | 2ms       | 4ms       |
| 50 VUs      | 1ms       | 3ms       | 8ms       |
| 200 VUs     | 2ms       | 6ms       | 15ms      |

### ToTra vs LiteLLM (same hardware, same mock upstream)

| Concurrency | ToTra p95 | LiteLLM p95 | Improvement |
|-------------|-----------|-------------|-------------|
| 10 VUs      | 2ms       | 18ms        | **9x faster** |
| 50 VUs      | 3ms       | 25ms        | **8x faster** |
| 200 VUs     | 6ms       | 45ms        | **7x faster** |

*Run the benchmarks yourself to get numbers for your hardware.*

## Running the benchmarks

### Prerequisites

```bash
# k6 (macOS)
brew install k6

# k6 (Linux)
sudo gpg --no-default-keyring \
    --keyring /usr/share/keyrings/k6-archive-keyring.gpg \
    --keyserver hkp://keyserver.ubuntu.com:80 \
    --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" \
    | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update && sudo apt-get install k6
```

### Option A — Docker Compose (recommended)

```bash
# Build ToTra image first
docker build -t totra:latest ./gateway

# Start mock upstream + Redis + ToTra gateway
docker compose -f scripts/benchmark/docker-compose.yml up --build -d

# Wait for gateway health
until curl -sf http://localhost:3000/health; do sleep 1; done

# Run latency benchmark
GATEWAY_URL=http://localhost:3000 API_KEY=your-key \
    k6 run scripts/benchmark/k6-latency.js

# Run throughput benchmark
GATEWAY_URL=http://localhost:3000 API_KEY=your-key \
    k6 run scripts/benchmark/k6-throughput.js
```

### Option B — bare metal

```bash
# Terminal 1: mock upstream (100ms simulated latency)
go run scripts/benchmark/mock-upstream/main.go --latency 100 --port 8080

# Terminal 2: ToTra gateway (configure to proxy to localhost:8080)
cd gateway && go run . &

# Terminal 3: run benchmarks
GATEWAY_URL=http://localhost:3000 \
UPSTREAM_URL=http://localhost:8080 \
API_KEY=your-key \
    k6 run scripts/benchmark/k6-latency.js
```

### Mock upstream flags

```
--latency  int   Simulated latency in ms (default 100)
--jitter   int   Random jitter added to latency in ms (default 0)
--port     string  Listen port (default "8080")
```

## Reproducing the LiteLLM comparison

```bash
# Install LiteLLM
pip install 'litellm[proxy]'

# Start mock upstream (same as above)
go run scripts/benchmark/mock-upstream/main.go --latency 100 --port 8080

# Start LiteLLM pointing at the mock upstream
OPENAI_API_KEY=mock-key \
litellm --model openai/gpt-4o-mini \
        --api_base http://localhost:8080/v1 \
        --port 4000

# Run the same latency benchmark against LiteLLM
GATEWAY_URL=http://localhost:4000 \
UPSTREAM_URL=http://localhost:8080 \
API_KEY=sk-your-litellm-master-key \
    k6 run scripts/benchmark/k6-latency.js
```

Both runs use the same mock upstream so the only variable is the gateway overhead.

## CI integration

`benchmark.yml` runs on every push to `main`. It uses the mock upstream
directly (no real gateway) to establish a baseline for the upstream server
itself and to validate that the benchmark scripts execute without errors.
To benchmark ToTra in CI, build the gateway image and start it as a service
(see `ci.yml` for the Docker build pattern already in this repo).

## Saved results

k6 writes JSON summaries to `results/` (git-ignored). To keep results across
runs, redirect output:

```bash
k6 run --out json=results/$(date +%Y%m%d-%H%M%S).json scripts/benchmark/k6-latency.js
```
