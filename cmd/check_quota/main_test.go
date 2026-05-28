package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseUsageExtractsFiveHourAndWeekly(t *testing.T) {
	body := []byte(`{
		"plan_type": "plus",
		"rate_limit": {
			"limit_reached": false,
			"primary_window": {"used_percent": 12.345, "reset_after_seconds": 60},
			"secondary_window": {"used_percent": "67.89", "reset_at": "2026-05-29T00:00:00Z"}
		}
	}`)

	got, err := parseUsage(body)
	if err != nil {
		t.Fatalf("parseUsage() error = %v", err)
	}
	if got.Plan != "plus" {
		t.Fatalf("Plan = %q, want plus", got.Plan)
	}
	if got.FiveHour == nil || got.FiveHour.UsedPercent != 12.35 || got.FiveHour.RemainingPercent != 87.65 {
		t.Fatalf("FiveHour = %#v, want rounded 12.35 used and 87.65 remaining", got.FiveHour)
	}
	if got.FiveHour.ResetAfter != "1m0s" {
		t.Fatalf("FiveHour.ResetAfter = %q, want 1m0s", got.FiveHour.ResetAfter)
	}
	if got.Weekly == nil || got.Weekly.UsedPercent != 67.89 || got.Weekly.RemainingPercent != 32.11 {
		t.Fatalf("Weekly = %#v, want 67.89 used and 32.11 remaining", got.Weekly)
	}
	if got.Weekly.ResetAt != "2026-05-29T00:00:00Z" {
		t.Fatalf("Weekly.ResetAt = %q", got.Weekly.ResetAt)
	}
}

func TestLoadAuthSupportsJWTFallback(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "1-codex.json")
	token := testJWT(map[string]any{
		"email": "user@example.com",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct_123",
			"chatgpt_plan_type":  "plus",
		},
	})
	if err := os.WriteFile(authPath, []byte(`{"type":"codex","access_token":"access","id_token":"`+token+`"}`), 0600); err != nil {
		t.Fatal(err)
	}

	auth, err := loadAuth(authPath)
	if err != nil {
		t.Fatalf("loadAuth() error = %v", err)
	}
	if auth.accountID() != "acct_123" {
		t.Fatalf("accountID() = %q, want acct_123", auth.accountID())
	}
	if auth.email() != "user@example.com" {
		t.Fatalf("email() = %q, want user@example.com", auth.email())
	}
	if auth.planType() != "plus" {
		t.Fatalf("planType() = %q, want plus", auth.planType())
	}
}

func TestCollectAuthPathsAcceptsDirectoryAndJSONOnly(t *testing.T) {
	dir := t.TempDir()
	authPath := filepath.Join(dir, "1-codex.json")
	if err := os.WriteFile(authPath, []byte(`{"type":"codex","access_token":"access","account_id":"acct_123","email":"user@example.com","plan_type":"plus"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "not-json.txt"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}

	paths, err := collectAuthPaths(dir)
	if err != nil {
		t.Fatalf("collectAuthPaths() error = %v", err)
	}
	if len(paths) != 1 || paths[0] != authPath {
		t.Fatalf("collectAuthPaths() = %#v, want only %s", paths, authPath)
	}

	paths, err = collectAuthPaths(authPath)
	if err != nil {
		t.Fatalf("collectAuthPaths(file) error = %v", err)
	}
	if len(paths) != 1 || paths[0] != authPath {
		t.Fatalf("collectAuthPaths(file) = %#v, want %s", paths, authPath)
	}
}

func testJWT(claims map[string]any) string {
	header := map[string]any{"alg": "none", "typ": "JWT"}
	return base64.RawURLEncoding.EncodeToString(mustJSON(header)) + "." +
		base64.RawURLEncoding.EncodeToString(mustJSON(claims)) + ".sig"
}

func mustJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
