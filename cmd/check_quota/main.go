package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

const (
	defaultUsageURL = "https://chatgpt.com/backend-api/wham/usage"
	defaultTokenURL = "https://auth.openai.com/oauth/token"

	codexClientID      = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexClientVersion = "0.101.0"
	codexUserAgent     = "codex_cli_rs/0.101.0 (Mac OS 26.0.1; arm64) Apple_Terminal/464"
)

type options struct {
	path            string
	usageURL        string
	tokenURL        string
	timeout         time.Duration
	jsonOutput      bool
	includeDisabled bool
	refreshExpired  bool
	writeRefreshed  bool
}

type authFile struct {
	Path             string    `json:"path,omitempty"`
	Type             string    `json:"type,omitempty"`
	Name             string    `json:"name,omitempty"`
	Email            string    `json:"email,omitempty"`
	AccessToken      string    `json:"access_token,omitempty"`
	RefreshToken     string    `json:"refresh_token,omitempty"`
	IDToken          string    `json:"id_token,omitempty"`
	AccountID        string    `json:"account_id,omitempty"`
	ChatGPTAccountID string    `json:"chatgpt_account_id,omitempty"`
	PlanType         string    `json:"plan_type,omitempty"`
	ChatGPTPlanType  string    `json:"chatgpt_plan_type,omitempty"`
	Expired          string    `json:"expired,omitempty"`
	LastRefresh      string    `json:"last_refresh,omitempty"`
	Disabled         bool      `json:"disabled,omitempty"`
	TokenData        *authFile `json:"token_data,omitempty"`
	raw              map[string]any
}

type usageResult struct {
	Path      string       `json:"path"`
	Account   string       `json:"account"`
	Email     string       `json:"email,omitempty"`
	Plan      string       `json:"plan,omitempty"`
	Status    string       `json:"status"`
	Error     string       `json:"error,omitempty"`
	LimitHit  bool         `json:"limit_reached,omitempty"`
	FiveHour  *limitWindow `json:"five_hour,omitempty"`
	Weekly    *limitWindow `json:"weekly,omitempty"`
	Refreshed bool         `json:"refreshed,omitempty"`
}

type limitWindow struct {
	UsedPercent      float64 `json:"used_percent"`
	RemainingPercent float64 `json:"remaining_percent"`
	ResetAfter       string  `json:"reset_after,omitempty"`
	ResetAt          string  `json:"reset_at,omitempty"`
}

func main() {
	opts := parseFlags()
	if err := run(context.Background(), opts, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.path, "path", "", "auth JSON file or directory containing auth JSON files")
	flag.StringVar(&opts.usageURL, "url", defaultUsageURL, "Codex usage endpoint")
	flag.StringVar(&opts.tokenURL, "token-url", defaultTokenURL, "OpenAI OAuth token endpoint")
	flag.DurationVar(&opts.timeout, "timeout", 30*time.Second, "per-account request timeout")
	flag.BoolVar(&opts.jsonOutput, "json", false, "print JSON instead of a table")
	flag.BoolVar(&opts.includeDisabled, "include-disabled", false, "include auth files with disabled=true")
	flag.BoolVar(&opts.refreshExpired, "refresh-expired", false, "refresh expired access tokens before querying")
	flag.BoolVar(&opts.writeRefreshed, "write-refreshed", false, "write refreshed tokens back to the auth JSON file; implies -refresh-expired")
	flag.Parse()

	if opts.path == "" && flag.NArg() > 0 {
		opts.path = flag.Arg(0)
	}
	if opts.path == "" {
		opts.path = "."
	}
	if opts.writeRefreshed {
		opts.refreshExpired = true
	}
	return opts
}

func run(parent context.Context, opts options, out io.Writer) error {
	paths, err := collectAuthPaths(opts.path)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no JSON auth files found under %s", opts.path)
	}

	client := &http.Client{}
	results := make([]usageResult, 0, len(paths))
	for _, path := range paths {
		result := inspectPath(parent, client, opts, path)
		if result.Status != "skipped" || opts.includeDisabled {
			results = append(results, result)
		}
	}

	if opts.jsonOutput {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}
	writeTable(out, results)
	return nil
}

