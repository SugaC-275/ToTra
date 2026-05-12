package services

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EfficiencySnapshot struct {
	ID                string    `json:"id"`
	UserID            string    `json:"user_id"`
	UserName          string    `json:"user_name"`
	YearMonth         string    `json:"year_month"`
	TotalSCU          float64   `json:"total_scu"`
	TotalOutputWeight float64   `json:"total_output_weight"`
	EfficiencyScore   float64   `json:"efficiency_score"`
	AIQScore          float64   `json:"aiq_score"`
	OSSScore          float64   `json:"oss_score"`
	GTSScore          float64   `json:"gts_score"`
	IntegrationLevel  int       `json:"integration_level"`
	AnomalyFlagged    bool      `json:"anomaly_flagged"`
	PeerGroup         string    `json:"peer_group"`
	Rank              int       `json:"rank"`
	PeerCount         int       `json:"peer_count"`
	SnapshotAt        time.Time `json:"snapshot_at"`
}

type KPIService struct {
	pool   *pgxpool.Pool
	aiqSvc *AIQService
}

func NewKPIService(pool *pgxpool.Pool) *KPIService {
	return &KPIService{pool: pool, aiqSvc: NewAIQService(pool)}
}

// DetectIntegrationLevel returns 1, 2, or 3 based on active webhook count.
func DetectIntegrationLevel(activeWebhooks int) int {
	if activeWebhooks >= 2 {
		return 3
	}
	if activeWebhooks == 1 {
		return 2
	}
	return 1
}

// AdaptiveWeights returns (aiqWeight, ossWeight, gtsWeight) summing to 1.0.
// If hasGTS is false, the GTS weight is redistributed to AIQ.
func AdaptiveWeights(level int, hasGTS bool) (float64, float64, float64) {
	type w struct{ a, o, g float64 }
	base := map[int]w{
		1: {0.55, 0.20, 0.25}, // L1: OSS weak (no webhooks), GTS↑ incentivizes adoption
		2: {0.40, 0.40, 0.20}, // L2: balanced, GTS↑ for growth awareness
		3: {0.35, 0.50, 0.15}, // L3: OSS dominant (reliable signal), GTS minimal
	}
	b, ok := base[level]
	if !ok {
		b = base[1]
	}
	if !hasGTS {
		return b.a + b.g, b.o, 0
	}
	return b.a, b.o, b.g
}

// ComputeGTS computes the Growth Trajectory Score from prior months' scores.
// priorScores[0] = t-1 (most recent prior), [1] = t-2, [2] = t-3.
// Returns (gts, hasGTS). hasGTS is false if < 2 prior months available.
func ComputeGTS(priorScores []float64) (float64, bool) {
	if len(priorScores) < 2 {
		return 0, false
	}
	weights := []float64{0.50, 0.33, 0.17}
	var gts, wSum float64
	for i := 0; i < len(priorScores)-1 && i < 3; i++ {
		cur := priorScores[i]
		prev := priorScores[i+1]
		mom := (cur - prev) / math.Max(prev, 0.01)
		mom = math.Max(-1, math.Min(2, mom))
		gts += weights[i] * mom
		wSum += weights[i]
	}
	if wSum > 0 {
		gts /= wSum
	}
	return gts, true
}

// IsAnomalySCU returns true if currentSCU exceeds personal 3-month mean+3σ.
// Requires at least 2 prior months of data to establish a baseline.
func IsAnomalySCU(currentSCU float64, priorSCUs []float64) bool {
	if len(priorSCUs) < 2 {
		return false
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
		return false
	}
	return currentSCU > mean+3*std
}

