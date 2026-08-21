package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

const (
	validURL = "http://tautulli.example.com"
	validKey = "abcdef1234567890"
)

func resetState() {
	consecutiveFailures = 0
	circuitOpenedAt = time.Time{}
	firstFailureAt = time.Time{}
	tautulliURL = validURL
	apiKey = validKey
	metricsPort = 8000
	scrapeInterval = 30
	requestTimeout = 10
	logLevel = "ERROR"
	logOut = io.Discard // keep test output quiet
	httpClient = &http.Client{Timeout: 2 * time.Second}
}

func activityJSON(streamCount, directPlay, directStream, transcode, totalBW, lanBW, wanBW int, sessions string) string {
	return fmt.Sprintf(`{
		"response": {
			"result": "success",
			"message": "",
			"data": {
				"stream_count": %d,
				"stream_count_direct_play": %d,
				"stream_count_direct_stream": %d,
				"stream_count_transcode": %d,
				"total_bandwidth": %d,
				"lan_bandwidth": %d,
				"wan_bandwidth": %d,
				"sessions": [%s]
			}
		}
	}`, streamCount, directPlay, directStream, transcode, totalBW, lanBW, wanBW, sessions)
}

func activityServer(t *testing.T, body string) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// ---------------------------------------------------------------------------
// validateConfig
// ---------------------------------------------------------------------------

func TestValidateConfig(t *testing.T) {
	cases := []struct {
		name      string
		url, key  string
		port      int
		interval  int
		wantValid bool
	}{
		{"valid config", validURL, validKey, 8000, 30, true},
		{"https url is valid", "https://tautulli.example.com", validKey, 8000, 30, true},
		{"missing url", "", validKey, 8000, 30, false},
		{"url without scheme", "tautulli.example.com", validKey, 8000, 30, false},
		{"url with invalid scheme", "ftp://tautulli.example.com", validKey, 8000, 30, false},
		{"url with no host", "http://", validKey, 8000, 30, false},
		{"url with user credentials", "http://user:password@tautulli.example.com", validKey, 8000, 30, false},
		{"url with query", "http://tautulli.example.com?proxy=true", validKey, 8000, 30, false},
		{"url with fragment", "http://tautulli.example.com#fragment", validKey, 8000, 30, false},
		{"missing api key", validURL, "", 8000, 30, false},
		{"short api key", validURL, "tooshort", 8000, 30, false},
		{"api key with invalid chars", validURL, "invalid-key-here!!", 8000, 30, false},
		{"api key with underscores is valid", validURL, "abcdef_1234567890", 8000, 30, true},
		{"api key of only underscores is invalid", validURL, "________________", 8000, 30, false},
		{"port zero", validURL, validKey, 0, 30, false},
		{"port too high", validURL, validKey, 65536, 30, false},
		{"scrape interval too low", validURL, validKey, 8000, 4, false},
		{"scrape interval minimum is valid", validURL, validKey, 8000, 5, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetState()
			tautulliURL = tc.url
			apiKey = tc.key
			metricsPort = tc.port
			scrapeInterval = tc.interval

			errs := validateConfig()
			if tc.wantValid && len(errs) > 0 {
				t.Errorf("expected valid config, got errors: %v", errs)
			}
			if !tc.wantValid && len(errs) == 0 {
				t.Error("expected validation errors, got none")
			}
		})
	}
}

func TestValidateConfigRejectsInvalidTimeoutAndLogLevel(t *testing.T) {
	for _, timeout := range []int{0, -1} {
		t.Run(fmt.Sprintf("timeout_%d", timeout), func(t *testing.T) {
			resetState()
			requestTimeout = timeout
			if errs := validateConfig(); len(errs) == 0 {
				t.Fatalf("REQUEST_TIMEOUT=%d unexpectedly passed validation", timeout)
			}
		})
	}

	resetState()
	logLevel = "BOGUS"
	if errs := validateConfig(); len(errs) == 0 {
		t.Fatal("invalid LOG_LEVEL unexpectedly passed validation")
	}
}

