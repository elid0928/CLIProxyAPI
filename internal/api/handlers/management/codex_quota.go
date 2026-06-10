package management

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	defaultCodexQuotaUsageURL = "https://chatgpt.com/backend-api/wham/usage"
	codexQuotaClientVersion   = "0.101.0"
	codexQuotaUserAgent       = "codex_cli_rs/0.101.0 (Mac OS 26.0.1; arm64) Apple_Terminal/464"
)

var codexQuotaUsageURL = defaultCodexQuotaUsageURL

type codexQuotaAuthFile struct {
	Path             string              `json:"path,omitempty"`
	Type             string              `json:"type,omitempty"`
	Name             string              `json:"name,omitempty"`
	Email            string              `json:"email,omitempty"`
	AccessToken      string              `json:"access_token,omitempty"`
	IDToken          string              `json:"id_token,omitempty"`
	AccountID        string              `json:"account_id,omitempty"`
	ChatGPTAccountID string              `json:"chatgpt_account_id,omitempty"`
	PlanType         string              `json:"plan_type,omitempty"`
	ChatGPTPlanType  string              `json:"chatgpt_plan_type,omitempty"`
	Disabled         bool                `json:"disabled,omitempty"`
	TokenData        *codexQuotaAuthFile `json:"token_data,omitempty"`
}

type codexQuotaResult struct {
	Name     string                 `json:"name"`
	Path     string                 `json:"path"`
	Account  string                 `json:"account"`
	Email    string                 `json:"email,omitempty"`
	Plan     string                 `json:"plan,omitempty"`
	Status   string                 `json:"status"`
	Error    string                 `json:"error,omitempty"`
	LimitHit bool                   `json:"limit_reached,omitempty"`
	FiveHour *codexQuotaLimitWindow `json:"five_hour,omitempty"`
	Weekly   *codexQuotaLimitWindow `json:"weekly,omitempty"`
}

type codexQuotaLimitWindow struct {
	UsedPercent      float64 `json:"used_percent"`
	RemainingPercent float64 `json:"remaining_percent"`
	ResetAfter       string  `json:"reset_after,omitempty"`
	ResetAt          string  `json:"reset_at,omitempty"`
}

type codexQuotaParsedUsage struct {
	Plan     string
	LimitHit bool
	FiveHour *codexQuotaLimitWindow
	Weekly   *codexQuotaLimitWindow
}

// GetCodexQuota scans the configured auth directory and checks Codex OAuth quota.
func (h *Handler) GetCodexQuota(c *gin.Context) {
	if h == nil || h.cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "handler not initialized"})
		return
	}
	authDir := strings.TrimSpace(h.cfg.AuthDir)
	if authDir == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "auth dir is not configured"})
		return
	}

	includeDisabled := parseCodexQuotaBoolQuery(c.Query("include_disabled")) || parseCodexQuotaBoolQuery(c.Query("include-disabled"))
	paths, err := collectCodexQuotaAuthPaths(authDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to read auth dir: %v", err)})
		return
	}

	client := &http.Client{Transport: h.apiCallTransport(nil)}
	results := make([]codexQuotaResult, 0, len(paths))
	for _, path := range paths {
		result, ok := h.inspectCodexQuotaPath(c.Request.Context(), client, path, includeDisabled)
		if ok {
			results = append(results, result)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"files": results,
		"count": len(results),
	})
}

func collectCodexQuotaAuthPaths(authDir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(authDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".json") {
			paths = append(paths, path)
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func (h *Handler) inspectCodexQuotaPath(ctx context.Context, client *http.Client, path string, includeDisabled bool) (codexQuotaResult, bool) {
	auth, err := loadCodexQuotaAuth(path)
	if err != nil {
		return codexQuotaResult{Name: filepath.Base(path), Path: path, Status: "error", Error: err.Error()}, true
	}
	if !strings.EqualFold(strings.TrimSpace(auth.Type), "codex") {
		return codexQuotaResult{}, false
	}
	if auth.Disabled && !includeDisabled {
		return codexQuotaResult{}, false
	}

	result := codexQuotaResult{
		Name:    filepath.Base(path),
		Path:    path,
		Account: auth.accountID(),
		Email:   auth.email(),
		Plan:    auth.planType(),
		Status:  "ok",
	}
	if strings.TrimSpace(auth.AccessToken) == "" {
		result.Status = "error"
		result.Error = "missing access_token"
		return result, true
	}
	accountID := auth.accountID()
	if accountID == "" {
		result.Status = "error"
		result.Error = "missing account_id/chatgpt_account_id"
		return result, true
	}

	usage, err := queryCodexQuotaUsage(ctx, client, codexQuotaUsageURL, auth.AccessToken, accountID)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result, true
	}
	result.LimitHit = usage.LimitHit
	if usage.Plan != "" {
		result.Plan = usage.Plan
	}
	result.FiveHour = usage.FiveHour
	result.Weekly = usage.Weekly
	return result, true
}

func loadCodexQuotaAuth(path string) (*codexQuotaAuthFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var auth codexQuotaAuthFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, err
	}
	if auth.TokenData != nil {
		auth.mergeTokenData(auth.TokenData)
	}
	if auth.IDToken != "" {
		claims, _ := parseCodexQuotaIDToken(auth.IDToken)
		auth.ChatGPTAccountID = codexQuotaFirstNonEmpty(auth.ChatGPTAccountID, codexQuotaStringFromPath(claims, "https://api.openai.com/auth", "chatgpt_account_id"))
		if auth.ChatGPTPlanType == "" {
			auth.ChatGPTPlanType = codexQuotaStringFromPath(claims, "https://api.openai.com/auth", "chatgpt_plan_type")
		}
		if auth.Email == "" {
			auth.Email = codexQuotaStringFromPath(claims, "email")
		}
	}
	return &auth, nil
}