// SessionDepthData holds raw session counts for one user (exported for unit tests).
type SessionDepthData struct {
	TotalSessions     int
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

type outputEventRow struct {
	UserID         string
	Platform       string
	EventType      string
	Weight         float64
	LinesChanged   int
	CycleTimeHours float64
	ReopenedCount  int
}

// ossFromEvents computes OSS for users with output events (level 2 or 3).
func ossFromEvents(events []*outputEventRow, level int) map[string]float64 {
	totals := map[string]float64{}
	for _, e := range events {
		w := e.Weight
		if level == 3 {
			if e.Platform == "github" && e.EventType == "pr_merged" {
				lm := 1.0 + math.Min(float64(e.LinesChanged)/500.0, 1.0)
				rm := 1.0 / (1.0 + float64(e.ReopenedCount))
				w *= lm * rm
			}
			if e.Platform == "jira" && e.EventType == "issue_closed" && e.CycleTimeHours > 0 {
				days := e.CycleTimeHours / 24.0
				mult := math.Max(0.5, 1.0-0.1*math.Max(0, days-7))
				w *= mult
			}
		}
		totals[e.UserID] += w
	}
	return totals
}

func (s *KPIService) getOutputEvents(ctx context.Context, tenantID, yearMonth string) ([]*outputEventRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(user_id::text,''), platform, event_type, weight,
			COALESCE(lines_changed,0), COALESCE(cycle_time_hours,0), COALESCE(reopened_count,0)
		FROM output_events
		WHERE tenant_id=$1
			AND to_char(occurred_at,'YYYY-MM')=$2
			AND user_id IS NOT NULL`, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*outputEventRow
	for rows.Next() {
		e := &outputEventRow{}
		rows.Scan(&e.UserID, &e.Platform, &e.EventType, &e.Weight,
			&e.LinesChanged, &e.CycleTimeHours, &e.ReopenedCount)
		result = append(result, e)
	}
	return result, nil
}

func (s *KPIService) getActiveWebhookCount(ctx context.Context, tenantID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(DISTINCT platform) FROM webhook_configs WHERE tenant_id=$1 AND is_active=true`,
		tenantID,
	).Scan(&n)
	return n, err
}

func (s *KPIService) getPriorScores(ctx context.Context, tenantID, userID, currentMonth string) ([]float64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT efficiency_score FROM efficiency_snapshots
		WHERE tenant_id=$1 AND user_id=$2 AND year_month < $3
		ORDER BY year_month DESC LIMIT 3`, tenantID, userID, currentMonth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var scores []float64
	for rows.Next() {
		var sc float64
		rows.Scan(&sc)
		scores = append(scores, sc)
	}
	return scores, nil
}

func (s *KPIService) getPriorSCUs(ctx context.Context, tenantID, userID, currentMonth string) ([]float64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(SUM(scu_cost), 0)
		FROM usage_records
		WHERE tenant_id=$1 AND user_id=$2
		  AND to_char(request_at,'YYYY-MM') < $3
		GROUP BY to_char(request_at,'YYYY-MM')
		ORDER BY to_char(request_at,'YYYY-MM') DESC
		LIMIT 3`, tenantID, userID, currentMonth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []float64
	for rows.Next() {
		var scu float64
		rows.Scan(&scu)
		result = append(result, scu)
	}
	return result, nil
}