func collectAuthPaths(input string) ([]string, error) {
	info, err := os.Stat(input)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if strings.EqualFold(filepath.Ext(input), ".json") {
			return []string{input}, nil
		}
		return nil, fmt.Errorf("%s is not a JSON file", input)
	}

	var paths []string
	err = filepath.WalkDir(input, func(path string, d os.DirEntry, err error) error {
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

func inspectPath(parent context.Context, client *http.Client, opts options, path string) usageResult {
	auth, err := loadAuth(path)
	if err != nil {
		return usageResult{Path: path, Status: "error", Error: err.Error()}
	}
	auth.Path = path
	result := usageResult{
		Path:    path,
		Account: auth.accountID(),
		Email:   auth.email(),
		Plan:    auth.planType(),
		Status:  "ok",
	}
	if auth.Disabled && !opts.includeDisabled {
		result.Status = "skipped"
		result.Error = "disabled auth"
		return result
	}

	if opts.refreshExpired && auth.isExpired(time.Now()) {
		refreshed, err := refreshAuth(parent, client, opts, auth)
		if err != nil {
			result.Status = "error"
			result.Error = "refresh expired token: " + err.Error()
			return result
		}
		auth = refreshed
		result.Refreshed = true
		result.Account = auth.accountID()
		result.Email = auth.email()
		result.Plan = auth.planType()
		if opts.writeRefreshed {
			if err := saveAuth(path, auth); err != nil {
				result.Status = "error"
				result.Error = "write refreshed token: " + err.Error()
				return result
			}
		}
	}

	if strings.TrimSpace(auth.AccessToken) == "" {
		result.Status = "error"
		result.Error = "missing access_token"
		return result
	}
	accountID := auth.accountID()
	if accountID == "" {
		result.Status = "error"
		result.Error = "missing account_id/chatgpt_account_id"
		return result
	}

	ctx, cancel := context.WithTimeout(parent, opts.timeout)
	defer cancel()
	usage, err := queryUsage(ctx, client, opts.usageURL, auth.AccessToken, accountID)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	result.LimitHit = usage.LimitHit
	if usage.Plan != "" {
		result.Plan = usage.Plan
	}
	result.FiveHour = usage.FiveHour
	result.Weekly = usage.Weekly
	return result
}

func loadAuth(path string) (*authFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var auth authFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err == nil {
		auth.raw = raw
	}
	if auth.TokenData != nil {
		auth.mergeTokenData(auth.TokenData)
	}
	if auth.IDToken != "" {
		claims, _ := parseIDToken(auth.IDToken)
		auth.ChatGPTAccountID = firstNonEmpty(auth.ChatGPTAccountID, stringFromPath(claims, "https://api.openai.com/auth", "chatgpt_account_id"))
		if auth.ChatGPTPlanType == "" {
			auth.ChatGPTPlanType = stringFromPath(claims, "https://api.openai.com/auth", "chatgpt_plan_type")
		}
		if auth.Email == "" {
			auth.Email = stringFromPath(claims, "email")
		}
	}
	return &auth, nil
}

func saveAuth(path string, auth *authFile) error {
	if auth == nil {
		return errors.New("nil auth")
	}
	raw := auth.raw
	if raw == nil {
		raw = map[string]any{}
	}
	raw["type"] = firstNonEmpty(auth.Type, "codex")
	raw["access_token"] = auth.AccessToken
	raw["refresh_token"] = auth.RefreshToken
	raw["id_token"] = auth.IDToken
	raw["account_id"] = auth.AccountID
	raw["chatgpt_account_id"] = firstNonEmpty(auth.ChatGPTAccountID, auth.AccountID)
	raw["email"] = auth.Email
	raw["expired"] = auth.Expired
	raw["last_refresh"] = time.Now().UTC().Format(time.RFC3339)
	if plan := auth.planType(); plan != "" {
		raw["plan_type"] = plan
		raw["chatgpt_plan_type"] = plan
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0600)
}

func (a *authFile) mergeTokenData(token *authFile) {
	if token == nil {
		return
	}
	a.AccessToken = firstNonEmpty(a.AccessToken, token.AccessToken)
	a.RefreshToken = firstNonEmpty(a.RefreshToken, token.RefreshToken)
	a.IDToken = firstNonEmpty(a.IDToken, token.IDToken)
	a.AccountID = firstNonEmpty(a.AccountID, token.AccountID)
	a.ChatGPTAccountID = firstNonEmpty(a.ChatGPTAccountID, token.ChatGPTAccountID)
	a.Email = firstNonEmpty(a.Email, token.Email)
	a.Expired = firstNonEmpty(a.Expired, token.Expired)
	a.PlanType = firstNonEmpty(a.PlanType, token.PlanType)
	a.ChatGPTPlanType = firstNonEmpty(a.ChatGPTPlanType, token.ChatGPTPlanType)
}

func (a *authFile) accountID() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(firstNonEmpty(a.AccountID, a.ChatGPTAccountID))
}

func (a *authFile) email() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(firstNonEmpty(a.Email, a.Name))
}

