# KPI Algorithm v3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve KPI algorithm precision with four targeted fixes: small peer group Z-score fallback, GTS adaptive weights by integration level, OSS L1 session-depth signal, and multi-layer anomaly detection (3σ hard + 2σ soft).

**Architecture:** All logic changes are confined to `admin/services/kpi.go` and `admin/services/aiq.go` (pure Go math functions). One DB migration adds `anomaly_soft_flagged`. The dashboard gets a yellow badge for soft anomalies. No new API endpoints are added; existing `ComputeAIQScores` and `IsAnomalySCU` remain backward-compatible.

**Tech Stack:** Go 1.26 + Fiber v2, pgx/v5, PostgreSQL 16, React 19 + TypeScript, Tailwind v4.

---

## File Structure

| File | Change |
|------|--------|
| `admin/services/aiq.go` | Export `MinActiveDays`, add `MinPeerGroupSize = 5` |
| `admin/services/kpi.go` | Peer group routing, GTS weights, OSS L1 signal, `IsAnomaly`, struct + queries |
| `admin/services/aiq_test.go` | Small peer group tests |
| `admin/services/kpi_test.go` | GTS weight, OSS L1, anomaly tests |
| `infra/postgres/013_kpi_v3.sql` | Add `anomaly_soft_flagged BOOLEAN DEFAULT FALSE` |
| `dashboard/src/api/client.ts` | Add `anomaly_soft_flagged` to `EfficiencySnapshot` |
| `dashboard/src/pages/admin/KpiPage.tsx` | Yellow soft-anomaly badge + row highlight |

---

## Task 1: Small peer group fallback

**Problem:** When a peer group has fewer than 5 eligible users (active_days ≥ 8), Z-score normalization produces unreliable scores. Users in small groups should be normalized against a global tenant-wide pool instead.

**Files:**
- Modify: `admin/services/aiq.go`
- Modify: `admin/services/kpi.go` (lines ~314–327)
- Modify: `admin/services/aiq_test.go`

- [ ] **Step 1: Write failing tests**

Add to `admin/services/aiq_test.go`:

```go
func TestMinPeerGroupSize(t *testing.T) {
	if services.MinPeerGroupSize != 5 {
		t.Errorf("MinPeerGroupSize should be 5, got %d", services.MinPeerGroupSize)
	}
}

func TestComputeAIQScores_SmallGroupStillScored(t *testing.T) {
	// 3 users (< MinPeerGroupSize=5): when the caller routes them to a global
	// pool, ComputeAIQScores must still produce valid scores (not all -1).
	metrics := []*services.RawAIQMetrics{
		{UserID: "u1", OutputDensity: 2.0, UsageConsistency: 0.8, TaskDepth: 4.0, CostEfficiency: 100, ActiveDays: 15},
		{UserID: "u2", OutputDensity: 1.0, UsageConsistency: 0.5, TaskDepth: 2.0, CostEfficiency: 50, ActiveDays: 10},
		{UserID: "u3", OutputDensity: 0.5, UsageConsistency: 0.3, TaskDepth: 1.0, CostEfficiency: 20, ActiveDays: 9},
	}
	scores := services.ComputeAIQScores(metrics)
	for uid, sc := range scores {
		if sc < 0 {
			t.Errorf("user %s got negative AIQ %v despite active_days >= threshold", uid, sc)
		}
	}
	if scores["u1"] <= scores["u3"] {
		t.Errorf("u1 (better metrics) should score higher than u3, got u1=%v u3=%v", scores["u1"], scores["u3"])
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestMinPeerGroupSize|TestComputeAIQScores_SmallGroupStillScored" -v
```

Expected: FAIL — `services.MinPeerGroupSize` undefined.

- [ ] **Step 3: Export constants in `aiq.go`**

In `admin/services/aiq.go`, find (inside `ComputeAIQScores`, line ~93):

```go
func ComputeAIQScores(metrics []*RawAIQMetrics) map[string]float64 {
	const MinActiveDays = 8
```

Replace with (constants at package level, before the function):

```go
const (
	MinActiveDays    = 8 // minimum active days for AIQ eligibility
	MinPeerGroupSize = 5 // minimum eligible users for isolated Z-score; smaller groups use global pool
)

func ComputeAIQScores(metrics []*RawAIQMetrics) map[string]float64 {
```

- [ ] **Step 4: Update peer group routing in `RunMonthlySnapshot`**

In `admin/services/kpi.go`, find the section that builds `peerGroups` and calls `ComputeAIQScores` (lines ~314–327):