func TestConfigWarnsWhenTimeoutNotBelowInterval(t *testing.T) {
	cases := []struct {
		name             string
		interval, timout int
		wantWarning      bool
	}{
		{"timeout below interval", 30, 10, false},
		{"timeout equals interval", 10, 10, true},
		{"timeout above interval", 5, 10, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetState()
			scrapeInterval = tc.interval
			requestTimeout = tc.timout

			if errs := validateConfig(); len(errs) > 0 {
				t.Fatalf("expected a valid config, got errors: %v", errs)
			}
			warnings := configWarnings()
			if got := len(warnings) > 0; got != tc.wantWarning {
				t.Errorf("configWarnings() = %v, want warning = %v", warnings, tc.wantWarning)
			}
		})
	}
}

func TestValidateConfigMultipleErrors(t *testing.T) {
	resetState()
	tautulliURL = ""
	apiKey = ""

	errs := validateConfig()
	if len(errs) < 2 {
		t.Errorf("expected at least 2 errors (URL and key), got %d: %v", len(errs), errs)
	}
}

// ---------------------------------------------------------------------------
// getTautulliActivity — circuit breaker
// ---------------------------------------------------------------------------

func TestCircuitOpenWithinCooldownSkipsRequest(t *testing.T) {
	resetState()
	srv, calls := activityServer(t, activityJSON(2, 1, 0, 1, 5000, 3000, 2000, ""))
	tautulliURL = srv.URL
	consecutiveFailures = maxConsecutiveFailures
	circuitOpenedAt = time.Now() // just opened

	getTautulliActivity(context.Background())

	if *calls != 0 {
		t.Errorf("expected no request while circuit open, got %d", *calls)
	}
}

func TestCircuitBreakerSkipDoesNotCountAsFailedHTTPRequest(t *testing.T) {
	resetState()
	var buf bytes.Buffer
	logOut = &buf
	logLevel = "WARNING"
	consecutiveFailures = maxConsecutiveFailures
	circuitOpenedAt = time.Now()
	before := testutil.ToFloat64(scrapeFailuresTotal)

	getTautulliActivity(context.Background())

	if got := testutil.ToFloat64(scrapeFailuresTotal); got != before {
		t.Fatalf("failure counter changed on skipped request: got %v, want %v", got, before)
	}
	if !strings.Contains(buf.String(), "HTTP scrape skipped") {
		t.Fatalf("circuit-breaker skip warning missing: %q", buf.String())
	}
}

func TestCircuitHalfOpenAfterCooldownProbes(t *testing.T) {
	resetState()
	srv, calls := activityServer(t, activityJSON(2, 1, 0, 1, 5000, 3000, 2000, ""))
	tautulliURL = srv.URL
	consecutiveFailures = maxConsecutiveFailures
	circuitOpenedAt = time.Now().Add(-(circuitBreakerResetInterval + time.Second))

	getTautulliActivity(context.Background())

	if *calls != 1 {
		t.Errorf("expected 1 probe request, got %d", *calls)
	}
	if consecutiveFailures != 0 {
		t.Errorf("successful probe should reset circuit, failures = %d", consecutiveFailures)
	}
}

func TestFailedProbeRearmsCooldown(t *testing.T) {
	resetState()
	tautulliURL = "http://127.0.0.1:1" // connection refused
	consecutiveFailures = maxConsecutiveFailures
	oldOpenedAt := time.Now().Add(-(circuitBreakerResetInterval + 10*time.Second))
	circuitOpenedAt = oldOpenedAt

	getTautulliActivity(context.Background())

	if !circuitOpenedAt.After(oldOpenedAt) {
		t.Error("circuit_opened_at must be refreshed after failed probe")
	}
}

func TestFailuresIncrementCounter(t *testing.T) {
	resetState()
	tautulliURL = "http://127.0.0.1:1"

	getTautulliActivity(context.Background())

	if consecutiveFailures != 1 {
		t.Errorf("expected 1 failure, got %d", consecutiveFailures)
	}
}

