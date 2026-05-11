package services_test

import (
	"context"
	"testing"

	"github.com/pashagolub/pgxmock/v3"
	"github.com/yourorg/totra/admin/services"
)

func newAllowlistPool(t *testing.T) (pgxmock.PgxPoolIface, *services.IPAllowlistService) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	svc := services.NewIPAllowlistService(mock)
	return mock, svc
}

func TestIPAllowlistList(t *testing.T) {
	mock, svc := newAllowlistPool(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"id", "cidr", "label", "created_at"}).
		AddRow("id-1", "10.0.0.0/8", "office", "2026-05-11T00:00:00Z").
		AddRow("id-2", "203.0.113.5/32", "vpn", "2026-05-11T00:00:00Z")
	mock.ExpectQuery(`SELECT id, cidr, label, created_at FROM tenant_ip_allowlists`).
		WithArgs("tenant-1").
		WillReturnRows(rows)

	entries, err := svc.List(context.Background(), "tenant-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].CIDR != "10.0.0.0/8" {
		t.Errorf("unexpected CIDR: %s", entries[0].CIDR)
	}
}

func TestIPAllowlistAdd(t *testing.T) {
	mock, svc := newAllowlistPool(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"id", "cidr", "label", "created_at"}).
		AddRow("new-id", "192.168.1.0/24", "branch", "2026-05-11T00:00:00Z")
	mock.ExpectQuery(`INSERT INTO tenant_ip_allowlists`).
		WithArgs("tenant-1", "192.168.1.0/24", "branch").
		WillReturnRows(rows)

	entry, err := svc.Add(context.Background(), "tenant-1", "192.168.1.0/24", "branch")
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID != "new-id" {
		t.Errorf("unexpected ID: %s", entry.ID)
	}
}

func TestIPAllowlistDelete(t *testing.T) {
	mock, svc := newAllowlistPool(t)
	defer mock.Close()

	mock.ExpectExec(`DELETE FROM tenant_ip_allowlists`).
		WithArgs("entry-id", "tenant-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	err := svc.Delete(context.Background(), "tenant-1", "entry-id")
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadTenantCIDRs(t *testing.T) {
	mock, svc := newAllowlistPool(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"cidr"}).
		AddRow("10.0.0.0/8").
		AddRow("203.0.113.5/32")
	mock.ExpectQuery(`SELECT cidr FROM tenant_ip_allowlists`).
		WithArgs("tenant-1").
		WillReturnRows(rows)

	cidrs, err := svc.LoadTenantCIDRs(context.Background(), "tenant-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cidrs) != 2 {
		t.Fatalf("expected 2, got %d", len(cidrs))
	}
}
