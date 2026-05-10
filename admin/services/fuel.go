package services

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FuelSettings struct {
	TenantID      string    `json:"tenant_id"`
	Top10PctBonus float64   `json:"top_10pct_bonus"`
	Top25PctBonus float64   `json:"top_25pct_bonus"`
	Top50PctBonus float64   `json:"top_50pct_bonus"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type FuelTransaction struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	AmountSCU int       `json:"amount_scu"`
	Reason    string    `json:"reason"`
	Tier      string    `json:"tier"`
	CreatedAt time.Time `json:"created_at"`
}

// RewardTier returns the tier key for a given rank/peerCount, or "" if no reward.
func RewardTier(rank, peerCount int, s FuelSettings) string {
	if peerCount == 0 {
		return ""
	}
	pct := float64(rank) / float64(peerCount)
	switch {
	case pct <= 0.10:
		return "top_10pct"
	case pct <= 0.25:
		return "top_25pct"
	case pct <= 0.50:
		return "top_50pct"
	}
	return ""
}

// AwardedSCU returns FLOOR(quota * bonusRate).
func AwardedSCU(quotaSCU int, bonusRate float64) int {
	return int(math.Floor(float64(quotaSCU) * bonusRate))
}

type FuelService struct {
	pool *pgxpool.Pool
}

func NewFuelService(pool *pgxpool.Pool) *FuelService {
	return &FuelService{pool: pool}
}

// GetSettings returns fuel settings for a tenant, inserting defaults if absent.
func (s *FuelService) GetSettings(ctx context.Context, tenantID string) (*FuelSettings, error) {
	fs := &FuelSettings{TenantID: tenantID}
	err := s.pool.QueryRow(ctx,
		`SELECT top_10pct_bonus, top_25pct_bonus, top_50pct_bonus, updated_at
		 FROM fuel_settings WHERE tenant_id=$1`,
		tenantID,
	).Scan(&fs.Top10PctBonus, &fs.Top25PctBonus, &fs.Top50PctBonus, &fs.UpdatedAt)
	if err != nil {
		_, insertErr := s.pool.Exec(ctx,
			`INSERT INTO fuel_settings (tenant_id) VALUES ($1) ON CONFLICT DO NOTHING`,
			tenantID,
		)
		if insertErr != nil {
			return nil, insertErr
		}
		fs.Top10PctBonus, fs.Top25PctBonus, fs.Top50PctBonus = 0.20, 0.10, 0.05
	}
	return fs, nil
}

// UpdateSettings saves new bonus rates for a tenant.
func (s *FuelService) UpdateSettings(ctx context.Context, tenantID string, top10, top25, top50 float64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO fuel_settings (tenant_id, top_10pct_bonus, top_25pct_bonus, top_50pct_bonus)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (tenant_id) DO UPDATE
		 SET top_10pct_bonus=$2, top_25pct_bonus=$3, top_50pct_bonus=$4, updated_at=NOW()`,
		tenantID, top10, top25, top50,
	)
	return err
}

// ApplyRewards computes and applies quota rewards for all established employees
// (peer_group NOT starting with "cohort_") in a tenant for a given yearMonth.
func (s *FuelService) ApplyRewards(ctx context.Context, tenantID, yearMonth string) error {
	settings, err := s.GetSettings(ctx, tenantID)
	if err != nil {
		return err
	}

	rows, err := s.pool.Query(ctx,
		`SELECT es.id, es.user_id, es.rank, es.peer_count, es.peer_group, u.quota_scu
		 FROM efficiency_snapshots es
		 JOIN users u ON es.user_id = u.id
		 WHERE es.tenant_id=$1 AND es.year_month=$2`,
		tenantID, yearMonth,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type rewardRow struct {
		SnapshotID string
		UserID     string
		Rank       int
		PeerCount  int
		PeerGroup  string
		QuotaSCU   int
	}
	var rewards []rewardRow
	for rows.Next() {
		var r rewardRow
		if err := rows.Scan(&r.SnapshotID, &r.UserID, &r.Rank, &r.PeerCount, &r.PeerGroup, &r.QuotaSCU); err != nil {
			return err
		}
		rewards = append(rewards, r)
	}
	rows.Close()

	for _, r := range rewards {
		// Skip new employees (cohort peer groups)
		if len(r.PeerGroup) >= 7 && r.PeerGroup[:7] == "cohort_" {
			continue
		}
		tier := RewardTier(r.Rank, r.PeerCount, *settings)
		if tier == "" {
			continue
		}
		var bonusRate float64
		switch tier {
		case "top_10pct":
			bonusRate = settings.Top10PctBonus
		case "top_25pct":
			bonusRate = settings.Top25PctBonus
		case "top_50pct":
			bonusRate = settings.Top50PctBonus
		}
		awarded := AwardedSCU(r.QuotaSCU, bonusRate)
		if awarded == 0 {
			continue
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`UPDATE users SET quota_scu = quota_scu + $1 WHERE id = $2`,
			awarded, r.UserID,
		)
		if err != nil {
			tx.Rollback(ctx)
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO fuel_transactions
			 (id, tenant_id, user_id, amount_scu, reason, ref_snapshot_id, tier)
			 VALUES ($1,$2,$3,$4,'efficiency_reward',$5,$6)`,
			uuid.New().String(), tenantID, r.UserID, awarded, r.SnapshotID, tier,
		)
		if err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// GetMyTransactions returns AI-Fuel reward history for a user (last 24 entries).
func (s *FuelService) GetMyTransactions(ctx context.Context, userID string) ([]*FuelTransaction, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, amount_scu, reason, COALESCE(tier,''), created_at
		 FROM fuel_transactions WHERE user_id=$1 ORDER BY created_at DESC LIMIT 24`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var txns []*FuelTransaction
	for rows.Next() {
		ft := &FuelTransaction{}
		if err := rows.Scan(&ft.ID, &ft.UserID, &ft.AmountSCU, &ft.Reason, &ft.Tier, &ft.CreatedAt); err != nil {
			return nil, err
		}
		txns = append(txns, ft)
	}
	return txns, nil
}
