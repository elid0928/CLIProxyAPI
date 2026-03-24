package cliproxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"sync"
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

type serviceDeleteStore struct {
	mu        sync.Mutex
	deleteIDs []string
}

func (s *serviceDeleteStore) List(context.Context) ([]*coreauth.Auth, error) { return nil, nil }
func (s *serviceDeleteStore) Save(context.Context, *coreauth.Auth) (string, error) {
	return "", nil
}
func (s *serviceDeleteStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteIDs = append(s.deleteIDs, id)
	return nil
}
func (s *serviceDeleteStore) DeletedIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.deleteIDs...)
}

type stubCodexCredentialInspector struct {
	mu       sync.Mutex
	statuses map[string]codexCredentialStatus
	errors   map[string]error
	calls    []string
}

func (s *stubCodexCredentialInspector) Inspect(_ context.Context, auth *coreauth.Auth) (codexCredentialStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, auth.ID)
	if err := s.errors[auth.ID]; err != nil {
		return codexCredentialStatus{}, err
	}
	return s.statuses[auth.ID], nil
}

func TestServiceRunCodexCredentialCheckDeletesInvalidOAuthCreds(t *testing.T) {
	store := &serviceDeleteStore{}
	manager := coreauth.NewManager(store, nil, nil)
	register := func(auth *coreauth.Auth) {
		t.Helper()
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("Register(%s) error = %v", auth.ID, err)
		}
	}

	register(&coreauth.Auth{
		ID:       "codex-unauthorized",
		Provider: "codex",
		Label:    "unauthorized",
		Metadata: map[string]any{"type": "codex", "access_token": "token-1", "account_id": "acct-1"},
	})
	register(&coreauth.Auth{
		ID:       "codex-low-quota",
		Provider: "codex",
		Label:    "low-quota",
		Metadata: map[string]any{"type": "codex", "access_token": "token-2", "account_id": "acct-2"},
	})
	register(&coreauth.Auth{
		ID:       "codex-healthy",
		Provider: "codex",
		Label:    "healthy",
		Metadata: map[string]any{"type": "codex", "access_token": "token-3", "account_id": "acct-3"},
	})
	register(&coreauth.Auth{
		ID:       "codex-api-key",
		Provider: "codex",
		Attributes: map[string]string{
			"api_key": "sk-test",
		},
	})
	register(&coreauth.Auth{
		ID:       "gemini-auth",
		Provider: "gemini",
		Metadata: map[string]any{"type": "gemini"},
	})

	inspector := &stubCodexCredentialInspector{
		statuses: map[string]codexCredentialStatus{
			"codex-unauthorized": {Unauthorized: true},
			"codex-low-quota": {
				HasRemainingQuota:  true,
				RemainingQuotaPerc: 9.5,
			},
			"codex-healthy": {
				HasRemainingQuota:  true,
				RemainingQuotaPerc: 24,
			},
		},
		errors: make(map[string]error),
	}
	service := &Service{
		cfg: &config.Config{
			CodexCredentialCheckIntervalSeconds:   60,
			CodexCredentialDeleteThresholdPercent: 10.0,
		},
		coreManager:              manager,
		codexCredentialInspector: inspector,
	}

	service.runCodexCredentialCheck(context.Background())

	if _, ok := manager.GetByID("codex-unauthorized"); ok {
		t.Fatalf("expected unauthorized auth to be removed")
	}
	if _, ok := manager.GetByID("codex-low-quota"); ok {
		t.Fatalf("expected low-quota auth to be removed")
	}
	if _, ok := manager.GetByID("codex-healthy"); !ok {
		t.Fatalf("expected healthy auth to remain")
	}
	if _, ok := manager.GetByID("codex-api-key"); !ok {
		t.Fatalf("expected codex api-key auth to be skipped")
	}
	if _, ok := manager.GetByID("gemini-auth"); !ok {
		t.Fatalf("expected non-codex auth to remain")
	}

	if got := store.DeletedIDs(); !sortedStringsEqual(got, []string{"codex-unauthorized", "codex-low-quota"}) {
		t.Fatalf("deleted IDs = %#v, want unauthorized and low-quota auths", got)
	}
	if got := inspector.calls; !sortedStringsEqual(got, []string{"codex-unauthorized", "codex-low-quota", "codex-healthy"}) {
		t.Fatalf("inspector calls = %#v", got)
	}
}

// whamResponse builds a /backend-api/wham/usage response body for tests.
func whamResponse(planType string, primaryUsed, secondaryUsed float64, limitReached bool) []byte {
	type window struct {
		UsedPercent float64 `json:"used_percent"`
	}
	type rateLimit struct {
		LimitReached    bool    `json:"limit_reached"`
		PrimaryWindow   *window `json:"primary_window"`
		SecondaryWindow *window `json:"secondary_window"`
	}
	type response struct {
		PlanType  string    `json:"plan_type"`
		RateLimit rateLimit `json:"rate_limit"`
	}

	var sec *window
	if secondaryUsed >= 0 {
		sec = &window{UsedPercent: secondaryUsed}
	}
	r := response{
		PlanType: planType,
		RateLimit: rateLimit{
			LimitReached:    limitReached,
			PrimaryWindow:   &window{UsedPercent: primaryUsed},
			SecondaryWindow: sec,
		},
	}
	b, _ := json.Marshal(r)
	return b
}

func newTestInspector(srv *httptest.Server) *codexHTTPInspector {
	return &codexHTTPInspector{httpClient: srv.Client(), overrideURL: srv.URL}
}