// ---------------------------------------------------------------------------
// getTautulliActivity — success path & metrics
// ---------------------------------------------------------------------------

func TestSuccessResetsFailureCounterAndTimestamp(t *testing.T) {
	resetState()
	srv, _ := activityServer(t, activityJSON(2, 1, 0, 1, 5000, 3000, 2000, ""))
	tautulliURL = srv.URL
	consecutiveFailures = 3
	lastSuccessTimestamp.Set(0)

	getTautulliActivity(context.Background())

	if consecutiveFailures != 0 {
		t.Errorf("expected failures reset to 0, got %d", consecutiveFailures)
	}
	if got := testutil.ToFloat64(lastSuccessTimestamp); got <= 0 {
		t.Error("last-success metric timestamp not updated")
	}
}

func TestRecoveryAfterFailuresIsLogged(t *testing.T) {
	resetState()
	var buf bytes.Buffer
	logOut = &buf
	logLevel = "INFO"

	// Two failures against a dead endpoint, then a working one.
	tautulliURL = "http://127.0.0.1:1"
	getTautulliActivity(context.Background())
	getTautulliActivity(context.Background())

	srv, _ := activityServer(t, activityJSON(0, 0, 0, 0, 0, 0, 0, ""))
	tautulliURL = srv.URL
	buf.Reset()
	getTautulliActivity(context.Background())

	line := buf.String()
	for _, want := range []string{`"level":"INFO"`, "Collection resumed after 2 consecutive failures"} {
		if !strings.Contains(line, want) {
			t.Errorf("recovery log %q missing %s", line, want)
		}
	}
	if strings.Contains(line, "circuit breaker closed") {
		t.Errorf("circuit breaker was never open, got %q", line)
	}
}

func TestRecoveryFromOpenCircuitMentionsBreaker(t *testing.T) {
	resetState()
	var buf bytes.Buffer
	logOut = &buf
	logLevel = "INFO"
	srv, _ := activityServer(t, activityJSON(0, 0, 0, 0, 0, 0, 0, ""))
	tautulliURL = srv.URL
	consecutiveFailures = maxConsecutiveFailures
	firstFailureAt = time.Now().Add(-90 * time.Second)
	circuitOpenedAt = time.Now().Add(-(circuitBreakerResetInterval + time.Second))

	getTautulliActivity(context.Background())

	line := buf.String()
	if !strings.Contains(line, "Collection resumed after 5 consecutive failures over 90s; circuit breaker closed") {
		t.Errorf("unexpected recovery log: %q", line)
	}
}

func TestNoRecoveryLogWithoutPriorFailures(t *testing.T) {
	resetState()
	var buf bytes.Buffer
	logOut = &buf
	logLevel = "INFO"
	srv, _ := activityServer(t, activityJSON(0, 0, 0, 0, 0, 0, 0, ""))
	tautulliURL = srv.URL

	getTautulliActivity(context.Background())

	if strings.Contains(buf.String(), "Collection resumed") {
		t.Errorf("unexpected recovery log on clean scrape: %q", buf.String())
	}
}

func TestMetricsSetFromActivityData(t *testing.T) {
	resetState()
	srv, _ := activityServer(t, activityJSON(3, 1, 1, 1, 9000, 6000, 3000, ""))
	tautulliURL = srv.URL

	getTautulliActivity(context.Background())

	checks := map[string]struct {
		got  float64
		want float64
	}{
		"plex_active_streams_total":         {testutil.ToFloat64(activeStreamsTotal), 3},
		"plex_active_streams_direct":        {testutil.ToFloat64(activeStreamsDirect), 2}, // direct_play + direct_stream
		"plex_active_streams_direct_play":   {testutil.ToFloat64(activeStreamsDirectPlay), 1},
		"plex_active_streams_direct_stream": {testutil.ToFloat64(activeStreamsDirectStream), 1},
		"plex_active_streams_transcode":     {testutil.ToFloat64(activeStreamsTranscode), 1},
		"plex_bandwidth_total_kbps":         {testutil.ToFloat64(bandwidthTotal), 9000},
		"plex_bandwidth_lan_kbps":           {testutil.ToFloat64(bandwidthLAN), 6000},
		"plex_bandwidth_wan_kbps":           {testutil.ToFloat64(bandwidthWAN), 3000},
	}
	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", name, c.got, c.want)
		}
	}
}

