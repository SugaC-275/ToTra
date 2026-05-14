package services_test

import (
	"testing"

	"github.com/yourorg/totra/admin/services"
)

func TestSpendChange_Increase(t *testing.T) {
	got := services.SpendChange(120.0, 100.0)
	if got != 20.0 {
		t.Fatalf("want 20.0, got %f", got)
	}
}

func TestSpendChange_Decrease(t *testing.T) {
	got := services.SpendChange(80.0, 100.0)
	if got != -20.0 {
		t.Fatalf("want -20.0, got %f", got)
	}
}

func TestSpendChange_NoPrevious(t *testing.T) {
	got := services.SpendChange(50.0, 0)
	if got != 0 {
		t.Fatalf("want 0 when no previous, got %f", got)
	}
}

func TestCrossedThresholds_None(t *testing.T) {
	got := services.CrossedThresholds(30.0)
	if len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}

func TestCrossedThresholds_Half(t *testing.T) {
	got := services.CrossedThresholds(55.0)
	if len(got) != 1 || got[0] != 50 {
		t.Fatalf("want [50], got %v", got)
	}
}

func TestCrossedThresholds_All(t *testing.T) {
	got := services.CrossedThresholds(105.0)
	if len(got) != 3 {
		t.Fatalf("want [50 80 100], got %v", got)
	}
}