// RunMonthlySnapshot computes v2 efficiency snapshots for all active users in a tenant.
func (s *KPIService) RunMonthlySnapshot(ctx context.Context, tenantID, yearMonth string) error {
	// 1. Detect integration level
	webhookCount, err := s.getActiveWebhookCount(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("webhook count: %w", err)
	}
	level := DetectIntegrationLevel(webhookCount)

	// 2. Get all active users with their peer group info
	type userRow struct {
		ID        string
		Name      string
		JobRole   string
		Role      string
		CreatedAt time.Time
	}
	urows, err := s.pool.Query(ctx,
		`SELECT id, name, COALESCE(job_role,''), role, created_at
		 FROM users WHERE tenant_id=$1 AND is_active=true`, tenantID)
	if err != nil {
		return err
	}
	defer urows.Close()
	var users []userRow
	for urows.Next() {
		var u userRow
		urows.Scan(&u.ID, &u.Name, &u.JobRole, &u.Role, &u.CreatedAt)
		users = append(users, u)
	}
	urows.Close()
	if len(users) == 0 {
		return nil
	}

	// 3. Assign peer groups
	peerGroupOf := map[string]string{}
	for _, u := range users {
		ageDays := int(time.Since(u.CreatedAt).Hours() / 24)
		if ageDays < 90 {
			peerGroupOf[u.ID] = "cohort_" + u.CreatedAt.Format("2006-01")
		} else if u.JobRole != "" {
			peerGroupOf[u.ID] = u.JobRole
		} else {
			peerGroupOf[u.ID] = u.Role
		}
	}

	// 4. Get raw AIQ metrics (batch query)
	rawMetrics, err := s.aiqSvc.GetRawMetrics(ctx, tenantID, yearMonth)
	if err != nil {
		return fmt.Errorf("aiq metrics: %w", err)
	}
	rawByUser := map[string]*RawAIQMetrics{}
	for _, m := range rawMetrics {
		rawByUser[m.UserID] = m
	}

	// 5. Group by peer group and compute AIQ scores
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

	// 6. Compute OSS
	ossScores := map[string]float64{}
	if level == 1 {
		ossScores, err = s.ossLevelOne(ctx, tenantID, yearMonth)
		if err != nil {
			return fmt.Errorf("oss level-1: %w", err)
		}
	} else {
		events, err := s.getOutputEvents(ctx, tenantID, yearMonth)
		if err != nil {
			return fmt.Errorf("output events: %w", err)
		}
		ossScores = ossFromEvents(events, level)
	}

	// 7. Build snapshots
	snapshotAt := time.Now().UTC()
	type snapshotData struct {
		userRow
		TotalSCU       float64
		TotalOW        float64
		AIQ            float64
		OSS            float64
		GTS            float64
		HasGTS         bool
		PeerGroup      string
		AnomalyFlagged bool
	}
	var snapshots []snapshotData

	for _, u := range users {
		var totalSCU, totalOW float64
		s.pool.QueryRow(ctx,
			`SELECT COALESCE(SUM(scu_cost),0) FROM usage_records
			 WHERE tenant_id=$1 AND user_id=$2 AND to_char(request_at,'YYYY-MM')=$3`,
			tenantID, u.ID, yearMonth,
		).Scan(&totalSCU)
		s.pool.QueryRow(ctx,
			`SELECT COALESCE(SUM(weight),0) FROM output_events
			 WHERE tenant_id=$1 AND user_id=$2 AND to_char(occurred_at,'YYYY-MM')=$3`,
			tenantID, u.ID, yearMonth,
		).Scan(&totalOW)

		priorScores, _ := s.getPriorScores(ctx, tenantID, u.ID, yearMonth)
		gts, hasGTS := ComputeGTS(priorScores)

		aiq := aiqScores[u.ID]
		oss := ossScores[u.ID]

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
	}

	// 8. Rank within peer groups by final score
	type peerEntry struct {
		idx   int
		score float64
	}
	groups := map[string][]peerEntry{}
	scores := make([]float64, len(snapshots))
	for i, snap := range snapshots {
		aiq := math.Max(0, snap.AIQ)
		aW, oW, gW := AdaptiveWeights(level, snap.HasGTS)
		sc := aW*aiq + oW*snap.OSS + gW*snap.GTS
		if snap.AIQ < 0 || snap.AnomalyFlagged {
			sc = 0 // below threshold or anomaly flagged
		}
		scores[i] = sc
		groups[snap.PeerGroup] = append(groups[snap.PeerGroup], peerEntry{i, sc})
	}
	ranks := make([]int, len(snapshots))
	for _, entries := range groups {
		for i := 1; i < len(entries); i++ {
			for j := i; j > 0 && entries[j].score > entries[j-1].score; j-- {
				entries[j], entries[j-1] = entries[j-1], entries[j]
			}
		}
		for rank, e := range entries {
			ranks[e.idx] = rank + 1
		}
	}

	// 9. Upsert snapshots
	for i, snap := range snapshots {
		sc := scores[i]
		aiq := math.Max(0, snap.AIQ)
		_, err := s.pool.Exec(ctx, `
			INSERT INTO efficiency_snapshots
			(id,tenant_id,user_id,year_month,total_scu,total_output_weight,
			 efficiency_score,aiq_score,oss_score,gts_score,integration_level,
			 peer_group,rank,peer_count,snapshot_at,anomaly_flagged)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
			ON CONFLICT (tenant_id,user_id,year_month) DO UPDATE
			SET total_scu=$5,total_output_weight=$6,efficiency_score=$7,
			    aiq_score=$8,oss_score=$9,gts_score=$10,integration_level=$11,
			    peer_group=$12,rank=$13,peer_count=$14,snapshot_at=$15,
			    anomaly_flagged=$16`,
			uuid.New().String(), tenantID, snap.ID, yearMonth,
			snap.TotalSCU, snap.TotalOW, sc,
			aiq, snap.OSS, snap.GTS, level,
			snap.PeerGroup, ranks[i], len(groups[snap.PeerGroup]), snapshotAt,
			snap.AnomalyFlagged,
		)
		if err != nil {
			return fmt.Errorf("upsert snapshot for %s: %w", snap.ID, err)
		}
	}
	return nil
}