```go
	peerGroups := map[string][]*RawAIQMetrics{}
	for _, u := range users {
		pg := peerGroupOf[u.ID]
		if m, ok := rawByUser[u.ID]; ok {
			peerGroups[pg] = append(peerGroups[pg], m)
		}
	}
	aiqScores := map[string]float64{}
	for _, members := range peerGroups {
		scores := ComputeAIQScores(members)
		for uid, sc := range scores {
			aiqScores[uid] = sc
		}
	}
```

Replace with:

```go
	peerGroups := map[string][]*RawAIQMetrics{}
	for _, u := range users {
		pg := peerGroupOf[u.ID]
		if m, ok := rawByUser[u.ID]; ok {
			peerGroups[pg] = append(peerGroups[pg], m)
		}
	}

	// Count eligible (active_days >= MinActiveDays) per group.
	eligibleCount := map[string]int{}
	for pg, members := range peerGroups {
		for _, m := range members {
			if m.ActiveDays >= MinActiveDays {
				eligibleCount[pg]++
			}
		}
	}

	// Groups with < MinPeerGroupSize eligible users route to a global fallback pool.
	var globalPool []*RawAIQMetrics
	for pg, members := range peerGroups {
		if eligibleCount[pg] < MinPeerGroupSize {
			globalPool = append(globalPool, members...)
		}
	}

	// Compute AIQ: large groups in isolation; small groups via global pool.
	aiqScores := map[string]float64{}
	for pg, members := range peerGroups {
		if eligibleCount[pg] < MinPeerGroupSize {
			continue
		}
		for uid, sc := range ComputeAIQScores(members) {
			aiqScores[uid] = sc
		}
	}
	if len(globalPool) > 0 {
		for uid, sc := range ComputeAIQScores(globalPool) {
			aiqScores[uid] = sc
		}
	}
```

- [ ] **Step 5: Run tests**

```
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestMinPeerGroupSize|TestComputeAIQScores_SmallGroupStillScored" -v
```

Expected: PASS

- [ ] **Step 6: Run full service suite**

```
cd /Users/sugac.275/ToTra/admin && go test ./services/... -v 2>&1 | tail -20
```

Expected: all PASS

- [ ] **Step 7: Commit**

```bash
cd /Users/sugac.275/ToTra
git add admin/services/aiq.go admin/services/kpi.go admin/services/aiq_test.go
git commit -m "feat(kpi): small peer group fallback — <5 eligible → global pool Z-score"
```

---

## Task 2: GTS adaptive weights

**Problem:** GTS is fixed at 15% across all integration levels. At L1 where OSS is a weak signal, GTS should have more weight to incentivize adoption growth. L3 keeps 15% (OSS is dominant and reliable).

**Files:**
- Modify: `admin/services/kpi.go` (lines ~56–58, `AdaptiveWeights`)
- Modify: `admin/services/kpi_test.go`

- [ ] **Step 1: Write failing test for new weight values**

Add to `admin/services/kpi_test.go`:

```go
func TestAdaptiveWeights_v3(t *testing.T) {
	// L1: GTS↑ 0.15→0.25 (OSS unreliable, reward growth more)
	aW, oW, gW := services.AdaptiveWeights(1, true)
	assert.InDelta(t, 1.0, aW+oW+gW, 1e-9, "L1 weights must sum to 1")
	assert.InDelta(t, 0.25, gW, 1e-9, "L1 GTS should be 0.25")
	assert.InDelta(t, 0.20, oW, 1e-9, "L1 OSS should be 0.20")
	assert.InDelta(t, 0.55, aW, 1e-9, "L1 AIQ should be 0.55")

	// L2: GTS↑ 0.15→0.20, OSS and AIQ balanced
	aW2, oW2, gW2 := services.AdaptiveWeights(2, true)
	assert.InDelta(t, 1.0, aW2+oW2+gW2, 1e-9, "L2 weights must sum to 1")
	assert.InDelta(t, 0.20, gW2, 1e-9, "L2 GTS should be 0.20")
	assert.InDelta(t, 0.40, oW2, 1e-9, "L2 OSS should be 0.40")
	assert.InDelta(t, 0.40, aW2, 1e-9, "L2 AIQ should be 0.40")

	// L3: unchanged — OSS dominant, GTS minimal
	aW3, oW3, gW3 := services.AdaptiveWeights(3, true)
	assert.InDelta(t, 1.0, aW3+oW3+gW3, 1e-9, "L3 weights must sum to 1")
	assert.InDelta(t, 0.15, gW3, 1e-9, "L3 GTS should be 0.15 (unchanged)")
	assert.InDelta(t, 0.50, oW3, 1e-9, "L3 OSS should be 0.50 (unchanged)")

	// No-GTS: GTS weight (0.25) redistributed to AIQ → AIQ = 0.55+0.25 = 0.80
	aW4, oW4, gW4 := services.AdaptiveWeights(1, false)
	assert.InDelta(t, 1.0, aW4+oW4+gW4, 1e-9, "no-GTS weights must sum to 1")
	assert.Equal(t, 0.0, gW4)
	assert.InDelta(t, 0.80, aW4, 1e-9, "no-GTS L1 AIQ = 0.55+0.25")
}
```