func TestTranscodeComponentCountsFromSessions(t *testing.T) {
	resetState()
	sessions := `
		{"transcode_video_decision": "transcode", "transcode_audio_decision": "transcode", "transcode_container_decision": "direct play"},
		{"transcode_video_decision": "direct play", "transcode_audio_decision": "copy", "transcode_container_decision": "transcode"},
		{"transcode_video_decision": "transcode", "transcode_audio_decision": "direct play", "transcode_container_decision": "direct play"}`
	srv, _ := activityServer(t, activityJSON(3, 0, 0, 3, 0, 0, 0, sessions))
	tautulliURL = srv.URL

	getTautulliActivity(context.Background())

	if got := testutil.ToFloat64(transcodeVideo); got != 2 {
		t.Errorf("video transcodes = %v, want 2", got)
	}
	if got := testutil.ToFloat64(transcodeAudio); got != 1 {
		t.Errorf("audio transcodes = %v, want 1", got)
	}
	if got := testutil.ToFloat64(transcodeContainer); got != 1 {
		t.Errorf("container transcodes = %v, want 1", got)
	}
}

func TestSessionMissingDecisionDefaultsToDirectPlay(t *testing.T) {
	resetState()
	srv, _ := activityServer(t, activityJSON(1, 1, 0, 0, 0, 0, 0, "{}"))
	tautulliURL = srv.URL

	getTautulliActivity(context.Background())

	if got := testutil.ToFloat64(transcodeVideo); got != 0 {
		t.Errorf("video transcodes = %v, want 0", got)
	}
	if got := testutil.ToFloat64(transcodeAudio); got != 0 {
		t.Errorf("audio transcodes = %v, want 0", got)
	}
	if got := testutil.ToFloat64(transcodeContainer); got != 0 {
		t.Errorf("container transcodes = %v, want 0", got)
	}
}

func TestNumericFieldsAsStringsAreParsed(t *testing.T) {
	// Some Tautulli versions return counts as JSON strings
	resetState()
	body := `{"response": {"result": "success", "data": {
		"stream_count": "4", "stream_count_direct_play": "2", "stream_count_direct_stream": "1",
		"stream_count_transcode": "1", "total_bandwidth": "7000", "lan_bandwidth": "5000",
		"wan_bandwidth": "2000", "sessions": []}}}`
	srv, _ := activityServer(t, body)
	tautulliURL = srv.URL

	getTautulliActivity(context.Background())

	if got := testutil.ToFloat64(activeStreamsTotal); got != 4 {
		t.Errorf("total streams = %v, want 4", got)
	}
	if got := testutil.ToFloat64(bandwidthTotal); got != 7000 {
		t.Errorf("total bandwidth = %v, want 7000", got)
	}
}

func TestAPIFailureResultIncrementsCounter(t *testing.T) {
	resetState()
	srv, _ := activityServer(t, `{"response": {"result": "error", "message": "bad key"}}`)
	tautulliURL = srv.URL

	getTautulliActivity(context.Background())

	if consecutiveFailures != 1 {
		t.Errorf("expected 1 failure, got %d", consecutiveFailures)
	}
}

func TestHTTPErrorIncrementsCounter(t *testing.T) {
	resetState()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	tautulliURL = srv.URL

	getTautulliActivity(context.Background())

	if consecutiveFailures != 1 {
		t.Errorf("expected 1 failure, got %d", consecutiveFailures)
	}
}

