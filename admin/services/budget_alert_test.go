package services_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/admin/services"
)

func TestBudgetAlertService_Constructor(t *testing.T) {
	svc := services.NewBudgetAlertService((*pgxpool.Pool)(nil))
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

func TestCrossedThresholds_Exactly50(t *testing.T) {
	got := services.CrossedThresholds(50.0)
	if len(got) != 1 || got[0] != 50 {
		t.Fatalf("want [50], got %v", got)
	}
}

func TestCrossedThresholds_Exactly80(t *testing.T) {
	got := services.CrossedThresholds(80.0)
	if len(got) != 2 {
		t.Fatalf("want [50 80], got %v", got)
	}
}

func TestCrossedThresholds_Exactly100(t *testing.T) {
	got := services.CrossedThresholds(100.0)
	if len(got) != 3 {
		t.Fatalf("want [50 80 100], got %v", got)
	}
}