- [ ] **Step 2: Run to confirm failure**

```
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestAdaptiveWeights_v3" -v
```

Expected: FAIL — current L1 GTS is 0.15, test expects 0.25.

- [ ] **Step 3: Update the weight table in `AdaptiveWeights`**

In `admin/services/kpi.go`, find (lines ~56–58):

```go
	base := map[int]w{
		1: {0.60, 0.25, 0.15},
		2: {0.45, 0.40, 0.15},
		3: {0.35, 0.50, 0.15},
	}
```

Replace with:

```go
	base := map[int]w{
		1: {0.55, 0.20, 0.25}, // L1: OSS weak (no webhooks), GTS↑ incentivizes adoption
		2: {0.40, 0.40, 0.20}, // L2: balanced, GTS↑ for growth awareness
		3: {0.35, 0.50, 0.15}, // L3: OSS dominant (reliable signal), GTS minimal
	}
```

- [ ] **Step 4: Run tests**

```
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestAdaptiveWeights_v3|TestAdaptiveWeights" -v
```

Expected: all PASS (existing `TestAdaptiveWeights` only checks sum=1, still passes)

- [ ] **Step 5: Run full service suite**

```
cd /Users/sugac.275/ToTra/admin && go test ./services/... -v 2>&1 | tail -20
```

Expected: all PASS

- [ ] **Step 6: Commit**

```bash
cd /Users/sugac.275/ToTra
git add admin/services/kpi.go admin/services/kpi_test.go
git commit -m "feat(kpi): GTS adaptive weights — L1 25%, L2 20%, L3 15% (was flat 15%)"
```

---

## Task 3: OSS L1 session-depth signal

**Problem:** The L1 OSS formula `log(tokens)/log(peer_median)` measures only token volume — it can't distinguish deep multi-turn work from shallow Q&A, and rewards verbosity over usage quality.

**Replacement:** `0.6 × (multi_turn_ratio / peer_max_ratio) + 0.4 × (log(sessions+1) / log(peer_max_sessions+1))`
- 60% depth: what fraction of conversations had ≥ 3 turns (normalized vs peer max)
- 40% volume: relative session count (log-scaled)

**Files:**
- Modify: `admin/services/kpi.go` (replace `ossLevelFallback` + wire `ossLevelOne`)
- Modify: `admin/services/kpi_test.go`

- [ ] **Step 1: Write failing tests for `ComputeOSSLevelOne`**

Add to `admin/services/kpi_test.go`:

```go
func TestComputeOSSLevelOne_Basic(t *testing.T) {
	data := map[string]services.SessionDepthData{
		"u1": {TotalSessions: 10, MultiTurnSessions: 8}, // 80% deep, highest volume
		"u2": {TotalSessions: 5, MultiTurnSessions: 1},  // 20% deep, medium volume
		"u3": {TotalSessions: 8, MultiTurnSessions: 0},  // 0% deep, no multi-turn
	}
	scores := services.ComputeOSSLevelOne(data)
	if scores["u1"] <= scores["u2"] {
		t.Errorf("u1 (deeper + more sessions) should beat u2, got u1=%v u2=%v", scores["u1"], scores["u2"])
	}
	if scores["u2"] <= scores["u3"] {
		t.Errorf("u2 (some multi-turn) should beat u3 (none), got u2=%v u3=%v", scores["u2"], scores["u3"])
	}
	for uid, sc := range scores {
		if sc < 0 || sc > 1+1e-9 {
			t.Errorf("user %s: score %v out of [0,1]", uid, sc)
		}
	}
}

func TestComputeOSSLevelOne_Empty(t *testing.T) {
	scores := services.ComputeOSSLevelOne(map[string]services.SessionDepthData{})
	if len(scores) != 0 {
		t.Errorf("empty input should return empty map, got %v", scores)
	}
}

func TestComputeOSSLevelOne_SingleUser(t *testing.T) {
	data := map[string]services.SessionDepthData{
		"u1": {TotalSessions: 5, MultiTurnSessions: 3},
	}
	scores := services.ComputeOSSLevelOne(data)
	// Single user: normalized depth = 1.0, normalized volume = 1.0 → score = 1.0
	if math.Abs(scores["u1"]-1.0) > 1e-9 {
		t.Errorf("single user should get 1.0, got %v", scores["u1"])
	}
}

func TestComputeOSSLevelOne_ZeroSessions(t *testing.T) {
	data := map[string]services.SessionDepthData{
		"u1": {TotalSessions: 0, MultiTurnSessions: 0},
	}
	scores := services.ComputeOSSLevelOne(data)
	if scores["u1"] != 0 {
		t.Errorf("zero sessions should give score 0, got %v", scores["u1"])
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestComputeOSSLevelOne" -v
```