func TestInvalidJSONIncrementsCounter(t *testing.T) {
	resetState()
	srv, _ := activityServer(t, "not json at all")
	tautulliURL = srv.URL

	getTautulliActivity(context.Background())

	if consecutiveFailures != 1 {
		t.Errorf("expected 1 failure, got %d", consecutiveFailures)
	}
}

func TestTimeoutIncrementsCounter(t *testing.T) {
	resetState()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()
	tautulliURL = srv.URL
	httpClient = &http.Client{Timeout: 50 * time.Millisecond}

	getTautulliActivity(context.Background())

	if consecutiveFailures != 1 {
		t.Errorf("expected 1 failure, got %d", consecutiveFailures)
	}
}

func TestConnectionErrorDoesNotLogAPIKey(t *testing.T) {
	resetState()
	var buf bytes.Buffer
	logOut = &buf
	logLevel = "ERROR"
	tautulliURL = "http://127.0.0.1:1"

	getTautulliActivity(context.Background())

	if strings.Contains(buf.String(), apiKey) {
		t.Fatalf("connection error leaked API key: %q", buf.String())
	}
}

func TestOversizedResponseIsRejected(t *testing.T) {
	resetState()
	srv, _ := activityServer(t, strings.Repeat("x", maxResponseBodyBytes+1))
	tautulliURL = srv.URL

	getTautulliActivity(context.Background())

	if consecutiveFailures != 1 {
		t.Fatalf("oversized response failures = %d, want 1", consecutiveFailures)
	}
}

func TestCanceledRequestDoesNotCountAsFailure(t *testing.T) {
	resetState()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	getTautulliActivity(ctx)

	if consecutiveFailures != 0 {
		t.Fatalf("canceled shutdown request failures = %d, want 0", consecutiveFailures)
	}
}

func TestAllErrorsUpdateCircuitOpenedAt(t *testing.T) {
	resetState()
	tautulliURL = "http://127.0.0.1:1"
	before := time.Now()

	getTautulliActivity(context.Background())

	if circuitOpenedAt.Before(before) {
		t.Error("circuit_opened_at not updated on failure")
	}
}

func TestAPIURLConstructedCorrectly(t *testing.T) {
	resetState()
	var gotPath, gotAPIKey, gotCmd string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.URL.Query().Get("apikey")
		gotCmd = r.URL.Query().Get("cmd")
		w.Write([]byte(activityJSON(0, 0, 0, 0, 0, 0, 0, "")))
	}))
	defer srv.Close()
	tautulliURL = srv.URL

	getTautulliActivity(context.Background())

	if gotPath != "/api/v2" {
		t.Errorf("path = %q, want /api/v2", gotPath)
	}
	if gotAPIKey != validKey {
		t.Errorf("apikey = %q, want %q", gotAPIKey, validKey)
	}
	if gotCmd != "get_activity" {
		t.Errorf("cmd = %q, want get_activity", gotCmd)
	}
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

func doRequest(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestHealthzReturns200OK(t *testing.T) {
	resetState()
	rec := doRequest("/healthz")
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "OK" {
		t.Errorf("body = %q, want OK", rec.Body.String())
	}
}

func TestReadyReturns200WhenNeverScraped(t *testing.T) {
	resetState()
	rec := doRequest("/ready")
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200 (startup grace period)", rec.Code)
	}
	if rec.Body.String() != "READY" {
		t.Errorf("body = %q, want READY", rec.Body.String())
	}
}

func TestReadyReturns200WhenRecentlyScraped(t *testing.T) {
	resetState()
	rec := doRequest("/ready")
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// Readiness must stay 200 while Tautulli is unreachable. A 503 here would
// drop the pod from its Service endpoints and make the exporter unscrapeable
// during the outage; plex_up carries that signal instead.
func TestReadyReturns200WhenScrapeTooOld(t *testing.T) {
	resetState()
	rec := doRequest("/ready")
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200 (upstream health must not gate readiness)", rec.Code)
	}
	if rec.Body.String() != "READY" {
		t.Errorf("body = %q, want READY", rec.Body.String())
	}
}

