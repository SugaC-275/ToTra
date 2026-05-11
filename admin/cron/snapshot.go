package cron

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/admin/services"
)

// StartMonthlySnapshot starts a background goroutine that runs the KPI snapshot
// at 00:05 on the 1st of each month for all tenants, then triggers fuel rewards.
func StartMonthlySnapshot(
	pool *pgxpool.Pool,
	kpiSvc *services.KPIService,
	fuelSvc *services.FuelService,
	auditSvc *services.AuditService,
) {
	go func() {
		for {
			next := nextMonthStart()
			log.Printf("cron: next KPI snapshot at %s", next.Format(time.RFC3339))
			time.Sleep(time.Until(next))

			yearMonth := time.Now().UTC().AddDate(0, -1, 0).Format("2006-01")
			log.Printf("cron: running monthly KPI snapshot for %s", yearMonth)

			ctx := context.Background()
			rows, err := pool.Query(ctx, `SELECT id FROM tenants`)
			if err != nil {
				log.Printf("cron: failed to list tenants: %v", err)
				continue
			}
			var tenantIDs []string
			for rows.Next() {
				var id string
				rows.Scan(&id)
				tenantIDs = append(tenantIDs, id)
			}
			rows.Close()

			for _, tenantID := range tenantIDs {
				if err := kpiSvc.RunMonthlySnapshot(ctx, tenantID, yearMonth); err != nil {
					log.Printf("cron: snapshot failed for tenant %s: %v", tenantID, err)
					continue
				}
				if err := fuelSvc.ApplyRewards(ctx, tenantID, yearMonth); err != nil {
					log.Printf("cron: fuel rewards failed for tenant %s: %v", tenantID, err)
				}
				// Audit: append one entry per snapshot produced this month.
				snapshots, err := kpiSvc.GetSnapshots(ctx, tenantID, yearMonth)
				if err != nil {
					log.Printf("cron: audit fetch snapshots failed for tenant %s: %v", tenantID, err)
					continue
				}
				for _, snap := range snapshots {
					if aerr := auditSvc.AppendAuditLog(
						ctx,
						tenantID,
						"kpi_snapshot",
						snap.ID,
						snap,
					); aerr != nil {
						log.Printf("cron: audit append failed for snapshot %s: %v", snap.ID, aerr)
					}
				}
				log.Printf("cron: audited %d snapshots for tenant %s", len(snapshots), tenantID)
			}
			log.Printf("cron: monthly snapshot complete for %d tenants", len(tenantIDs))
		}
	}()
}

func nextMonthStart() time.Time {
	now := time.Now().UTC()
	first := time.Date(now.Year(), now.Month()+1, 1, 0, 5, 0, 0, time.UTC)
	return first
}
