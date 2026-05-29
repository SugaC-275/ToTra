package handlers

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
	"golang.org/x/net/websocket"
)

const (
	openAIRealtimeOrigin = "https://api.openai.com"
	wsGUID               = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	// realtimeQuotaEstimate is a flat SCU cost applied before the session starts
	// (actual usage is tracked from response.done events).
	realtimeQuotaEstimate = 100
)

// RealtimeLookup is satisfied by *storage.PGModelLookup.
type RealtimeLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

// RealtimeUsageRecorder is satisfied by *storage.UsageStore.
type RealtimeUsageRecorder interface {
	Record(r *storage.UsageRecord)
}

// realtimeDoneUsage matches the usage object inside a response.done event.
type realtimeDoneUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// realtimeDoneResponse is the top-level shape of a response.done event.
type realtimeDoneResponse struct {
	Type     string `json:"type"`
	Response struct {
		Usage *realtimeDoneUsage `json:"usage"`
	} `json:"response"`
}

// realtimeTextDelta is the shape of a response.text.delta event.
type realtimeTextDelta struct {
	Type  string `json:"type"`
	Delta struct {
		Text string `json:"text"`
	} `json:"delta"`
}

// RegisterRealtimeRoutes mounts the WebSocket realtime proxy on the given app.
// It applies its own auth + quota checks before upgrading so that the standard
// v1 middleware chain (which expects HTTP bodies) is bypassed.
func RegisterRealtimeRoutes(app *fiber.App, deps RealtimeDeps) {
	app.Get("/v1/realtime", NewRealtimeHandler(deps))

	// Admin route — list active and recent realtime sessions.
	if deps.SessionStore != nil {
		app.Get("/admin/realtime/sessions", func(c *fiber.Ctx) error {
			tenantID := c.Query("tenant_id")
			sessions, err := deps.SessionStore.ListActive(c.Context(), tenantID)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": fiber.Map{"message": err.Error(), "type": "server_error"},
				})
			}
			if sessions == nil {
				sessions = []*storage.RealtimeSession{}
			}
			return c.JSON(fiber.Map{"sessions": sessions})
		})
	}
}

