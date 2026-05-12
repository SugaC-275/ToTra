package services_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestEfficiencyScore(t *testing.T) {
	// 10 output weight, 100 SCU → 10/log(101) ≈ 2.17
	score := services.ComputeEfficiencyScore(10, 100)
	assert.InDelta(t, 10.0/math.Log(101), score, 0.001)
}

func TestEfficiencyScoreZeroSCU(t *testing.T) {
	// Zero SCU → score 0 (no AI usage)
	score := services.ComputeEfficiencyScore(5, 0)
	assert.Equal(t, 0.0, score)
}

func TestEfficiencyScoreNoOutput(t *testing.T) {
	score := services.ComputeEfficiencyScore(0, 100)
	assert.Equal(t, 0.0, score)
}

func TestIsNewEmployee(t *testing.T) {
	assert.True(t, services.IsNewEmployee(45))   // 45 days old
	assert.False(t, services.IsNewEmployee(91))  // 91 days old → not new
	assert.True(t, services.IsNewEmployee(89))   // 89 days → still new
	assert.False(t, services.IsNewEmployee(90))  // exactly 90 → NOT new (< 90 rule)
}

func TestCohortGroup(t *testing.T) {
	assert.Equal(t, "cohort_2026-03", services.CohortGroup("2026-03"))
}

func TestComputeGTS_NoHistory(t *testing.T) {
	gts, has := services.ComputeGTS(nil)
	if has {
		t.Error("expected hasGTS=false with no history")
	}
	if gts != 0 {
		t.Errorf("expected GTS=0, got %v", gts)
	}
}

func TestComputeGTS_OneMonth(t *testing.T) {
	history := []float64{80}
	gts, has := services.ComputeGTS(history)
	if has {
		t.Error("need ≥2 prior months for GTS; expected hasGTS=false")
	}
	_ = gts
}

func TestComputeGTS_ThreeMonths(t *testing.T) {
	history := []float64{90, 80, 70, 60}
	_, has := services.ComputeGTS(history[1:])
	if !has {
		t.Error("expected hasGTS=true with 3 prior months")
	}
}

func TestDetectIntegrationLevel(t *testing.T) {
	if services.DetectIntegrationLevel(0) != 1 { t.Error("0 webhooks → level 1") }
	if services.DetectIntegrationLevel(1) != 2 { t.Error("1 webhook → level 2") }
	if services.DetectIntegrationLevel(2) != 3 { t.Error("2 webhooks → level 3") }
	if services.DetectIntegrationLevel(5) != 3 { t.Error("5 webhooks → level 3") }
}

func TestAdaptiveWeights(t *testing.T) {
	aW, oW, gW := services.AdaptiveWeights(3, true)
	if math.Abs(aW+oW+gW-1.0) > 1e-9 { t.Errorf("weights must sum to 1, got %v+%v+%v", aW, oW, gW) }
	aW2, oW2, gW2 := services.AdaptiveWeights(1, false)
	if math.Abs(aW2+oW2+gW2-1.0) > 1e-9 { t.Errorf("weights (level1,noGTS) must sum to 1") }
	_ = gW2
}

func TestIsAnomalySCU(t *testing.T) {
	// Not enough prior data
	if services.IsAnomalySCU(1000, []float64{500}) {
		t.Error("should not flag with < 2 prior months")
	}
	// Normal usage — not anomalous
	if services.IsAnomalySCU(120, []float64{100, 110, 105}) {
		t.Error("120 is within 3σ of [100,110,105]")
	}
	// Anomalous usage — 10x spike
	if !services.IsAnomalySCU(1000, []float64{100, 110, 105}) {
		t.Error("1000 should be flagged vs [100,110,105]")
	}
	// std=0 case (identical prior months) — should not flag
	if services.IsAnomalySCU(200, []float64{100, 100, 100}) {
		t.Error("should not flag when std=0")
	}
}

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
	// Inactive user (zero sessions) in a group with an active peer:
	// inactive user should score 0, active user should score 1.0 (peer max).
	data := map[string]services.SessionDepthData{
		"active":   {TotalSessions: 8, MultiTurnSessions: 4},
		"inactive": {TotalSessions: 0, MultiTurnSessions: 0},
	}
	scores := services.ComputeOSSLevelOne(data)
	if scores["inactive"] != 0 {
		t.Errorf("inactive user should score 0, got %v", scores["inactive"])
	}
	if math.Abs(scores["active"]-1.0) > 1e-9 {
		t.Errorf("active user (peer max) should score 1.0, got %v", scores["active"])
	}
}

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