Expected: FAIL — `services.SessionDepthData` and `services.ComputeOSSLevelOne` undefined.

- [ ] **Step 3: Replace `ossLevelFallback` with new implementation in `kpi.go`**

In `admin/services/kpi.go`, find and replace the entire `ossLevelFallback` method (lines ~118–150):

```go
// SessionDepthData holds raw session counts for one user (exported for unit tests).
type SessionDepthData struct {
	TotalSessions    int
	MultiTurnSessions int // conversations with >= 3 turns
}

// ComputeOSSLevelOne computes OSS for L1 (no-webhook) users using session-depth signal.
// score = 0.6 × (depth_ratio / peer_max_depth) + 0.4 × (log(sessions+1) / log(peer_max_sessions+1))
func ComputeOSSLevelOne(data map[string]SessionDepthData) map[string]float64 {
	if len(data) == 0 {
		return map[string]float64{}
	}
	var maxSessions float64
	depthRatios := make(map[string]float64, len(data))
	for uid, d := range data {
		if d.TotalSessions > 0 {
			depthRatios[uid] = float64(d.MultiTurnSessions) / float64(d.TotalSessions)
		}
		if float64(d.TotalSessions) > maxSessions {
			maxSessions = float64(d.TotalSessions)
		}
	}
	var maxDepth float64
	for _, dr := range depthRatios {
		if dr > maxDepth {
			maxDepth = dr
		}
	}
	result := make(map[string]float64, len(data))
	for uid, d := range data {
		depthNorm := 0.0
		if maxDepth > 0 {
			depthNorm = depthRatios[uid] / maxDepth
		}
		volumeFactor := 0.0
		if maxSessions > 0 {
			volumeFactor = math.Log(float64(d.TotalSessions)+1) / math.Log(maxSessions+1)
		}
		result[uid] = 0.6*depthNorm + 0.4*volumeFactor
	}
	return result
}

// ossLevelOne fetches session-depth data from the DB and computes L1 OSS scores.
func (s *KPIService) ossLevelOne(ctx context.Context, tenantID, yearMonth string) (map[string]float64, error) {
	rows, err := s.pool.Query(ctx, `
		WITH conv AS (
			SELECT user_id, conversation_id, COUNT(*) AS turns
			FROM usage_records
			WHERE tenant_id=$1 AND to_char(request_at,'YYYY-MM')=$2
			  AND conversation_id IS NOT NULL
			GROUP BY user_id, conversation_id
		)
		SELECT user_id,
		       COUNT(*)                            AS total_sessions,
		       COUNT(*) FILTER (WHERE turns >= 3) AS multi_turn_sessions
		FROM conv GROUP BY user_id`, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	data := map[string]SessionDepthData{}
	for rows.Next() {
		var uid string
		var d SessionDepthData
		if err := rows.Scan(&uid, &d.TotalSessions, &d.MultiTurnSessions); err != nil {
			return nil, err
		}
		data[uid] = d
	}
	return ComputeOSSLevelOne(data), nil
}
```

- [ ] **Step 4: Update the call site in `RunMonthlySnapshot`**

In `admin/services/kpi.go`, find (lines ~332–336):

```go
	if level == 1 {
		ossScores, err = s.ossLevelFallback(ctx, tenantID, yearMonth)
		if err != nil {
			return fmt.Errorf("oss fallback: %w", err)
		}
```

Replace with:

```go
	if level == 1 {
		ossScores, err = s.ossLevelOne(ctx, tenantID, yearMonth)
		if err != nil {
			return fmt.Errorf("oss level-1: %w", err)
		}
```

- [ ] **Step 5: Run tests**