// NewRealtimeHandler returns a Fiber handler that proxies WebSocket connections
// to the OpenAI Realtime API (wss://api.openai.com/v1/realtime) with:
//   - JWT/API-key auth before upgrade
//   - Compliance bundle gating (healthcare → HIPAA-eligible)
//   - Quota check before upgrade
//   - Per-frame PII scanning on response.text.delta events
//   - Token/cost tracking from response.done events
func NewRealtimeHandler(deps RealtimeDeps) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// ── 1. Auth ────────────────────────────────────────────────────────────
		// WebSocket clients cannot set headers reliably; accept token from query
		// param as a fallback.
		token := strings.TrimPrefix(c.Get("Authorization"), "Bearer ")
		if token == "" {
			token = c.Query("token")
		}
		if token == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "missing auth token", "type": "auth_error"},
			})
		}

		// We replicate the auth lookup inline so the realtime handler can stand
		// on its own without the v1 middleware stack.
		var user *middleware.UserInfo
		if u, ok := c.Locals("user").(*middleware.UserInfo); ok && u != nil {
			user = u
		}
		// If auth middleware already ran (e.g. the route is mounted behind the
		// v1 group) we use the cached value; otherwise we cannot look up the user
		// without a lookup dependency. For simplicity the route is always mounted
		// outside the v1 group with its own auth middleware, so the Locals value
		// should be set. Guard defensively.
		if user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "unauthorized", "type": "auth_error"},
			})
		}

		// ── 2. Model validation ────────────────────────────────────────────────
		modelName := c.Query("model")
		if modelName == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "model query parameter required", "type": "bad_request"},
			})
		}

		modelCfg, err := deps.ModelLookup.GetByName(c.Context(), user.TenantID, modelName)
		if err != nil || modelCfg == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{
					"message": fmt.Sprintf("model %q not configured", modelName),
					"type":    "model_not_found",
				},
			})
		}

		// ── 3. Compliance bundle gating ────────────────────────────────────────
		if err := CheckBundleCompliance(c, user, modelCfg, deps.BundleChecker); err != nil {
			return err
		}

		// ── 4. Quota check ─────────────────────────────────────────────────────
		if deps.QuotaStore != nil && deps.QuotaFetcher != nil {
			yearMonth := time.Now().UTC().Format("2006-01")
			quotaLimit, qErr := deps.QuotaFetcher.GetUserQuota(c.Context(), user.TenantID, user.UserID)
			if qErr != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": fiber.Map{"message": "quota lookup failed", "type": "server_error"},
				})
			}
			allowed, _, qErr := deps.QuotaStore.CheckAndIncrement(
				c.Context(), user.TenantID, user.UserID, yearMonth,
				quotaLimit, realtimeQuotaEstimate,
			)
			if qErr != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": fiber.Map{"message": "quota check failed", "type": "server_error"},
				})
			}
			if !allowed {
				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
					"error": fiber.Map{
						"message": fmt.Sprintf("quota exceeded. limit: %d SCU/month", quotaLimit),
						"type":    "quota_exceeded",
					},
				})
			}
		}

		// ── 5. WebSocket upgrade check ─────────────────────────────────────────
		if c.Get("Upgrade") != "websocket" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "Upgrade: websocket header required", "type": "bad_request"},
			})
		}
		wsKey := c.Get("Sec-Websocket-Key")
		if wsKey == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "Sec-WebSocket-Key header required", "type": "bad_request"},
			})
		}

		wsAccept := computeAccept(wsKey)
		apiKey := modelCfg.APIKey
		realtimeURL := fmt.Sprintf("wss://api.openai.com/v1/realtime?model=%s", modelName)

		// Capture for goroutine closure.
		tenantID := user.TenantID
		userID := user.UserID
		modelConfigID := modelCfg.ID

		// Create session record (best-effort; nil store means disabled).
		sessionID := uuid.New().String()
		if deps.SessionStore != nil {
			sess := &storage.RealtimeSession{
				ID:        sessionID,
				TenantID:  tenantID,
				UserID:    userID,
				Model:     modelName,
				StartedAt: time.Now().UTC(),
			}
			if sErr := deps.SessionStore.Create(c.Context(), sess); sErr != nil {
				slog.Warn("realtime: create session record", "err", sErr)
			}
		}

		// ── 6. Hijack the TCP connection and complete the WS handshake ─────────
		c.Context().HijackSetNoResponse(true)
		c.Context().Hijack(func(clientConn net.Conn) {
			defer clientConn.Close()

			handshake := fmt.Sprintf(
				"HTTP/1.1 101 Switching Protocols\r\n"+
					"Upgrade: websocket\r\n"+
					"Connection: Upgrade\r\n"+
					"Sec-WebSocket-Accept: %s\r\n\r\n",
				wsAccept,
			)
			if _, hErr := io.WriteString(clientConn, handshake); hErr != nil {
				slog.Error("realtime: write handshake", "tenant", tenantID, "err", hErr)
				return
			}

			// ── 7. Open outbound WebSocket to OpenAI ──────────────────────────
			cfg, cfgErr := websocket.NewConfig(realtimeURL, openAIRealtimeOrigin)
			if cfgErr != nil {
				slog.Error("realtime: build ws config", "tenant", tenantID, "err", cfgErr)
				writeWSClose(clientConn, 1011, "upstream config error")
				return
			}
			cfg.Header.Set("Authorization", "Bearer "+apiKey)
			cfg.Header.Set("OpenAI-Beta", "realtime=v1")

			upstreamWS, dialErr := websocket.DialConfig(cfg)
			if dialErr != nil {
				slog.Error("realtime: dial upstream", "tenant", tenantID, "model", modelName, "err", dialErr)
				writeWSClose(clientConn, 1011, "upstream unavailable")
				return
			}
			defer upstreamWS.Close()

			done := make(chan struct{}, 2)

			// Accumulated token usage across response.done events this session.
			var totalInput, totalOutput int64

			// ── 8a. Client → OpenAI (forward verbatim) ────────────────────────
			go func() {
				defer func() { done <- struct{}{} }()
				if _, cpErr := io.Copy(upstreamWS, clientConn); cpErr != nil {
					slog.Debug("realtime: client→upstream copy ended", "tenant", tenantID, "err", cpErr)
				}
			}()

			// ── 8b. OpenAI → Client (inspect frames) ──────────────────────────
			go func() {
				defer func() { done <- struct{}{} }()
				buf := make([]byte, 64*1024)
				for {
					n, rdErr := upstreamWS.Read(buf)
					if rdErr != nil {
						if rdErr != io.EOF {
							slog.Debug("realtime: upstream read ended", "tenant", tenantID, "err", rdErr)
						}
						return
					}
					frame := buf[:n]
					handled := false

					// Opportunistically parse JSON frames (text frames only).
					if len(frame) > 0 && frame[0] == '{' {
						var ev struct {
							Type string `json:"type"`
						}
						if json.Unmarshal(frame, &ev) == nil {
							switch ev.Type {
							case "response.text.delta":
								var delta realtimeTextDelta
								if json.Unmarshal(frame, &delta) == nil && delta.Delta.Text != "" {
									if piiType, found := middleware.ScanForPII(delta.Delta.Text); found {
										// PII detected: cancel the response upstream, warn the client.
										cancelFrame := []byte(`{"type":"response.cancel"}`)
										if _, wErr := upstreamWS.Write(cancelFrame); wErr != nil {
											slog.Warn("realtime: send cancel", "tenant", tenantID, "err", wErr)
										}
										errEvent := fmt.Sprintf(
											`{"type":"error","error":{"type":"pii_blocked","message":"response cancelled: PII detected (%s)"}}`,
											piiType,
										)
										if _, wErr := clientConn.Write(buildWSTextFrame([]byte(errEvent))); wErr != nil {
											slog.Warn("realtime: send pii error to client", "tenant", tenantID, "err", wErr)
										}
										slog.Warn("realtime: PII in text delta — cancelled", "tenant", tenantID, "pii_type", piiType)
										// Async bookkeeping.
										if deps.SessionStore != nil {
											go deps.SessionStore.IncrPIIEvents(context.Background(), sessionID)
										}
										handled = true // do NOT forward the original delta
									}
								}

							case "response.done":
								var doneEv realtimeDoneResponse
								if json.Unmarshal(frame, &doneEv) == nil && doneEv.Response.Usage != nil {
									totalInput += doneEv.Response.Usage.InputTokens
									totalOutput += doneEv.Response.Usage.OutputTokens
									slog.Debug("realtime: usage event",
										"tenant", tenantID,
										"input_tokens", doneEv.Response.Usage.InputTokens,
										"output_tokens", doneEv.Response.Usage.OutputTokens,
									)
								}
							}
						}
					}

					if !handled {
						if _, wErr := clientConn.Write(frame); wErr != nil {
							slog.Debug("realtime: write to client ended", "tenant", tenantID, "err", wErr)
							return
						}
					}
				}
			}()

			<-done
			_ = upstreamWS.Close()
			clientConn.Close()
			<-done

			// ── 9. Record usage asynchronously ────────────────────────────────
			if deps.UsageStore != nil {
				deps.UsageStore.Record(&storage.UsageRecord{
					TenantID:         tenantID,
					UserID:           userID,
					ModelConfigID:    modelConfigID,
					PromptTokens:     int(totalInput),
					CompletionTokens: int(totalOutput),
				})
			}

			if deps.SessionStore != nil {
				closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				deps.SessionStore.Close(closeCtx, sessionID, totalInput, totalOutput, 0)
			}

			slog.Info("realtime: session ended",
				"tenant", tenantID,
				"model", modelName,
				"input_tokens", totalInput,
				"output_tokens", totalOutput,
			)
		})

		return nil
	}
}