func (a *authFile) planType() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(firstNonEmpty(a.PlanType, a.ChatGPTPlanType))
}

func (a *authFile) isExpired(now time.Time) bool {
	if a == nil || strings.TrimSpace(a.Expired) == "" {
		return false
	}
	exp, err := parseTime(a.Expired)
	if err != nil {
		return false
	}
	return !now.Before(exp.Add(-1 * time.Minute))
}

type parsedUsage struct {
	Plan     string
	LimitHit bool
	FiveHour *limitWindow
	Weekly   *limitWindow
}

func queryUsage(ctx context.Context, client *http.Client, usageURL, accessToken, accountID string) (parsedUsage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return parsedUsage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Chatgpt-Account-Id", accountID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", codexUserAgent)
	req.Header.Set("Version", codexClientVersion)
	req.Header.Set("Originator", "codex_cli_rs")

	resp, err := client.Do(req)
	if err != nil {
		return parsedUsage{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return parsedUsage{}, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return parsedUsage{}, fmt.Errorf("unauthorized (401)")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parsedUsage{}, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(limitBytes(body, 512))))
	}
	return parseUsage(body)
}

func parseUsage(body []byte) (parsedUsage, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return parsedUsage{}, err
	}
	rateLimit, _ := raw["rate_limit"].(map[string]any)
	if rateLimit == nil {
		return parsedUsage{Plan: stringValue(raw["plan_type"])}, nil
	}
	return parsedUsage{
		Plan:     stringValue(raw["plan_type"]),
		LimitHit: boolValue(rateLimit["limit_reached"]),
		FiveHour: parseWindowMap(firstMap(rateLimit["primary_window"], rateLimit["five_hour_window"], rateLimit["five_hour"])),
		Weekly:   parseWindowMap(firstMap(rateLimit["secondary_window"], rateLimit["weekly_window"], rateLimit["weekly"], rateLimit["week_window"])),
	}, nil
}

