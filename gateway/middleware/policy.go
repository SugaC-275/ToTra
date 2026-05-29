package middleware

import (
	"context"
	"log"
	"regexp"
	"time"

	"github.com/gofiber/fiber/v2"
)

type PolicyRule struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
	Action  string `json:"action"`
}

type PolicyRuleGetter interface {
	GetRules(ctx context.Context, tenantID string) ([]*PolicyRule, error)
}

// PolicyEventInserter is satisfied by *storage.PolicyEventStore.
// The interface lives in middleware to avoid an import cycle.
type PolicyEventInserter interface {
	InsertPolicyEvent(ctx context.Context, ev PolicyEventRecord) error
}

// PolicyEventRecord is the data shape passed across the interface boundary.
type PolicyEventRecord struct {
	TenantID       string
	UserID         string
	RuleName       string
	Action         string
	MatchedPattern string
	RequestID      string
}

// GuardrailConfigGetter is implemented by storage.GuardrailStore.
// It lives in the middleware package to avoid import cycles.
type GuardrailConfigGetter interface {
	GetGuardrailConfig(ctx context.Context, tenantID, name string) (*GuardrailCfg, error)
}

// GuardrailCfg is the minimal shape consumed by policy middleware.
// storage.GuardrailStore satisfies GuardrailConfigGetter by returning a type
// that is converted to *GuardrailCfg at the wire-up site in main.go.
type GuardrailCfg struct {
	Enabled    bool
	Strictness string // "permissive" | "standard" | "strict"
}

func NewPolicyMiddleware(store PolicyRuleGetter, siemChan chan<- SIEMEvent) fiber.Handler {
	return NewPolicyMiddlewareWithEvents(store, siemChan, nil)
}

// NewPolicyMiddlewareWithGuardrails wires in both a PolicyEventInserter and a
// GuardrailConfigGetter so policy enforcement respects per-tenant guardrail config.
func NewPolicyMiddlewareWithGuardrails(store PolicyRuleGetter, siemChan chan<- SIEMEvent, events PolicyEventInserter, guardrailStore GuardrailConfigGetter) fiber.Handler {
	return buildPolicyMiddleware(store, siemChan, events, guardrailStore)
}

// NewPolicyMiddlewareWithEvents wires in a PolicyEventInserter so that both
// 'block' and 'log' actions write structured events to the DB.
func NewPolicyMiddlewareWithEvents(store PolicyRuleGetter, siemChan chan<- SIEMEvent, events PolicyEventInserter) fiber.Handler {
	return buildPolicyMiddleware(store, siemChan, events, nil)
}

func buildPolicyMiddleware(store PolicyRuleGetter, siemChan chan<- SIEMEvent, events PolicyEventInserter, guardrailStore GuardrailConfigGetter) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*UserInfo)
		if !ok || user == nil {
			return c.Next()
		}

		// Consult guardrail config for policy_rules check.
		strictness := "standard"
		if guardrailStore != nil {
			cfg, err := guardrailStore.GetGuardrailConfig(c.Context(), user.TenantID, "policy_rules")
			if err != nil {
				log.Printf("policy middleware: guardrail config lookup: %v", err)
			} else if cfg != nil {
				if !cfg.Enabled {
					// Policy rules disabled for this tenant — skip all checks.
					return c.Next()
				}
				if cfg.Strictness != "" {
					strictness = cfg.Strictness
				}
			}
		}

		rules, err := store.GetRules(c.Context(), user.TenantID)
		if err != nil {
			log.Printf("policy middleware: %v", err)
			return c.Next()
		}
		body := string(c.Body())
		requestID := c.Get("X-Request-ID")
		for _, rule := range rules {
			re, err := regexp.Compile(rule.Pattern)
			if err != nil {
				continue
			}
			if !re.MatchString(body) {
				continue
			}

			// Apply strictness overrides before dispatching.
			action := rule.Action
			switch strictness {
			case "permissive":
				if action == "block" {
					action = "log"
				}
			case "strict":
				if action == "log" {
					action = "block"
				}
			}

			switch action {
			case "block":
				if siemChan != nil {
					select {
					case siemChan <- SIEMEvent{
						TenantID:  user.TenantID,
						EventType: "policy_block",
						Payload: map[string]any{
							"source":      "totra",
							"tenant_id":   user.TenantID,
							"event_type":  "policy_block",
							"occurred_at": time.Now().UTC().Format(time.RFC3339),
							"detail": map[string]any{
								"user_id":    user.UserID,
								"rule_name":  rule.Name,
								"action":     "blocked",
								"strictness": strictness,
								"path":       c.Path(),
							},
						},
					}:
					default:
					}
				}
				if events != nil {
					ev := PolicyEventRecord{
						TenantID:       user.TenantID,
						UserID:         user.UserID,
						RuleName:       rule.Name,
						Action:         "block",
						MatchedPattern: rule.Pattern,
						RequestID:      requestID,
					}
					go func(ev PolicyEventRecord) {
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						if err := events.InsertPolicyEvent(ctx, ev); err != nil {
							log.Printf("policy middleware: insert event: %v", err)
						}
					}(ev)
				}
				return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
					"error": fiber.Map{
						"message": "request blocked by policy rule: " + rule.Name,
						"type":    "policy_blocked",
					},
				})

			case "log":
				// Write structured event to DB and emit SIEM — request continues.
				if siemChan != nil {
					select {
					case siemChan <- SIEMEvent{
						TenantID:  user.TenantID,
						EventType: "policy_log",
						Payload: map[string]any{
							"source":      "totra",
							"tenant_id":   user.TenantID,
							"event_type":  "policy_log",
							"occurred_at": time.Now().UTC().Format(time.RFC3339),
							"detail": map[string]any{
								"user_id":    user.UserID,
								"rule_name":  rule.Name,
								"action":     "log",
								"strictness": strictness,
								"path":       c.Path(),
							},
						},
					}:
					default:
					}
				}
				if events != nil {
					ev := PolicyEventRecord{
						TenantID:       user.TenantID,
						UserID:         user.UserID,
						RuleName:       rule.Name,
						Action:         "log",
						MatchedPattern: rule.Pattern,
						RequestID:      requestID,
					}
					go func(ev PolicyEventRecord) {
						ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						if err := events.InsertPolicyEvent(ctx, ev); err != nil {
							log.Printf("policy middleware: insert event: %v", err)
						}
					}(ev)
				}
				// Do NOT return — request continues.

			default:
				log.Printf("policy rule matched (unknown action %q): tenant=%s rule=%s", rule.Action, user.TenantID, rule.Name)
			}
		}
		return c.Next()
	}
}