// GetSnapshots returns KPI snapshots for a tenant for a given month (admin view).
func (s *KPIService) GetSnapshots(ctx context.Context, tenantID, yearMonth string) ([]*EfficiencySnapshot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT es.id, es.user_id, u.name, es.year_month,
		       es.total_scu, es.total_output_weight, es.efficiency_score,
		       COALESCE(es.aiq_score,0), COALESCE(es.oss_score,0), COALESCE(es.gts_score,0),
		       COALESCE(es.integration_level,1),
		       es.peer_group, es.rank, es.peer_count, es.snapshot_at,
		       COALESCE(es.anomaly_flagged, false)
		FROM efficiency_snapshots es JOIN users u ON es.user_id=u.id
		WHERE es.tenant_id=$1 AND es.year_month=$2
		ORDER BY es.peer_group, es.rank`, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSnapshots(rows)
}

// GetUserHistory returns last 12 months of snapshots for a user (admin use).
func (s *KPIService) GetUserHistory(ctx context.Context, tenantID, userID string) ([]*EfficiencySnapshot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, '' AS user_name, year_month,
		       total_scu, total_output_weight, efficiency_score,
		       COALESCE(aiq_score,0), COALESCE(oss_score,0), COALESCE(gts_score,0),
		       COALESCE(integration_level,1),
		       peer_group, rank, peer_count, snapshot_at,
		       COALESCE(anomaly_flagged, false)
		FROM efficiency_snapshots
		WHERE tenant_id=$1 AND user_id=$2
		ORDER BY year_month DESC LIMIT 12`, tenantID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSnapshots(rows)
}

// GetUserAIQMetrics returns the raw AIQ sub-metrics for a single user in a given month.
// Returns nil, nil if the user has no usage data for that month.
func (s *KPIService) GetUserAIQMetrics(ctx context.Context, tenantID, userID, yearMonth string) (*RawAIQMetrics, error) {
	all, err := s.aiqSvc.GetRawMetrics(ctx, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	for _, m := range all {
		if m.UserID == userID {
			return m, nil
		}
	}
	return nil, nil
}

// GetMySnapshots returns up to 12 months of snapshots for the logged-in user.
func (s *KPIService) GetMySnapshots(ctx context.Context, userID string) ([]*EfficiencySnapshot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, '' AS user_name, year_month,
		       total_scu, total_output_weight, efficiency_score,
		       COALESCE(aiq_score,0), COALESCE(oss_score,0), COALESCE(gts_score,0),
		       COALESCE(integration_level,1),
		       peer_group, rank, peer_count, snapshot_at,
		       COALESCE(anomaly_flagged, false)
		FROM efficiency_snapshots WHERE user_id=$1
		ORDER BY year_month DESC LIMIT 12`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSnapshots(rows)
}

// GetAnomalies returns snapshots flagged for review in a given month.
func (s *KPIService) GetAnomalies(ctx context.Context, tenantID, yearMonth string) ([]*EfficiencySnapshot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT es.id, es.user_id, u.name, es.year_month,
		       es.total_scu, es.total_output_weight, es.efficiency_score,
		       COALESCE(es.aiq_score,0), COALESCE(es.oss_score,0), COALESCE(es.gts_score,0),
		       COALESCE(es.integration_level,1),
		       es.peer_group, es.rank, es.peer_count, es.snapshot_at,
		       COALESCE(es.anomaly_flagged,false)
		FROM efficiency_snapshots es JOIN users u ON es.user_id=u.id
		WHERE es.tenant_id=$1 AND es.year_month=$2 AND es.anomaly_flagged=true
		ORDER BY es.total_scu DESC`, tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSnapshots(rows)
}

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
			&s.PeerGroup, &s.Rank, &s.PeerCount, &s.SnapshotAt, &s.AnomalyFlagged); err != nil {
			return nil, err
		}
		results = append(results, s)
	}
	return results, nil
}

// IsNewEmployee and CohortGroup preserved for backwards compatibility.
func IsNewEmployee(ageDays int) bool { return ageDays < 90 }
func CohortGroup(hireYearMonth string) string { return "cohort_" + hireYearMonth }

// ComputeEfficiencyScore kept for any existing tests; v2 uses RunMonthlySnapshot directly.
func ComputeEfficiencyScore(outputWeight, totalSCU float64) float64 {
	if totalSCU == 0 || outputWeight == 0 {
		return 0
	}
	return outputWeight / math.Log(totalSCU+1)
}
