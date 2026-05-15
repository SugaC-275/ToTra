package handlers

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/providers"
	"github.com/yourorg/totra/gateway/storage"
	"github.com/yourorg/totra/gateway/tokenizer"
)

const maxFileSizeBytes = 20 * 1024 * 1024 // 20 MB

// ModelLookup is satisfied by *storage.PGModelLookup.
type ModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

// FileParser is satisfied by *storage.ParserClient.
type FileParser interface {
	Parse(ctx context.Context, filename string, data []byte) (*storage.ParseResult, error)
}

// UsageRecorder is satisfied by *storage.UsageStore.
type UsageRecorder interface {
	Record(r *storage.UsageRecord)
}

func NewFileChatHandler(
	modelLookup ModelLookup,
	parser FileParser,
	piiRecorder middleware.ViolationRecorder,
	usageRecorder UsageRecorder,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user := c.Locals("user").(*middleware.UserInfo)

		fileHeader, err := c.FormFile("file")
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fiber.Map{
				"message": "file field required", "type": "bad_request",
			}})
		}
		if fileHeader.Size > maxFileSizeBytes {
			return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"error": fiber.Map{
				"message": "file exceeds 20MB limit", "type": "file_too_large",
			}})
		}

		message := c.FormValue("message")
		modelName := c.FormValue("model")
		if message == "" || modelName == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fiber.Map{
				"message": "message and model fields required", "type": "bad_request",
			}})
		}

		modelCfg, err := modelLookup.GetByName(c.Context(), user.TenantID, modelName)
		if err != nil || modelCfg == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fiber.Map{
				"message": fmt.Sprintf("model %q not configured", modelName), "type": "model_not_found",
			}})
		}

		if modelCfg.Provider == "local" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fiber.Map{
				"message": "local models do not support file uploads", "type": "local_model_not_supported",
			}})
		}

		adapter, err := providers.New(modelCfg.Provider, modelCfg.BaseURL, modelCfg.APIKey)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fiber.Map{
				"message": "unsupported provider: " + modelCfg.Provider, "type": "unsupported_provider",
			}})
		}

		f, err := fileHeader.Open()
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fiber.Map{
				"message": "failed to open file", "type": "internal_error",
			}})
		}
		defer f.Close()
		fileBytes, err := io.ReadAll(f)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fiber.Map{
				"message": "failed to read file", "type": "internal_error",
			}})
		}

		parseResult, err := parser.Parse(c.Context(), fileHeader.Filename, fileBytes)
		if err != nil {
			if strings.Contains(err.Error(), "unsupported format") {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fiber.Map{
					"message": "unsupported file format (PDF, DOCX, PPTX only)", "type": "unsupported_format",
				}})
			}
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": fiber.Map{
				"message": "parser error: " + err.Error(), "type": "parser_error",
			}})
		}

		if piiType, found := middleware.ScanForPII(parseResult.Text); found {
			piiRecorder.RecordViolation(user.TenantID, user.UserID, piiType, "blocked", "/v1/files/chat")
			return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": fiber.Map{
				"message": "file blocked: PII detected (" + piiType + ")", "type": "pii_blocked",
			}})
		}

		start := time.Now()
		body := adapter.BuildFilePrompt(modelName, parseResult.Text, message)
		result, usage, err := adapter.Forward(c.Context(), body)
		if err != nil {
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": fiber.Map{
				"message": "upstream error: " + err.Error(), "type": "upstream_error",
			}})
		}

		responseMS := int(time.Since(start).Milliseconds())
		usageRecorder.Record(&storage.UsageRecord{
			TenantID:         user.TenantID,
			UserID:           user.UserID,
			ModelConfigID:    modelCfg.ID,
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			SCUCost:          tokenizer.ToSCU(usage.PromptTokens, usage.CompletionTokens, modelCfg.SCURate),
			USDCost:          0,
			ResponseMS:       responseMS,
		})

		for k, vs := range result.Headers {
			for _, v := range vs {
				c.Set(k, v)
			}
		}
		return c.Status(result.StatusCode).Send(result.Body)
	}
}
