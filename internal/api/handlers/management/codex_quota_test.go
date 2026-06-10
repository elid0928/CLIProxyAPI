package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestCodexQuotaCheckScansAuthDirAndFiltersNonCodex(t *testing.T) {
	var gotAuthorization string
	var gotAccountID string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		gotAccountID = r.Header.Get("Chatgpt-Account-Id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"plan_type": "free",
			"rate_limit": {
				"limit_reached": false,
				"primary_window": {"used_percent": 12.345, "reset_after_seconds": 3600},
				"secondary_window": {"usage_percent": 66.6, "reset_at": "2026-06-10T12:00:00Z"}
			}
		}`))
	}))
	defer upstream.Close()

	oldUsageURL := codexQuotaUsageURL
	codexQuotaUsageURL = upstream.URL
	t.Cleanup(func() { codexQuotaUsageURL = oldUsageURL })

	authDir := t.TempDir()
	writeTestFile(t, filepath.Join(authDir, "1-codex.json"), `{
		"type": "codex",
		"name": "codex-user",
		"email": "codex@example.com",
		"access_token": "access-token",
		"account_id": "acct_123"
	}`)
	writeTestFile(t, filepath.Join(authDir, "2-claude.json"), `{
		"type": "claude",
		"access_token": "claude-token",
		"account_id": "acct_claude"
	}`)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, nil)
	router.GET("/quota", h.GetCodexQuota)

	req := httptest.NewRequest(http.MethodGet, "/quota", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gotAuthorization != "Bearer access-token" {
		t.Fatalf("Authorization header = %q, want Bearer access-token", gotAuthorization)
	}
	if gotAccountID != "acct_123" {
		t.Fatalf("Chatgpt-Account-Id header = %q, want acct_123", gotAccountID)
	}

	var body struct {
		Files []codexQuotaResult `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(body.Files) != 1 {
		t.Fatalf("files len = %d, want 1: %s", len(body.Files), rec.Body.String())
	}
	got := body.Files[0]
	if got.Name != "1-codex.json" {
		t.Fatalf("name = %q, want 1-codex.json", got.Name)
	}
	if got.Status != "ok" {
		t.Fatalf("status = %q, want ok", got.Status)
	}
	if got.Account != "acct_123" {
		t.Fatalf("account = %q, want acct_123", got.Account)
	}
	if got.Email != "codex@example.com" {
		t.Fatalf("email = %q, want codex@example.com", got.Email)
	}
	if got.Plan != "free" {
		t.Fatalf("plan = %q, want free", got.Plan)
	}
	if got.FiveHour == nil {
		t.Fatal("five_hour is nil")
	}
	if got.FiveHour.UsedPercent != 12.35 {
		t.Fatalf("five_hour.used_percent = %v, want 12.35", got.FiveHour.UsedPercent)
	}
	if got.FiveHour.RemainingPercent != 87.65 {
		t.Fatalf("five_hour.remaining_percent = %v, want 87.65", got.FiveHour.RemainingPercent)
	}
	if got.Weekly == nil {
		t.Fatal("weekly is nil")
	}
	if got.Weekly.UsedPercent != 66.6 {
		t.Fatalf("weekly.used_percent = %v, want 66.6", got.Weekly.UsedPercent)
	}
	if got.Weekly.RemainingPercent != 33.4 {
		t.Fatalf("weekly.remaining_percent = %v, want 33.4", got.Weekly.RemainingPercent)
	}
	if got.Weekly.ResetAt != "2026-06-10T12:00:00Z" {
		t.Fatalf("weekly.reset_at = %q, want 2026-06-10T12:00:00Z", got.Weekly.ResetAt)
	}
}

func writeTestFile(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
