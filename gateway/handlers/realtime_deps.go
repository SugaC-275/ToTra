package handlers

import (
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// RealtimeDeps holds the dependencies injected into the realtime WebSocket proxy.
type RealtimeDeps struct {
	ModelLookup   RealtimeLookup
	QuotaStore    *storage.QuotaStore
	QuotaFetcher  middleware.UserQuotaFetcher
	UsageStore    RealtimeUsageRecorder
	BundleChecker BundleComplianceChecker
	SessionStore  *storage.RealtimeSessionStore
}
