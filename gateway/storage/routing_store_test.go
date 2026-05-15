package storage_test

import (
	"testing"

	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// Interface compliance: RoutingStore must implement RoutingRecorder
// (which now returns (int64, error)).
func TestRoutingStore_ImplementsInterface(t *testing.T) {
	var _ middleware.RoutingRecorder = (*storage.RoutingStore)(nil)
}

func TestModelPrice_Fields(t *testing.T) {
	p := storage.ModelPrice{PricePerMInput: 5.0, PricePerMOutput: 15.0}
	if p.PricePerMInput != 5.0 || p.PricePerMOutput != 15.0 {
		t.Fatal("ModelPrice field assignment failed")
	}
}