func newTestCodexAuth() *coreauth.Auth {
	return &coreauth.Auth{
		ID:       "codex-auth",
		Provider: "codex",
		Metadata: map[string]any{
			"access_token": "access-token",
			"account_id":   "acct-123",
		},
	}
}

func TestCodexHTTPInspector_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	status, err := newTestInspector(srv).Inspect(context.Background(), newTestCodexAuth())
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !status.Unauthorized {
		t.Fatalf("expected Unauthorized=true")
	}
}

func TestCodexHTTPInspector_LimitReached(t *testing.T) {
	body := whamResponse("free", 0, 100, true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	status, err := newTestInspector(srv).Inspect(context.Background(), newTestCodexAuth())
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !status.HasRemainingQuota {
		t.Fatalf("expected HasRemainingQuota=true")
	}
	if status.RemainingQuotaPerc != 0 {
		t.Fatalf("RemainingQuotaPerc = %v, want 0", status.RemainingQuotaPerc)
	}
}

func TestCodexHTTPInspector_LowQuotaSecondaryWindow(t *testing.T) {
	// free account: primary=0% used, secondary=93% used → remaining = 7%
	body := whamResponse("free", 0, 93, false)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	status, err := newTestInspector(srv).Inspect(context.Background(), newTestCodexAuth())
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !status.HasRemainingQuota {
		t.Fatalf("expected HasRemainingQuota=true")
	}
	if status.RemainingQuotaPerc != 7 {
		t.Fatalf("RemainingQuotaPerc = %v, want 7", status.RemainingQuotaPerc)
	}
}

func TestCodexHTTPInspector_CreditsExhausted(t *testing.T) {
	// limit_reached=true → remaining forced to 0 regardless of credits field
	body := whamResponse("free", 0, 100, true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	status, err := newTestInspector(srv).Inspect(context.Background(), newTestCodexAuth())
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !status.HasRemainingQuota {
		t.Fatalf("expected HasRemainingQuota=true")
	}
	if status.RemainingQuotaPerc != 0 {
		t.Fatalf("RemainingQuotaPerc = %v, want 0", status.RemainingQuotaPerc)
	}
}

func TestCodexHTTPInspector_Healthy(t *testing.T) {
	// free account: primary=0% used, secondary=2% used → remaining = 98%
	body := whamResponse("free", 0, 2, false)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	status, err := newTestInspector(srv).Inspect(context.Background(), newTestCodexAuth())
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if status.Unauthorized {
		t.Fatalf("expected Unauthorized=false")
	}
	if !status.HasRemainingQuota {
		t.Fatalf("expected HasRemainingQuota=true")
	}
	if status.RemainingQuotaPerc != 98 {
		t.Fatalf("RemainingQuotaPerc = %v, want 98", status.RemainingQuotaPerc)
	}
}

func TestCodexHTTPInspector_PlusAccountSkipsQuotaCheck(t *testing.T) {
	// plus account with 93% used → quota check skipped, no deletion
	body := whamResponse("plus", 0, 93, false)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	status, err := newTestInspector(srv).Inspect(context.Background(), newTestCodexAuth())
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if status.HasRemainingQuota {
		t.Fatalf("expected HasRemainingQuota=false for plus account")
	}
}

func TestCodexHTTPInspector_TeamAccountSkipsQuotaCheck(t *testing.T) {
	// team account with limit_reached=true → quota check skipped, no deletion
	body := whamResponse("team", 0, 100, true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	status, err := newTestInspector(srv).Inspect(context.Background(), newTestCodexAuth())
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if status.HasRemainingQuota {
		t.Fatalf("expected HasRemainingQuota=false for team account")
	}
}

func TestParseWhamUsageStatus(t *testing.T) {
	tests := []struct {
		name              string
		body              []byte
		wantHasQuota      bool
		wantRemainingPerc float64
	}{
		{
			name:              "free: limit_reached forces 0%",
			body:              whamResponse("free", 0, 100, true),
			wantHasQuota:      true,
			wantRemainingPerc: 0,
		},
		{
			name:              "free: secondary 93% used → 7% remaining",
			body:              whamResponse("free", 0, 93, false),
			wantHasQuota:      true,
			wantRemainingPerc: 7,
		},
		{
			name:              "free: primary 50% used, no secondary → 50% remaining",
			body:              whamResponse("free", 50, -1, false),
			wantHasQuota:      true,
			wantRemainingPerc: 50,
		},
		{
			name:         "plus: quota check skipped even when exhausted",
			body:         whamResponse("plus", 0, 100, true),
			wantHasQuota: false,
		},
		{
			name:         "team: quota check skipped even when exhausted",
			body:         whamResponse("team", 0, 100, true),
			wantHasQuota: false,
		},
		{
			name:         "unknown plan: treated as free, quota checked",
			body:         whamResponse("", 0, 93, false),
			wantHasQuota: true,
			wantRemainingPerc: 7,
		},
		{
			name:         "empty body returns no-quota status",
			body:         []byte(`{}`),
			wantHasQuota: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseWhamUsageStatus(tc.body)
			if got.HasRemainingQuota != tc.wantHasQuota {
				t.Fatalf("HasRemainingQuota = %v, want %v", got.HasRemainingQuota, tc.wantHasQuota)
			}
			if tc.wantHasQuota && got.RemainingQuotaPerc != tc.wantRemainingPerc {
				t.Fatalf("RemainingQuotaPerc = %v, want %v", got.RemainingQuotaPerc, tc.wantRemainingPerc)
			}
		})
	}
}

func sortedStringsEqual(got, want []string) bool {
	got = append([]string(nil), got...)
	want = append([]string(nil), want...)
	sort.Strings(got)
	sort.Strings(want)
	return slices.Equal(got, want)
}