```
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestComputeOSSLevelOne" -v
```

Expected: all PASS

- [ ] **Step 6: Build check**

```
cd /Users/sugac.275/ToTra/admin && go build ./... 2>&1
```

Expected: no errors

- [ ] **Step 7: Run full service suite**

```
cd /Users/sugac.275/ToTra/admin && go test ./services/... -v 2>&1 | tail -20
```

Expected: all PASS

- [ ] **Step 8: Commit**

```bash
cd /Users/sugac.275/ToTra
git add admin/services/kpi.go admin/services/kpi_test.go
git commit -m "feat(kpi): replace OSS L1 with session-depth signal (multi-turn 60% + volume 40%)"
```

---

## Task 4: Multi-layer anomaly — backend

**Problem:** The current 3σ single-month hard flag is too coarse. A user inflating 2.9σ every month never triggers it. Adding a 2σ soft flag provides an early warning without zeroing scores.

- **Hard flag** (`anomaly_flagged`): `currentSCU > mean + 3σ` — score → 0, red badge
- **Soft flag** (`anomaly_soft_flagged`): `currentSCU > mean + 2σ` (and not hard) — warning badge only, score unaffected

**Files:**
- Create: `infra/postgres/013_kpi_v3.sql`
- Modify: `admin/services/kpi.go` (IsAnomaly, struct, snapshotData, RunMonthlySnapshot, scanSnapshots, 4 queries)
- Modify: `admin/services/kpi_test.go`

- [ ] **Step 1: Write failing tests for `IsAnomaly`**

Add to `admin/services/kpi_test.go`:

```go
func TestIsAnomaly(t *testing.T) {
	// Not enough prior data → both false
	hard, soft := services.IsAnomaly(1000, []float64{500})
	assert.False(t, hard)
	assert.False(t, soft)

	// Normal usage (well within 2σ) → both false
	// [100,105,110]: mean=105, std=5; 2σ threshold=115
	hard2, soft2 := services.IsAnomaly(112, []float64{100, 105, 110})
	assert.False(t, hard2, "112 is within 2σ of [100,105,110]")
	assert.False(t, soft2)

	// Soft anomaly: between 2σ and 3σ
	// [95,100,105]: mean=100, std=5; 2σ=110, 3σ=115
	hard3, soft3 := services.IsAnomaly(112, []float64{95, 100, 105})
	assert.False(t, hard3, "112 should not hard-flag (< 3σ=115)")
	assert.True(t, soft3, "112 should soft-flag (> 2σ=110)")

	// Hard anomaly: >3σ — soft should be false (mutually exclusive)
	hard4, soft4 := services.IsAnomaly(1000, []float64{100, 110, 105})
	assert.True(t, hard4, "1000 is a hard anomaly")
	assert.False(t, soft4, "hard anomaly should not also set soft")

	// std=0 (identical prior months) → both false
	hard5, soft5 := services.IsAnomaly(200, []float64{100, 100, 100})
	assert.False(t, hard5)
	assert.False(t, soft5)
}

func TestIsAnomalySCU_BackwardCompat(t *testing.T) {
	// IsAnomalySCU is now a wrapper; verify it still works as before
	assert.False(t, services.IsAnomalySCU(120, []float64{100, 110, 105}))
	assert.True(t, services.IsAnomalySCU(1000, []float64{100, 110, 105}))
}
```

- [ ] **Step 2: Run to confirm failure**

```
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestIsAnomaly|TestIsAnomalySCU_BackwardCompat" -v
```

Expected: FAIL — `services.IsAnomaly` undefined.

- [ ] **Step 3: Create migration**

Create `infra/postgres/013_kpi_v3.sql`:

```sql
ALTER TABLE efficiency_snapshots
    ADD COLUMN IF NOT EXISTS anomaly_soft_flagged BOOLEAN NOT NULL DEFAULT FALSE;
```

- [ ] **Step 4: Replace `IsAnomalySCU` with `IsAnomaly` + wrapper in `kpi.go`**

In `admin/services/kpi.go`, find and replace the entire `IsAnomalySCU` function (lines ~96–115):