func TestReadyReturns200WhenCircuitBreakerActive(t *testing.T) {
	resetState()
	consecutiveFailures = maxConsecutiveFailures
	rec := doRequest("/ready")
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200 (circuit breaker must not gate readiness)", rec.Code)
	}
}

func TestRecordFailureSetsUpZeroAndCountsFailure(t *testing.T) {
	resetState()
	up.Set(1)
	before := testutil.ToFloat64(scrapeFailuresTotal)

	recordFailure("synthetic failure")

	if got := testutil.ToFloat64(up); got != 0 {
		t.Errorf("plex_up = %v, want 0 after a failed scrape", got)
	}
	if got := testutil.ToFloat64(scrapeFailuresTotal); got != before+1 {
		t.Errorf("plex_scrape_failures_total = %v, want %v", got, before+1)
	}
}

func TestSuccessfulScrapeSetsUpAndTimestamp(t *testing.T) {
	resetState()
	up.Set(0)
	lastSuccessTimestamp.Set(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"response":{"result":"success","data":{"stream_count":"0","sessions":[]}}}`))
	}))
	defer srv.Close()
	tautulliURL = srv.URL

	getTautulliActivity(context.Background())

	if got := testutil.ToFloat64(up); got != 1 {
		t.Errorf("plex_up = %v, want 1 after a successful scrape", got)
	}
	if got := testutil.ToFloat64(lastSuccessTimestamp); got <= 0 {
		t.Errorf("plex_last_successful_scrape_timestamp_seconds = %v, want a positive unix time", got)
	}
}

func TestScrapeHealthMetricsAreExposed(t *testing.T) {
	resetState()
	body := doRequest("/metrics").Body.String()
	for _, name := range []string{
		"plex_up",
		"plex_last_successful_scrape_timestamp_seconds",
		"plex_scrape_failures_total",
	} {
		if !strings.Contains(body, name) {
			t.Errorf("metrics output missing %s", name)
		}
	}
}

func TestMetricsReturns200WithPrometheusOutput(t *testing.T) {
	resetState()
	rec := doRequest("/metrics")
	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "plex_active_streams_total") {
		t.Error("metrics output missing plex_active_streams_total")
	}
}

func TestUnknownPathReturns404(t *testing.T) {
	resetState()
	rec := doRequest("/unknown")
	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if rec.Body.String() != "Not Found" {
		t.Errorf("body = %q, want Not Found", rec.Body.String())
	}
}

func TestAccessLogOnlyInDebugMode(t *testing.T) {
	resetState()
	var buf bytes.Buffer
	logOut = &buf

	logLevel = "INFO"
	doRequest("/healthz")
	if strings.Contains(buf.String(), "Request:") {
		t.Error("access log emitted at INFO level")
	}

	logLevel = "DEBUG"
	doRequest("/healthz")
	if !strings.Contains(buf.String(), "Request:") {
		t.Error("access log missing at DEBUG level")
	}
}

// ---------------------------------------------------------------------------
// loadConfig
// ---------------------------------------------------------------------------

func TestLoadConfigReadsAndNormalizesEnv(t *testing.T) {
	resetState()
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("TAUTULLI_URL", "  http://tautulli.example.com/  ")
	t.Setenv("TAUTULLI_API_KEY", "  abcdef1234567890  ")
	t.Setenv("METRICS_PORT", "9100")
	t.Setenv("SCRAPE_INTERVAL", "15")
	t.Setenv("REQUEST_TIMEOUT", "5")

	if errs := loadConfig(); len(errs) > 0 {
		t.Fatalf("loadConfig() errors = %v", errs)
	}

	if logLevel != "DEBUG" {
		t.Errorf("logLevel = %q, want DEBUG (uppercased)", logLevel)
	}
	if tautulliURL != "http://tautulli.example.com" {
		t.Errorf("tautulliURL = %q, want trimmed with trailing slash stripped", tautulliURL)
	}
	if apiKey != "abcdef1234567890" {
		t.Errorf("apiKey = %q, want trimmed", apiKey)
	}
	if metricsPort != 9100 || scrapeInterval != 15 || requestTimeout != 5 {
		t.Errorf("ints = %d/%d/%d, want 9100/15/5", metricsPort, scrapeInterval, requestTimeout)
	}
	if httpClient.Timeout != 5*time.Second {
		t.Errorf("client timeout = %v, want 5s", httpClient.Timeout)
	}
}

func TestLoadConfigTrimsNumericEnv(t *testing.T) {
	resetState()
	t.Setenv("TAUTULLI_URL", validURL)
	t.Setenv("TAUTULLI_API_KEY", validKey)
	t.Setenv("METRICS_PORT", "  9100  ")
	t.Setenv("SCRAPE_INTERVAL", "\t15\n")
	t.Setenv("REQUEST_TIMEOUT", " 5 ")

	if errs := loadConfig(); len(errs) > 0 {
		t.Fatalf("loadConfig() errors = %v", errs)
	}
	if metricsPort != 9100 || scrapeInterval != 15 || requestTimeout != 5 {
		t.Errorf("got port=%d interval=%d timeout=%d, want 9100/15/5", metricsPort, scrapeInterval, requestTimeout)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	resetState()
	for _, v := range []string{"LOG_LEVEL", "TAUTULLI_URL", "TAUTULLI_API_KEY", "METRICS_PORT", "SCRAPE_INTERVAL", "REQUEST_TIMEOUT"} {
		t.Setenv(v, "") // register restore, then unset
		os.Unsetenv(v)
	}

	if errs := loadConfig(); len(errs) > 0 {
		t.Fatalf("loadConfig() errors = %v", errs)
	}

	if logLevel != "ERROR" { // untouched when LOG_LEVEL is unset (resetState set ERROR)
		t.Errorf("logLevel = %q, want ERROR (unchanged)", logLevel)
	}
	if tautulliURL != "" || apiKey != "" {
		t.Errorf("url/key = %q/%q, want empty", tautulliURL, apiKey)
	}
	if metricsPort != 8000 || scrapeInterval != 30 || requestTimeout != 10 {
		t.Errorf("ints = %d/%d/%d, want defaults 8000/30/10", metricsPort, scrapeInterval, requestTimeout)
	}
}

func TestLoadConfigReturnsIntegerErrors(t *testing.T) {
	resetState()
	t.Setenv("METRICS_PORT", "not-a-port")
	t.Setenv("SCRAPE_INTERVAL", "not-an-interval")
	t.Setenv("REQUEST_TIMEOUT", "not-a-timeout")

	errs := loadConfig()
	if len(errs) != 3 {
		t.Fatalf("loadConfig() returned %d errors, want 3: %v", len(errs), errs)
	}
	if metricsPort != 8000 || scrapeInterval != 30 || requestTimeout != 10 {
		t.Fatalf("invalid integers did not fall back to defaults: %d/%d/%d", metricsPort, scrapeInterval, requestTimeout)
	}
}

// ---------------------------------------------------------------------------
// flexInt
// ---------------------------------------------------------------------------

func TestFlexIntUnmarshal(t *testing.T) {
	cases := []struct {
		in      string
		want    flexInt
		wantErr bool
	}{
		{`5000`, 5000, false},
		{`"5000"`, 5000, false},
		{`5.9`, 5, false}, // truncates like Python int()
		{`null`, 0, false},
		{`""`, 0, false},
		{`"abc"`, 0, true},
	}
	for _, tc := range cases {
		var f flexInt
		err := f.UnmarshalJSON([]byte(tc.in))
		if tc.wantErr != (err != nil) {
			t.Errorf("UnmarshalJSON(%s) error = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && f != tc.want {
			t.Errorf("UnmarshalJSON(%s) = %d, want %d", tc.in, f, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// logging
// ---------------------------------------------------------------------------

func TestLogFormatMatchesPythonExporter(t *testing.T) {
	resetState()
	var buf bytes.Buffer
	logOut = &buf
	logLevel = "INFO"

	logInfo("hello")

	line := buf.String()
	for _, want := range []string{`"level":"INFO"`, `"logger":"plex_exporter"`, `"message":"hello"`, `"timestamp":"`} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q missing %s", line, want)
		}
	}
}

