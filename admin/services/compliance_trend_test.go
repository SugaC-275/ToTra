package services_test

import (
	"testing"

	"github.com/yourorg/totra/admin/services"
)

func TestMonthlyRiskScore_Zero(t *testing.T) {
	if got := services.MonthlyRiskScore(0); got != 0.0 {
		t.Fatalf("want 0.0, got %f", got)
	}
}

func TestMonthlyRiskScore_Low(t *testing.T) {
	got := services.MonthlyRiskScore(5)
	if got < 9.999 || got > 10.001 {
		t.Fatalf("want ~10.0, got %f", got)
	}
}

func TestMonthlyRiskScore_Saturates(t *testing.T) {
	if got := services.MonthlyRiskScore(100); got != 100.0 {
		t.Fatalf("want 100.0, got %f", got)
	}
	if got := services.MonthlyRiskScore(999); got != 100.0 {
		t.Fatalf("want 100.0, got %f", got)
	}
}

func TestMonthlyRiskScore_Midpoint(t *testing.T) {
	got := services.MonthlyRiskScore(25)
	if got < 49.999 || got > 50.001 {
		t.Fatalf("want ~50.0, got %f", got)
	}
}