func (a *codexQuotaAuthFile) mergeTokenData(token *codexQuotaAuthFile) {
	if token == nil {
		return
	}
	a.AccessToken = codexQuotaFirstNonEmpty(a.AccessToken, token.AccessToken)
	a.IDToken = codexQuotaFirstNonEmpty(a.IDToken, token.IDToken)
	a.AccountID = codexQuotaFirstNonEmpty(a.AccountID, token.AccountID)
	a.ChatGPTAccountID = codexQuotaFirstNonEmpty(a.ChatGPTAccountID, token.ChatGPTAccountID)
	a.Email = codexQuotaFirstNonEmpty(a.Email, token.Email)
	a.PlanType = codexQuotaFirstNonEmpty(a.PlanType, token.PlanType)
	a.ChatGPTPlanType = codexQuotaFirstNonEmpty(a.ChatGPTPlanType, token.ChatGPTPlanType)
}

func (a *codexQuotaAuthFile) accountID() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(codexQuotaFirstNonEmpty(a.AccountID, a.ChatGPTAccountID))
}

func (a *codexQuotaAuthFile) email() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(codexQuotaFirstNonEmpty(a.Email, a.Name))
}

func (a *codexQuotaAuthFile) planType() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(codexQuotaFirstNonEmpty(a.PlanType, a.ChatGPTPlanType))
}

func queryCodexQuotaUsage(ctx context.Context, client *http.Client, usageURL string, accessToken string, accountID string) (codexQuotaParsedUsage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return codexQuotaParsedUsage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Chatgpt-Account-Id", accountID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", codexQuotaUserAgent)
	req.Header.Set("Version", codexQuotaClientVersion)
	req.Header.Set("Originator", "codex_cli_rs")

	resp, err := client.Do(req)
	if err != nil {
		return codexQuotaParsedUsage{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return codexQuotaParsedUsage{}, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return codexQuotaParsedUsage{}, fmt.Errorf("unauthorized (401)")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return codexQuotaParsedUsage{}, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(limitCodexQuotaBytes(body, 512))))
	}
	return parseCodexQuotaUsage(body)
}

func parseCodexQuotaUsage(body []byte) (codexQuotaParsedUsage, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return codexQuotaParsedUsage{}, err
	}
	rateLimit, _ := raw["rate_limit"].(map[string]any)
	if rateLimit == nil {
		return codexQuotaParsedUsage{Plan: codexQuotaStringValue(raw["plan_type"])}, nil
	}
	return codexQuotaParsedUsage{
		Plan:     codexQuotaStringValue(raw["plan_type"]),
		LimitHit: codexQuotaBoolValue(rateLimit["limit_reached"]),
		FiveHour: parseCodexQuotaWindowMap(codexQuotaFirstMap(rateLimit["primary_window"], rateLimit["five_hour_window"], rateLimit["five_hour"])),
		Weekly:   parseCodexQuotaWindowMap(codexQuotaFirstMap(rateLimit["secondary_window"], rateLimit["weekly_window"], rateLimit["weekly"], rateLimit["week_window"])),
	}, nil
}

func parseCodexQuotaWindowMap(window map[string]any) *codexQuotaLimitWindow {
	if window == nil {
		return nil
	}
	used, ok := codexQuotaFloatValue(window["used_percent"])
	if !ok {
		used, ok = codexQuotaFloatValue(window["usage_percent"])
	}
	if !ok {
		return nil
	}
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	used = roundCodexQuota2(used)
	w := &codexQuotaLimitWindow{
		UsedPercent:      used,
		RemainingPercent: roundCodexQuota2(100 - used),
	}
	if seconds, ok := codexQuotaFirstFloat(window, "reset_after_seconds", "seconds_until_reset", "reset_in_seconds"); ok {
		duration := time.Duration(seconds * float64(time.Second))
		w.ResetAfter = duration.Round(time.Second).String()
		w.ResetAt = time.Now().Add(duration).UTC().Format(time.RFC3339)
	}
	if resetAt := codexQuotaFirstString(window, "reset_at", "resets_at", "end_time"); resetAt != "" {
		w.ResetAt = resetAt
	}
	return w
}

func parseCodexQuotaIDToken(idToken string) (map[string]any, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT token")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func codexQuotaFirstMap(values ...any) map[string]any {
	for _, value := range values {
		if m, ok := value.(map[string]any); ok {
			return m
		}
	}
	return nil
}

func codexQuotaFirstFloat(m map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		if v, ok := codexQuotaFloatValue(m[key]); ok {
			return v, true
		}
	}
	return 0, false
}

func codexQuotaFirstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s := strings.TrimSpace(codexQuotaStringValue(m[key])); s != "" {
			return s
		}
	}
	return ""
}

func codexQuotaFloatValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(v), "%"), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func codexQuotaBoolValue(value any) bool {
	v, _ := value.(bool)
	return v
}

func codexQuotaStringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func codexQuotaStringFromPath(root map[string]any, path ...string) string {
	var current any = root
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[key]
	}
	return codexQuotaStringValue(current)
}

func codexQuotaFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseCodexQuotaBoolQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func limitCodexQuotaBytes(data []byte, max int) []byte {
	if len(data) <= max {
		return data
	}
	return data[:max]
}

func roundCodexQuota2(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
}