```go
// IsAnomaly returns (hard, soft) anomaly flags based on personal SCU baseline.
// Hard: currentSCU > mean(prior) + 3σ — score zeroed in final calc.
// Soft: currentSCU > mean(prior) + 2σ (exclusive with hard) — warning only.
// Both require ≥2 prior months; returns (false, false) otherwise.
func IsAnomaly(currentSCU float64, priorSCUs []float64) (hard bool, soft bool) {
	if len(priorSCUs) < 2 {
		return false, false
	}
	var sum float64
	for _, s := range priorSCUs {
		sum += s
	}
	mean := sum / float64(len(priorSCUs))
	var variance float64
	for _, s := range priorSCUs {
		variance += (s - mean) * (s - mean)
	}
	variance /= float64(len(priorSCUs) - 1)
	std := math.Sqrt(variance)
	if std == 0 {
		return false, false
	}
	hard = currentSCU > mean+3*std
	soft = !hard && currentSCU > mean+2*std
	return
}

// IsAnomalySCU is kept for backward compatibility; prefer IsAnomaly.
func IsAnomalySCU(currentSCU float64, priorSCUs []float64) bool {
	hard, _ := IsAnomaly(currentSCU, priorSCUs)
	return hard
}
```

- [ ] **Step 5: Add `AnomalySoftFlagged` to `EfficiencySnapshot` struct**

In `admin/services/kpi.go`, find the `EfficiencySnapshot` struct (lines ~13–30) and replace it:

```go
type EfficiencySnapshot struct {
	ID                 string    `json:"id"`
	UserID             string    `json:"user_id"`
	UserName           string    `json:"user_name"`
	YearMonth          string    `json:"year_month"`
	TotalSCU           float64   `json:"total_scu"`
	TotalOutputWeight  float64   `json:"total_output_weight"`
	EfficiencyScore    float64   `json:"efficiency_score"`
	AIQScore           float64   `json:"aiq_score"`
	OSSScore           float64   `json:"oss_score"`
	GTSScore           float64   `json:"gts_score"`
	IntegrationLevel   int       `json:"integration_level"`
	AnomalyFlagged     bool      `json:"anomaly_flagged"`
	AnomalySoftFlagged bool      `json:"anomaly_soft_flagged"`
	PeerGroup          string    `json:"peer_group"`
	Rank               int       `json:"rank"`
	PeerCount          int       `json:"peer_count"`
	SnapshotAt         time.Time `json:"snapshot_at"`
}
```

- [ ] **Step 6: Update `snapshotData` local struct in `RunMonthlySnapshot`**

Find the local `snapshotData` struct inside `RunMonthlySnapshot` (lines ~346–357) and add `AnomalySoftFlagged`:

```go
	type snapshotData struct {
		userRow
		TotalSCU           float64
		TotalOW            float64
		AIQ                float64
		OSS                float64
		GTS                float64
		HasGTS             bool
		PeerGroup          string
		AnomalyFlagged     bool
		AnomalySoftFlagged bool
	}
```

- [ ] **Step 7: Update anomaly detection in the snapshot building loop**

Find (lines ~378–391):

```go
		priorSCUs, _ := s.getPriorSCUs(ctx, tenantID, u.ID, yearMonth)
		anomalyFlagged := IsAnomalySCU(totalSCU, priorSCUs)

		snapshots = append(snapshots, snapshotData{
			userRow:        u,
			TotalSCU:       totalSCU,
			TotalOW:        totalOW,
			AIQ:            aiq,
			OSS:            oss,
			GTS:            gts,
			HasGTS:         hasGTS,
			PeerGroup:      peerGroupOf[u.ID],
			AnomalyFlagged: anomalyFlagged,
		})
```

Replace with:

```go
		priorSCUs, _ := s.getPriorSCUs(ctx, tenantID, u.ID, yearMonth)
		hardAnomaly, softAnomaly := IsAnomaly(totalSCU, priorSCUs)

		snapshots = append(snapshots, snapshotData{
			userRow:            u,
			TotalSCU:           totalSCU,
			TotalOW:            totalOW,
			AIQ:                aiq,
			OSS:                oss,
			GTS:                gts,
			HasGTS:             hasGTS,
			PeerGroup:          peerGroupOf[u.ID],
			AnomalyFlagged:     hardAnomaly,
			AnomalySoftFlagged: softAnomaly,
		})
```

- [ ] **Step 8: Update the upsert query**

Find the upsert SQL (lines ~427–447) and add `anomaly_soft_flagged` as the 17th parameter:

```go
		_, err := s.pool.Exec(ctx, `
			INSERT INTO efficiency_snapshots
			(id,tenant_id,user_id,year_month,total_scu,total_output_weight,
			 efficiency_score,aiq_score,oss_score,gts_score,integration_level,
			 peer_group,rank,peer_count,snapshot_at,anomaly_flagged,anomaly_soft_flagged)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
			ON CONFLICT (tenant_id,user_id,year_month) DO UPDATE
			SET total_scu=$5,total_output_weight=$6,efficiency_score=$7,
			    aiq_score=$8,oss_score=$9,gts_score=$10,integration_level=$11,
			    peer_group=$12,rank=$13,peer_count=$14,snapshot_at=$15,
			    anomaly_flagged=$16,anomaly_soft_flagged=$17`,
			uuid.New().String(), tenantID, snap.ID, yearMonth,
			snap.TotalSCU, snap.TotalOW, sc,
			aiq, snap.OSS, snap.GTS, level,
			snap.PeerGroup, ranks[i], len(groups[snap.PeerGroup]), snapshotAt,
			snap.AnomalyFlagged, snap.AnomalySoftFlagged,
		)
```

- [ ] **Step 9: Update all 4 SELECT queries to include `anomaly_soft_flagged`**

**`GetSnapshots`** (lines ~453–462): add `COALESCE(es.anomaly_soft_flagged, false)` as the 17th column:

```go
	rows, err := s.pool.Query(ctx, `
		SELECT es.id, es.user_id, u.name, es.year_month,
		       es.total_scu, es.total_output_weight, es.efficiency_score,
		       COALESCE(es.aiq_score,0), COALESCE(es.oss_score,0), COALESCE(es.gts_score,0),
		       COALESCE(es.integration_level,1),
		       es.peer_group, es.rank, es.peer_count, es.snapshot_at,
		       COALESCE(es.anomaly_flagged, false),
		       COALESCE(es.anomaly_soft_flagged, false)
		FROM efficiency_snapshots es JOIN users u ON es.user_id=u.id
		WHERE es.tenant_id=$1 AND es.year_month=$2
		ORDER BY es.peer_group, es.rank`, tenantID, yearMonth)
```

**`GetUserHistory`** (lines ~472–482):

```go
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, '' AS user_name, year_month,
		       total_scu, total_output_weight, efficiency_score,
		       COALESCE(aiq_score,0), COALESCE(oss_score,0), COALESCE(gts_score,0),
		       COALESCE(integration_level,1),
		       peer_group, rank, peer_count, snapshot_at,
		       COALESCE(anomaly_flagged, false),
		       COALESCE(anomaly_soft_flagged, false)
		FROM efficiency_snapshots
		WHERE tenant_id=$1 AND user_id=$2
		ORDER BY year_month DESC LIMIT 12`, tenantID, userID)
```

**`GetMySnapshots`** (lines ~506–516):

```go
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, '' AS user_name, year_month,
		       total_scu, total_output_weight, efficiency_score,
		       COALESCE(aiq_score,0), COALESCE(oss_score,0), COALESCE(gts_score,0),
		       COALESCE(integration_level,1),
		       peer_group, rank, peer_count, snapshot_at,
		       COALESCE(anomaly_flagged, false),
		       COALESCE(anomaly_soft_flagged, false)
		FROM efficiency_snapshots WHERE user_id=$1
		ORDER BY year_month DESC LIMIT 12`, userID)
```

**`GetAnomalies`** (lines ~524–534):

```go
	rows, err := s.pool.Query(ctx, `
		SELECT es.id, es.user_id, u.name, es.year_month,
		       es.total_scu, es.total_output_weight, es.efficiency_score,
		       COALESCE(es.aiq_score,0), COALESCE(es.oss_score,0), COALESCE(es.gts_score,0),
		       COALESCE(es.integration_level,1),
		       es.peer_group, es.rank, es.peer_count, es.snapshot_at,
		       COALESCE(es.anomaly_flagged,false),
		       COALESCE(es.anomaly_soft_flagged,false)
		FROM efficiency_snapshots es JOIN users u ON es.user_id=u.id
		WHERE es.tenant_id=$1 AND es.year_month=$2 AND es.anomaly_flagged=true
		ORDER BY es.total_scu DESC`, tenantID, yearMonth)
```

- [ ] **Step 10: Update `scanSnapshots` to scan 17 columns**

In `admin/services/kpi.go`, find `scanSnapshots` (lines ~541–557) and add `&s.AnomalySoftFlagged`:

```go
func scanSnapshots(rows interface {
	Next() bool
	Scan(...any) error
}) ([]*EfficiencySnapshot, error) {
	var results []*EfficiencySnapshot
	for rows.Next() {
		s := &EfficiencySnapshot{}
		if err := rows.Scan(&s.ID, &s.UserID, &s.UserName, &s.YearMonth,
			&s.TotalSCU, &s.TotalOutputWeight, &s.EfficiencyScore,
			&s.AIQScore, &s.OSSScore, &s.GTSScore, &s.IntegrationLevel,
			&s.PeerGroup, &s.Rank, &s.PeerCount, &s.SnapshotAt, &s.AnomalyFlagged,
			&s.AnomalySoftFlagged); err != nil {
			return nil, err
		}
		results = append(results, s)
	}
	return results, nil
}
```