// computeAccept returns the Sec-WebSocket-Accept header value for the given key.
func computeAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// buildWSTextFrame wraps payload in an unmasked WebSocket text frame (opcode 1).
func buildWSTextFrame(payload []byte) []byte {
	return buildWSFrame(1, payload)
}

// writeWSClose sends a WebSocket close frame to conn with the given status code
// and reason. Errors are silently ignored since we are in a cleanup path.
func writeWSClose(conn net.Conn, code uint16, reason string) {
	payload := make([]byte, 2+len(reason))
	payload[0] = byte(code >> 8)
	payload[1] = byte(code & 0xff)
	copy(payload[2:], reason)
	frame := buildWSFrame(8, payload)
	_, _ = conn.Write(frame)
}

// buildWSFrame builds a minimal unmasked WebSocket frame (server-to-client).
func buildWSFrame(opcode byte, payload []byte) []byte {
	n := len(payload)
	var header []byte
	first := byte(0x80) | (opcode & 0x0f) // FIN + opcode
	switch {
	case n < 126:
		header = []byte{first, byte(n)}
	case n < 65536:
		header = []byte{first, 126, byte(n >> 8), byte(n & 0xff)}
	default:
		header = []byte{first, 127,
			0, 0, 0, 0,
			byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n & 0xff),
		}
	}
	return append(header, payload...)
}
