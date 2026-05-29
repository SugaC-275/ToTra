package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

type FilesModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

func RegisterFilesRoutes(router fiber.Router, store *storage.FilesStore, lookup FilesModelLookup) {
	router.Post("/files", uploadFile(store, lookup))
	router.Get("/files", listFiles(store))
	router.Get("/files/:id", retrieveFile(store))
	router.Delete("/files/:id", deleteFile(store))
}

func uploadFile(store *storage.FilesStore, lookup FilesModelLookup) fiber.Handler {
	client := &http.Client{Timeout: 120 * time.Second}
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}

		model := c.FormValue("model")
		purpose := c.FormValue("purpose")
		if model == "" || purpose == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "model and purpose are required"},
			})
		}

		modelCfg, err := lookup.GetByName(c.Context(), user.TenantID, model)
		if err != nil || modelCfg == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": fmt.Sprintf("model %q not configured", model)},
			})
		}

		fileHeader, err := c.FormFile("file")
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fiber.Map{"message": "file is required"}})
		}
		f, err := fileHeader.Open()
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to read file"})
		}
		defer f.Close()
		fileContent, err := io.ReadAll(f)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to read file"})
		}

		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("purpose", purpose)
		fw, _ := mw.CreateFormFile("file", fileHeader.Filename)
		_, _ = fw.Write(fileContent)
		mw.Close()

		endpoint := strings.TrimRight(modelCfg.BaseURL, "/") + "/files"
		req, err := http.NewRequestWithContext(c.Context(), http.MethodPost, endpoint, &body)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "request error"})
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		if modelCfg.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+modelCfg.APIKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			slog.Error("files upload: upstream", "err", err)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream unavailable"})
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 400 {
			return c.Status(resp.StatusCode).Send(out)
		}

		fileID := "file-" + uuid.New().String()
		uploadedFile := &storage.UploadedFile{
			ID:            fileID,
			TenantID:      user.TenantID,
			UserID:        user.UserID,
			ModelConfigID: modelCfg.ID,
			Filename:      fileHeader.Filename,
			Purpose:       purpose,
			Bytes:         int64(len(fileContent)),
			Status:        "uploaded",
			Provider:      modelCfg.Provider,
		}
		if err := store.Create(c.Context(), uploadedFile); err != nil {
			slog.Error("files upload: store", "err", err)
		}
		return c.Status(fiber.StatusCreated).JSON(uploadedFile)
	}
}

func listFiles(store *storage.FilesStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		files, err := store.List(c.Context(), user.TenantID, c.Query("purpose"), 20)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if files == nil {
			files = []*storage.UploadedFile{}
		}
		return c.JSON(fiber.Map{"object": "list", "data": files})
	}
}

func retrieveFile(store *storage.FilesStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		file, err := store.Get(c.Context(), user.TenantID, c.Params("id"))
		if err != nil || file == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "file not found"})
		}
		return c.JSON(file)
	}
}

func deleteFile(store *storage.FilesStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if err := store.Delete(c.Context(), user.TenantID, c.Params("id")); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"id": c.Params("id"), "object": "file", "deleted": true})
	}
}