- [ ] **Step 11: Run tests**

```
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run "TestIsAnomaly|TestIsAnomalySCU_BackwardCompat" -v
```

Expected: PASS

- [ ] **Step 12: Build check**

```
cd /Users/sugac.275/ToTra/admin && go build ./... 2>&1
```

Expected: no errors

- [ ] **Step 13: Run full service suite**

```
cd /Users/sugac.275/ToTra/admin && go test ./services/... -v 2>&1 | tail -30
```

Expected: all PASS

- [ ] **Step 14: Commit**

```bash
cd /Users/sugac.275/ToTra
git add infra/postgres/013_kpi_v3.sql admin/services/kpi.go admin/services/kpi_test.go
git commit -m "feat(kpi): multi-layer anomaly — 3σ hard (score→0) + 2σ soft (warning badge)"
```

---

## Task 5: Dashboard soft anomaly badge

**Files:**
- Modify: `dashboard/src/api/client.ts` (lines ~128–145)
- Modify: `dashboard/src/pages/admin/KpiPage.tsx` (lines ~147–155)

- [ ] **Step 1: Add `anomaly_soft_flagged` to `EfficiencySnapshot` interface**

In `dashboard/src/api/client.ts`, find the `EfficiencySnapshot` interface (lines ~128–145) and replace it:

```typescript
export interface EfficiencySnapshot {
  id: string;
  user_id: string;
  user_name: string;
  year_month: string;
  total_scu: number;
  total_output_weight: number;
  efficiency_score: number;
  aiq_score: number;
  oss_score: number;
  gts_score: number;
  integration_level: number;
  anomaly_flagged: boolean;
  anomaly_soft_flagged: boolean;
  peer_group: string;
  rank: number;
  peer_count: number;
  snapshot_at: string;
}
```

- [ ] **Step 2: Update row background highlight in `KpiPage.tsx`**

In `dashboard/src/pages/admin/KpiPage.tsx`, find the `<tr>` row className (line ~147):

```tsx
className={`border-b border-zinc-800/50 cursor-pointer transition-colors ${s.anomaly_flagged ? "bg-red-950/30 hover:bg-red-900/30" : "hover:bg-zinc-800/30"}`}
```

Replace with:

```tsx
className={`border-b border-zinc-800/50 cursor-pointer transition-colors ${
  s.anomaly_flagged
    ? "bg-red-950/30 hover:bg-red-900/30"
    : s.anomaly_soft_flagged
    ? "bg-yellow-950/20 hover:bg-yellow-900/20"
    : "hover:bg-zinc-800/30"
}`}
```

- [ ] **Step 3: Add soft anomaly badge in the name cell**

In `dashboard/src/pages/admin/KpiPage.tsx`, find the name cell (lines ~149–155):

```tsx
<td className="py-2">
  <span>{s.user_name}</span>
  {s.anomaly_flagged && (
    <Badge variant="destructive" className="ml-2 text-xs">⚠ Review</Badge>
  )}
  <span className="ml-2 text-zinc-600 text-xs">
    {expandedUser === s.user_id ? "▲" : "▼"}
  </span>
</td>
```

Replace with:

```tsx
<td className="py-2">
  <span>{s.user_name}</span>
  {s.anomaly_flagged && (
    <Badge variant="destructive" className="ml-2 text-xs">⚠ Review</Badge>
  )}
  {s.anomaly_soft_flagged && !s.anomaly_flagged && (
    <Badge variant="outline" className="ml-2 text-xs text-yellow-400 border-yellow-400/50">⚠ Watch</Badge>
  )}
  <span className="ml-2 text-zinc-600 text-xs">
    {expandedUser === s.user_id ? "▲" : "▼"}
  </span>
</td>
```

- [ ] **Step 4: TypeScript build check**

```
cd /Users/sugac.275/ToTra/dashboard && npm run build 2>&1 | tail -20
```

Expected: no type errors, build succeeds

- [ ] **Step 5: Commit**

```bash
cd /Users/sugac.275/ToTra
git add dashboard/src/api/client.ts dashboard/src/pages/admin/KpiPage.tsx
git commit -m "feat(dashboard): yellow soft-anomaly badge on KPI leaderboard"
```