func parseWindowMap(window map[string]any) *limitWindow {
	if window == nil {
		return nil
	}
	used, ok := floatValue(window["used_percent"])
	if !ok {
		used, ok = floatValue(window["usage_percent"])
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
	used = round2(used)
	w := &limitWindow{
		UsedPercent:      used,
		RemainingPercent: round2(100 - used),
	}
	if seconds, ok := firstFloat(window, "reset_after_seconds", "seconds_until_reset", "reset_in_seconds"); ok {
		duration := time.Duration(seconds * float64(time.Second))
		w.ResetAfter = duration.Round(time.Second).String()
		w.ResetAt = time.Now().Add(duration).UTC().Format(time.RFC3339)
	}
	if resetAt := firstString(window, "reset_at", "resets_at", "end_time"); resetAt != "" {
		w.ResetAt = resetAt
	}
	return w
}

func refreshAuth(parent context.Context, client *http.Client, opts options, auth *authFile) (*authFile, error) {
	if strings.TrimSpace(auth.RefreshToken) == "" {
		return nil, errors.New("missing refresh_token")
	}
	ctx, cancel := context.WithTimeout(parent, opts.timeout)
	defer cancel()

	form := url.Values{
		"client_id":     {codexClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {auth.RefreshToken},
		"scope":         {"openid profile email"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(limitBytes(body, 512))))
	}

	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return nil, errors.New("refresh response missing access_token")
	}

	updated := *auth
	updated.AccessToken = token.AccessToken
	updated.RefreshToken = firstNonEmpty(token.RefreshToken, auth.RefreshToken)
	updated.IDToken = firstNonEmpty(token.IDToken, auth.IDToken)
	if token.ExpiresIn > 0 {
		updated.Expired = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	if updated.IDToken != "" {
		claims, _ := parseIDToken(updated.IDToken)
		updated.AccountID = firstNonEmpty(stringFromPath(claims, "https://api.openai.com/auth", "chatgpt_account_id"), updated.AccountID)
		updated.ChatGPTAccountID = firstNonEmpty(updated.ChatGPTAccountID, updated.AccountID)
		updated.ChatGPTPlanType = firstNonEmpty(stringFromPath(claims, "https://api.openai.com/auth", "chatgpt_plan_type"), updated.ChatGPTPlanType)
		updated.Email = firstNonEmpty(stringFromPath(claims, "email"), updated.Email)
	}
	return &updated, nil
}

func parseIDToken(idToken string) (map[string]any, error) {
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

func writeTable(out io.Writer, results []usageResult) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FILE\tACCOUNT\tPLAN\t5H USED\t5H LEFT\t5H RESET\tWEEK USED\tWEEK LEFT\tWEEK RESET\tSTATUS")
	for _, r := range results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Path,
			shortAccount(r.Account),
			emptyDash(r.Plan),
			percent(r.FiveHour, true),
			percent(r.FiveHour, false),
			resetText(r.FiveHour),
			percent(r.Weekly, true),
			percent(r.Weekly, false),
			resetText(r.Weekly),
			statusText(r),
		)
	}
	tw.Flush()
}

func percent(w *limitWindow, used bool) string {
	if w == nil {
		return "-"
	}
	if used {
		return fmt.Sprintf("%.2f%%", w.UsedPercent)
	}
	return fmt.Sprintf("%.2f%%", w.RemainingPercent)
}

func resetText(w *limitWindow) string {
	if w == nil {
		return "-"
	}
	if w.ResetAfter != "" {
		return w.ResetAfter
	}
	if w.ResetAt != "" {
		return w.ResetAt
	}
	return "-"
}

func statusText(r usageResult) string {
	if r.Error != "" {
		return r.Status + ": " + r.Error
	}
	if r.LimitHit {
		return "limit_reached"
	}
	if r.Refreshed {
		return "ok/refreshed"
	}
	return r.Status
}

func firstMap(values ...any) map[string]any {
	for _, value := range values {
		if m, ok := value.(map[string]any); ok {
			return m
		}
	}
	return nil
}

func firstFloat(m map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		if v, ok := floatValue(m[key]); ok {
			return v, true
		}
	}
	return 0, false
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s := strings.TrimSpace(stringValue(m[key])); s != "" {
			return s
		}
	}
	return ""
}

func floatValue(value any) (float64, bool) {
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

func boolValue(value any) bool {
	v, _ := value.(bool)
	return v
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func stringFromPath(root map[string]any, path ...string) string {
	var current any = root
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[key]
	}
	return stringValue(current)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func shortAccount(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return emptyDash(value)
	}
	return value[:8] + "..." + value[len(value)-4:]
}

func parseTime(value string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02T15:04:05.000Z", value)
}

func limitBytes(data []byte, max int) []byte {
	if len(data) <= max {
		return data
	}
	return data[:max]
}

func round2(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
}