func TestLogLevelFiltering(t *testing.T) {
	resetState()
	var buf bytes.Buffer
	logOut = &buf

	logLevel = "ERROR"
	logInfo("suppressed")
	if buf.Len() != 0 {
		t.Errorf("INFO not suppressed at ERROR level: %q", buf.String())
	}

	logLevel = "BOGUS" // unknown level falls back to INFO threshold
	logDebug("suppressed")
	if buf.Len() != 0 {
		t.Errorf("DEBUG not suppressed at fallback INFO level: %q", buf.String())
	}
	logInfo("emitted")
	if !strings.Contains(buf.String(), "emitted") {
		t.Error("INFO suppressed at fallback INFO level")
	}
}

// ---------------------------------------------------------------------------
// signals & run()
// ---------------------------------------------------------------------------

func TestSignalName(t *testing.T) {
	cases := map[os.Signal]string{
		syscall.SIGTERM: "SIGTERM",
		syscall.SIGINT:  "SIGINT",
		syscall.SIGHUP:  "SIGHUP",
		syscall.SIGUSR1: syscall.SIGUSR1.String(),
	}
	for sig, want := range cases {
		if got := signalName(sig); got != want {
			t.Errorf("signalName(%v) = %q, want %q", sig, got, want)
		}
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestRunExitsOnInvalidConfig(t *testing.T) {
	resetState()
	tautulliURL = ""
	sigCh := make(chan os.Signal, 1)
	defer signal.Stop(sigCh)

	if code := run(sigCh); code != 1 {
		t.Errorf("run() = %d, want 1 on invalid config", code)
	}
}

func TestRunExitsOnServerStartFailure(t *testing.T) {
	resetState()
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	metricsPort = ln.Addr().(*net.TCPAddr).Port // already in use

	sigCh := make(chan os.Signal, 1)
	defer signal.Stop(sigCh)

	if code := run(sigCh); code != 1 {
		t.Errorf("run() = %d, want 1 when port is taken", code)
	}
}

func TestRunScrapesServesAndShutsDownGracefully(t *testing.T) {
	resetState()
	requestReceived := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(requestReceived) })
		w.Write([]byte(activityJSON(1, 1, 0, 0, 100, 100, 0, "")))
	}))
	defer srv.Close()
	tautulliURL = srv.URL
	metricsPort = freePort(t)

	sigCh := make(chan os.Signal, 1)
	defer signal.Stop(sigCh)
	result := make(chan int, 1)
	go func() { result <- run(sigCh) }()

	select {
	case <-requestReceived:
		sigCh <- syscall.SIGTERM
	case <-time.After(2 * time.Second):
		t.Fatal("exporter did not perform its initial scrape")
	}

	select {
	case code := <-result:
		if code != 0 {
			t.Errorf("run() = %d, want 0 on graceful shutdown", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("exporter did not shut down")
	}
}

func TestRunCancelsInFlightScrapeOnShutdown(t *testing.T) {
	resetState()
	requestReceived := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(requestReceived)
		<-r.Context().Done()
	}))
	defer srv.Close()
	tautulliURL = srv.URL
	metricsPort = freePort(t)
	httpClient = &http.Client{}

	sigCh := make(chan os.Signal, 1)
	result := make(chan int, 1)
	go func() { result <- run(sigCh) }()

	select {
	case <-requestReceived:
		sigCh <- syscall.SIGTERM
	case <-time.After(2 * time.Second):
		t.Fatal("exporter did not begin its initial scrape")
	}

	select {
	case code := <-result:
		if code != 0 {
			t.Errorf("run() = %d, want 0 on graceful shutdown", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not cancel the in-flight scrape")
	}
}
