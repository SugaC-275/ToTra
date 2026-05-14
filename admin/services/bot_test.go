package services_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/totra/admin/crypto"
	"github.com/yourorg/totra/admin/services"
)

const testBotEncKey = "0000000000000000000000000000000000000000000000000000000000000000"

func TestSendBotMessage_Feishu(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	err := services.SendBotMessage("feishu", srv.URL, "hello feishu")
	require.NoError(t, err)

	var payload map[string]interface{}
	require.NoError(t, json.NewDecoder(bytes.NewReader(received)).Decode(&payload))
	assert.Equal(t, "text", payload["msg_type"])
	content, _ := payload["content"].(map[string]interface{})
	assert.Equal(t, "hello feishu", content["text"])
}

func TestSendBotMessage_Slack(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := services.SendBotMessage("slack", srv.URL, "hello slack")
	require.NoError(t, err)

	var payload map[string]interface{}
	require.NoError(t, json.NewDecoder(bytes.NewReader(received)).Decode(&payload))
	assert.Equal(t, "hello slack", payload["text"])
}

func TestSendBotMessage_UnknownPlatform(t *testing.T) {
	err := services.SendBotMessage("discord", "http://example.com", "msg")
	assert.Error(t, err)
}

func TestSendBotMessage_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := services.SendBotMessage("slack", srv.URL, "msg")
	assert.Error(t, err)
}

func TestEncryptDecryptBotURL(t *testing.T) {
	url := "https://open.feishu.cn/open-apis/bot/v2/hook/abc123"
	enc, err := crypto.Encrypt(url, testBotEncKey)
	require.NoError(t, err)
	assert.NotEqual(t, url, enc)

	dec, err := crypto.Decrypt(enc, testBotEncKey)
	require.NoError(t, err)
	assert.Equal(t, url, dec)
}
