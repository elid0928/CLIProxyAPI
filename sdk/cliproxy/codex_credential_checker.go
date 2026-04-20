package cliproxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	codexauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/codex"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"

	log "github.com/sirupsen/logrus"
)

const (
	codexCredentialCheckTimeout = 30 * time.Second

	codexWhamUsageURL       = "https://chatgpt.com/backend-api/wham/usage"
	codexCheckClientVersion = "0.101.0"
	codexCheckUserAgent     = "codex_cli_rs/0.101.0 (Mac OS 26.0.1; arm64) Apple_Terminal/464"
)

type codexCredentialInspector interface {
	Inspect(ctx context.Context, auth *coreauth.Auth) (codexCredentialStatus, error)
}

type codexCredentialStatus struct {
	Unauthorized       bool
	HasRemainingQuota  bool
	RemainingQuotaPerc float64
}

type codexHTTPInspector struct {
	httpClient *http.Client
	// overrideURL replaces codexWhamUsageURL in tests.
	overrideURL string
}

func (s *Service) startCodexCredentialChecker(parent context.Context) {
	if s == nil || s.coreManager == nil {
		return
	}
	if s.codexCredentialCheckCancel != nil {
		s.codexCredentialCheckCancel()
		s.codexCredentialCheckCancel = nil
	}
	if s.codexCredentialCheckReload == nil {
		s.codexCredentialCheckReload = make(chan struct{}, 1)
	}
	ctx, cancel := context.WithCancel(parent)
	s.codexCredentialCheckCancel = cancel
	go s.codexCredentialCheckLoop(ctx)
}

func (s *Service) notifyCodexCredentialCheckerReload() {
	if s == nil || s.codexCredentialCheckReload == nil {
		return
	}
	select {
	case s.codexCredentialCheckReload <- struct{}{}:
	default:
	}
}

func (s *Service) codexCredentialCheckLoop(ctx context.Context) {
	for {
		interval := s.codexCredentialCheckInterval()
		if interval <= 0 {
			select {
			case <-ctx.Done():
				return
			case <-s.codexCredentialCheckReload:
				continue
			}
		}

		s.runCodexCredentialCheck(ctx)

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-s.codexCredentialCheckReload:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
	}
}

func (s *Service) codexCredentialCheckInterval() time.Duration {
	if s == nil {
		return 0
	}
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	if s.cfg == nil || s.cfg.CodexCredentialCheckIntervalSeconds <= 0 {
		return 0
	}
	return time.Duration(s.cfg.CodexCredentialCheckIntervalSeconds) * time.Second
}

func (s *Service) runCodexCredentialCheck(ctx context.Context) {
	if s == nil || s.coreManager == nil {
		return
	}
	inspector := s.codexCredentialInspectorOrDefault()
	if inspector == nil {
		return
	}

	s.cfgMu.RLock()
	threshold := s.cfg.CodexCredentialDeleteThresholdPercent
	s.cfgMu.RUnlock()

	auths := s.coreManager.List()
	for _, auth := range auths {
		if !shouldInspectCodexCredential(auth) {
			continue
		}
		checkCtx, cancel := context.WithTimeout(ctx, codexCredentialCheckTimeout)
		status, err := inspector.Inspect(checkCtx, auth)
		cancel()
		if err != nil {
			log.Warnf("codex credential check failed for %s: %v", codexCredentialLogLabel(auth), err)
			continue
		}
		if status.Unauthorized {
			s.removeCodexCredential(context.Background(), auth, "401 unauthorized during scheduled check")
			continue
		}
		if status.HasRemainingQuota && status.RemainingQuotaPerc < threshold {
			s.removeCodexCredential(context.Background(), auth, fmt.Sprintf("remaining quota %.2f%% below %.2f%%", status.RemainingQuotaPerc, threshold))
		}
	}
}

func (s *Service) removeCodexCredential(ctx context.Context, auth *coreauth.Auth, reason string) {
	if s == nil || auth == nil || auth.ID == "" {
		return
	}
	log.Warnf("deleting codex credential %s: %s", codexCredentialLogLabel(auth), strings.TrimSpace(reason))
	s.deleteCoreAuth(ctx, auth.ID)
}

func (s *Service) codexCredentialInspectorOrDefault() codexCredentialInspector {
	if s == nil {
		return nil
	}
	if s.codexCredentialInspector != nil {
		return s.codexCredentialInspector
	}
	s.cfgMu.RLock()
	cfg := s.cfg
	s.cfgMu.RUnlock()
	var httpClient *http.Client
	if cfg != nil {
		httpClient = util.SetProxy(&cfg.SDKConfig, &http.Client{})
	} else {
		httpClient = &http.Client{}
	}
	s.codexCredentialInspector = &codexHTTPInspector{httpClient: httpClient}
	return s.codexCredentialInspector
}

func shouldInspectCodexCredential(auth *coreauth.Auth) bool {
	if auth == nil || auth.Disabled {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(auth.Provider), "codex") {
		return false
	}
	if auth.Attributes != nil {
		if v := strings.ToLower(strings.TrimSpace(auth.Attributes["runtime_only"])); v == "true" {
			return false
		}
		if strings.TrimSpace(auth.Attributes["api_key"]) != "" {
			return false
		}
	}
	_, accountID, _, ok := codexCredentialParams(auth)
	return ok && accountID != ""
}

