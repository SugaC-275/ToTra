package middleware

type SIEMEvent struct {
	TenantID  string
	EventType string
	Payload   map[string]any
}