func codexCredentialLogLabel(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if label := strings.TrimSpace(auth.Label); label != "" {
		parts = append(parts, label)
	}
	if id := strings.TrimSpace(auth.ID); id != "" && id != strings.TrimSpace(auth.Label) {
		parts = append(parts, id)
	}
	if len(parts) == 0 {
		return "codex"
	}
	return strings.Join(parts, " ")
}

func codexCredentialParams(auth *coreauth.Auth) (accessToken string, accountID string, planType string, ok bool) {
	if auth == nil || auth.Metadata == nil {
		return "", "", "", false
	}
	if v, okToken := auth.Metadata["access_token"].(string); okToken {
		accessToken = strings.TrimSpace(v)
	}
	if v, okAccount := auth.Metadata["account_id"].(string); okAccount {
		accountID = strings.TrimSpace(v)
	}
	if auth.Attributes != nil {
		planType = strings.TrimSpace(auth.Attributes["plan_type"])
	}
	if accountID == "" {
		if rawIDToken, okIDToken := auth.Metadata["id_token"].(string); okIDToken {
			if claims, err := codexauth.ParseJWTToken(strings.TrimSpace(rawIDToken)); err == nil && claims != nil {
				accountID = strings.TrimSpace(claims.CodexAuthInfo.ChatgptAccountID)
				if planType == "" {
					planType = strings.TrimSpace(claims.CodexAuthInfo.ChatgptPlanType)
				}
			}
		}
	}
	if accessToken == "" || accountID == "" {
		return "", "", "", false
	}
	return accessToken, accountID, planType, true
}

// Inspect checks a Codex OAuth credential by calling the wham/usage REST endpoint
// directly, without relying on the codex CLI binary. It returns the remaining quota
// percentage so the caller can delete credentials that fall below the threshold.
func (i *codexHTTPInspector) Inspect(ctx context.Context, auth *coreauth.Auth) (codexCredentialStatus, error) {
	accessToken, accountID, _, ok := codexCredentialParams(auth)
	if !ok {
		return codexCredentialStatus{}, fmt.Errorf("missing codex access token or account id")
	}

	url := codexWhamUsageURL
	if i.overrideURL != "" {
		url = i.overrideURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return codexCredentialStatus{}, fmt.Errorf("codex credential check: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Chatgpt-Account-Id", accountID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", codexCheckUserAgent)
	req.Header.Set("Version", codexCheckClientVersion)
	req.Header.Set("Originator", "codex_cli_rs")

	resp, err := i.httpClient.Do(req)
	if err != nil {
		return codexCredentialStatus{}, fmt.Errorf("codex credential check: http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return codexCredentialStatus{Unauthorized: true}, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return codexCredentialStatus{}, fmt.Errorf("codex credential check: unexpected status %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return codexCredentialStatus{}, fmt.Errorf("codex credential check: read response: %w", err)
	}

	return parseWhamUsageStatus(body), nil
}

// parseWhamUsageStatus extracts credential health from a GET /backend-api/wham/usage response.
//
// Quota threshold checks only apply to free-plan accounts. Plus, Team, and other
// subscription accounts are not subject to the quota deletion threshold.
//
// Response structure (relevant fields):
//
//	{
//	  "plan_type": "free" | "plus" | "team" | ...,
//	  "rate_limit": {
//	    "limit_reached": bool,
//	    "primary_window":   { "used_percent": float64 },
//	    "secondary_window": { "used_percent": float64 }  // may be null
//	  }
//	}
//
// Remaining quota is 100 minus the highest used_percent across primary and secondary
// windows. If limit_reached is true, remaining quota is forced to 0.
func parseWhamUsageStatus(body []byte) codexCredentialStatus {
	root := gjson.ParseBytes(body)

	// Only check quota for free accounts; skip threshold logic for paid plans.
	planType := strings.ToLower(strings.TrimSpace(root.Get("plan_type").String()))
	if planType != "" && planType != "free" {
		return codexCredentialStatus{}
	}

	rateLimit := root.Get("rate_limit")
	if !rateLimit.Exists() {
		// No rate_limit field — treat as healthy (unknown plan or unexpected response).
		return codexCredentialStatus{}
	}

	// If the server already says limit_reached, remaining is 0.
	if rateLimit.Get("limit_reached").Bool() {
		return codexCredentialStatus{HasRemainingQuota: true, RemainingQuotaPerc: 0}
	}

	// Find the highest used_percent across primary and secondary windows.
	maxUsed := -1.0
	for _, key := range []string{"primary_window", "secondary_window"} {
		w := rateLimit.Get(key)
		if !w.Exists() || w.Type == gjson.Null {
			continue
		}
		used := w.Get("used_percent").Float()
		if used > maxUsed {
			maxUsed = used
		}
	}

	if maxUsed < 0 {
		// No window data — treat as healthy.
		return codexCredentialStatus{}
	}

	remaining := 100 - maxUsed
	if remaining < 0 {
		remaining = 0
	}

	return codexCredentialStatus{HasRemainingQuota: true, RemainingQuotaPerc: remaining}
}
