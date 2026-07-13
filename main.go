package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Configuration from environment
var (
	logLevel       = "INFO"
	tautulliURL    string
	apiKey         string
	metricsPort    = 8000
	scrapeInterval = 30
	requestTimeout = 10
)

// Prometheus metrics
var (
	activeStreamsTotal        = promauto.NewGauge(prometheus.GaugeOpts{Name: "plex_active_streams_total", Help: "Total number of active Plex streams"})
	activeStreamsDirect       = promauto.NewGauge(prometheus.GaugeOpts{Name: "plex_active_streams_direct", Help: "Number of non-transcoding streams (direct play + direct stream)"})
	activeStreamsDirectPlay   = promauto.NewGauge(prometheus.GaugeOpts{Name: "plex_active_streams_direct_play", Help: "Number of direct play sessions"})
	activeStreamsDirectStream = promauto.NewGauge(prometheus.GaugeOpts{Name: "plex_active_streams_direct_stream", Help: "Number of direct stream sessions"})
	activeStreamsTranscode    = promauto.NewGauge(prometheus.GaugeOpts{Name: "plex_active_streams_transcode", Help: "Number of transcoding streams"})
	transcodeVideo            = promauto.NewGauge(prometheus.GaugeOpts{Name: "plex_transcode_video_sessions", Help: "Video transcoding sessions"})
	transcodeAudio            = promauto.NewGauge(prometheus.GaugeOpts{Name: "plex_transcode_audio_sessions", Help: "Audio transcoding sessions"})
	transcodeContainer        = promauto.NewGauge(prometheus.GaugeOpts{Name: "plex_transcode_container_sessions", Help: "Container transcoding sessions"})
	bandwidthTotal            = promauto.NewGauge(prometheus.GaugeOpts{Name: "plex_bandwidth_total_kbps", Help: "Total Plex streaming bandwidth (kbps)"})
	bandwidthLAN              = promauto.NewGauge(prometheus.GaugeOpts{Name: "plex_bandwidth_lan_kbps", Help: "LAN streaming bandwidth (kbps)"})
	bandwidthWAN              = promauto.NewGauge(prometheus.GaugeOpts{Name: "plex_bandwidth_wan_kbps", Help: "WAN streaming bandwidth (kbps)"})
)

// Track consecutive failures for circuit breaker pattern
const (
	maxConsecutiveFailures      = 5
	circuitBreakerResetInterval = 60 * time.Second // before allowing a probe attempt
)

var (
	consecutiveFailures  int
	lastSuccessfulScrape time.Time
	circuitOpenedAt      time.Time
	metricsLock          sync.Mutex
)

var httpClient = &http.Client{}

// --- Structured JSON logging, same format as the Python exporter ---

var levelOrder = map[string]int{"DEBUG": 10, "INFO": 20, "WARNING": 30, "ERROR": 40}

var logOut io.Writer = os.Stderr

func logAt(level, msg string) {
	threshold, ok := levelOrder[logLevel]
	if !ok {
		threshold = levelOrder["INFO"]
	}
	if levelOrder[level] < threshold {
		return
	}
	quoted, _ := json.Marshal(msg)
	fmt.Fprintf(logOut, "{\"timestamp\":\"%s\",\"level\":\"%s\",\"logger\":\"plex_exporter\",\"message\":%s}\n",
		time.Now().Format("2006-01-02T15:04:05"), level, quoted)
}

func logDebug(msg string) { logAt("DEBUG", msg) }
func logInfo(msg string)  { logAt("INFO", msg) }
func logError(msg string) { logAt("ERROR", msg) }

func loadConfig() {
	if v, ok := os.LookupEnv("LOG_LEVEL"); ok {
		logLevel = strings.ToUpper(v)
	}
	tautulliURL = strings.TrimRight(strings.TrimSpace(os.Getenv("TAUTULLI_URL")), "/")
	apiKey = strings.TrimSpace(os.Getenv("TAUTULLI_API_KEY"))
	metricsPort = intFromEnv("METRICS_PORT", 8000)
	scrapeInterval = intFromEnv("SCRAPE_INTERVAL", 30)
	requestTimeout = intFromEnv("REQUEST_TIMEOUT", 10)
	httpClient.Timeout = time.Duration(requestTimeout) * time.Second
}

func intFromEnv(name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		logError(fmt.Sprintf("%s '%s' is not a valid integer", name, v))
		os.Exit(1)
	}
	return n
}

func validateConfig() []string {
	var errs []string

	if tautulliURL == "" {
		errs = append(errs, "TAUTULLI_URL environment variable is required")
	} else {
		parsed, err := url.Parse(tautulliURL)
		switch {
		case err != nil:
			errs = append(errs, fmt.Sprintf("TAUTULLI_URL validation error: %v", err))
		case parsed.Scheme == "":
			errs = append(errs, fmt.Sprintf("TAUTULLI_URL '%s' must include http:// or https://", tautulliURL))
		case parsed.Scheme != "http" && parsed.Scheme != "https":
			errs = append(errs, fmt.Sprintf("TAUTULLI_URL scheme must be http or https, not '%s'", parsed.Scheme))
		case parsed.Host == "":
			errs = append(errs, fmt.Sprintf("TAUTULLI_URL '%s' is not a valid URL", tautulliURL))
		}
	}

	if apiKey == "" {
		errs = append(errs, "TAUTULLI_API_KEY environment variable is required")
	} else if len(apiKey) < 16 || !isAlnumIgnoringUnderscores(apiKey) {
		errs = append(errs, "TAUTULLI_API_KEY appears to be invalid format")
	}

	if metricsPort < 1 || metricsPort > 65535 {
		errs = append(errs, fmt.Sprintf("METRICS_PORT %d is not valid (must be 1-65535)", metricsPort))
	}

	if scrapeInterval < 5 {
		errs = append(errs, fmt.Sprintf("SCRAPE_INTERVAL %d is too low (minimum 5 seconds)", scrapeInterval))
	}

	return errs
}

func isAlnumIgnoringUnderscores(s string) bool {
	stripped := strings.ReplaceAll(s, "_", "")
	if stripped == "" {
		return false
	}
	for _, r := range stripped {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsNumber(r) {
			return false
		}
	}
	return true
}

// flexInt tolerates Tautulli returning numbers as either JSON numbers or strings.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*f = flexInt(n)
	return nil
}

type tautulliSession struct {
	TranscodeVideoDecision     string `json:"transcode_video_decision"`
	TranscodeAudioDecision     string `json:"transcode_audio_decision"`
	TranscodeContainerDecision string `json:"transcode_container_decision"`
}

type tautulliResponse struct {
	Response struct {
		Result  string `json:"result"`
		Message string `json:"message"`
		Data    struct {
			StreamCount             flexInt           `json:"stream_count"`
			StreamCountDirectPlay   flexInt           `json:"stream_count_direct_play"`
			StreamCountDirectStream flexInt           `json:"stream_count_direct_stream"`
			StreamCountTranscode    flexInt           `json:"stream_count_transcode"`
			TotalBandwidth          flexInt           `json:"total_bandwidth"`
			LANBandwidth            flexInt           `json:"lan_bandwidth"`
			WANBandwidth            flexInt           `json:"wan_bandwidth"`
			Sessions                []tautulliSession `json:"sessions"`
		} `json:"data"`
	} `json:"response"`
}

func recordFailure(format string, args ...any) {
	metricsLock.Lock()
	consecutiveFailures++
	failureCount := consecutiveFailures
	circuitOpenedAt = time.Now()
	metricsLock.Unlock()
	msg := fmt.Sprintf(format, args...)
	logError(fmt.Sprintf("%s (failure %d/%d)", msg, failureCount, maxConsecutiveFailures))
}

func getTautulliActivity() {
	metricsLock.Lock()
	if consecutiveFailures >= maxConsecutiveFailures {
		timeSinceOpen := time.Since(circuitOpenedAt)
		if timeSinceOpen < circuitBreakerResetInterval {
			failures := consecutiveFailures
			metricsLock.Unlock()
			logError(fmt.Sprintf("Circuit breaker active: %d consecutive failures", failures))
			return
		}
		logInfo(fmt.Sprintf("Circuit breaker half-open: probing after %ds", int(timeSinceOpen.Seconds())))
	}
	metricsLock.Unlock()

	apiEndpoint := tautulliURL + "/api/v2"
	params := url.Values{}
	params.Set("apikey", apiKey)
	params.Set("cmd", "get_activity")

	logDebug(fmt.Sprintf("Fetching activity from %s", apiEndpoint))
	resp, err := httpClient.Get(apiEndpoint + "?" + params.Encode())
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			recordFailure("Request timeout after %ds", requestTimeout)
		} else {
			recordFailure("Connection error: %v", err)
		}
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		recordFailure("Connection error: %v", err)
		return
	}

	if resp.StatusCode >= 400 {
		recordFailure("HTTP error: %s for url: %s", resp.Status, apiEndpoint)
		return
	}

	var data tautulliResponse
	if err := json.Unmarshal(body, &data); err != nil {
		recordFailure("Invalid JSON response: %v", err)
		return
	}

	if data.Response.Result != "success" {
		errorMsg := data.Response.Message
		if errorMsg == "" {
			errorMsg = "Unknown error"
		}
		recordFailure("Unexpected error: API returned error: %s", errorMsg)
		return
	}

	activityData := data.Response.Data

	// Reset failure counter on success
	metricsLock.Lock()
	consecutiveFailures = 0
	lastSuccessfulScrape = time.Now()
	metricsLock.Unlock()

	// Aggregate counts from activity data (pre-calculated by Tautulli)
	totalStreams := int(activityData.StreamCount)
	directPlayStreams := int(activityData.StreamCountDirectPlay)
	directStreamStreams := int(activityData.StreamCountDirectStream)
	transcodeStreams := int(activityData.StreamCountTranscode)

	// Backwards compatible: direct = direct_play + direct_stream
	directStreams := directPlayStreams + directStreamStreams

	totalBW := int(activityData.TotalBandwidth)
	lanBW := int(activityData.LANBandwidth)
	wanBW := int(activityData.WANBandwidth)

	// Per-component transcode analysis (from individual sessions)
	videoTranscodes := 0
	audioTranscodes := 0
	containerTranscodes := 0
	for _, session := range activityData.Sessions {
		if session.TranscodeVideoDecision == "transcode" {
			videoTranscodes++
		}
		if session.TranscodeAudioDecision == "transcode" {
			audioTranscodes++
		}
		if session.TranscodeContainerDecision == "transcode" {
			containerTranscodes++
		}
	}

	activeStreamsTotal.Set(float64(totalStreams))
	activeStreamsDirect.Set(float64(directStreams))
	activeStreamsDirectPlay.Set(float64(directPlayStreams))
	activeStreamsDirectStream.Set(float64(directStreamStreams))
	activeStreamsTranscode.Set(float64(transcodeStreams))
	transcodeVideo.Set(float64(videoTranscodes))
	transcodeAudio.Set(float64(audioTranscodes))
	transcodeContainer.Set(float64(containerTranscodes))
	bandwidthTotal.Set(float64(totalBW))
	bandwidthLAN.Set(float64(lanBW))
	bandwidthWAN.Set(float64(wanBW))

	logDebug("Metrics updated")
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	// Liveness probe - always return 200 if service is running
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func readyHandler(w http.ResponseWriter, _ *http.Request) {
	// Readiness probe - check if we can scrape data
	metricsLock.Lock()
	lastScrape := lastSuccessfulScrape
	failures := consecutiveFailures
	metricsLock.Unlock()

	timeSinceLastSuccess := time.Since(lastScrape)

	// Consider ready if:
	// 1. Never scraped yet (startup grace period)
	// 2. Last successful scrape was within 2 intervals AND not in circuit breaker state
	if lastScrape.IsZero() || (timeSinceLastSuccess < time.Duration(scrapeInterval*2)*time.Second && failures < maxConsecutiveFailures) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("READY"))
	} else {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, "NOT READY: Last success %ds ago, failures: %d", int(timeSinceLastSuccess.Seconds()), failures)
	}
}

func notFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("Not Found"))
}

func newMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/ready", readyHandler)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/", notFoundHandler)
	// Suppress access logs except in debug mode
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if logLevel == "DEBUG" {
			logDebug(fmt.Sprintf("Health check: \"%s %s %s\"", r.Method, r.URL.Path, r.Proto))
		}
		mux.ServeHTTP(w, r)
	})
}

func signalName(s os.Signal) string {
	switch s {
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGHUP:
		return "SIGHUP"
	default:
		return s.String()
	}
}

func run(sigCh chan os.Signal) int {
	logInfo("Starting Plex exporter")

	// Validate configuration
	if errs := validateConfig(); len(errs) > 0 {
		for _, e := range errs {
			logError(e)
		}
		return 1
	}

	// Register signal handlers for graceful shutdown
	// Note: SIGKILL and SIGSTOP cannot be caught
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	logInfo("Signal handlers registered for graceful shutdown")

	// Start HTTP server with health endpoints
	server := &http.Server{Handler: newMux()}
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", metricsPort))
	if err != nil {
		logError(fmt.Sprintf("Failed to start metrics server: %v", err))
		return 1
	}
	go server.Serve(listener)

	logInfo(fmt.Sprintf("Metrics server started on port %d", metricsPort))
	logInfo("Health endpoints: /healthz (liveness), /ready (readiness), /metrics")

	// Main loop
	shuttingDown := false
	for !shuttingDown {
		getTautulliActivity()

		// Wait for SCRAPE_INTERVAL or until shutdown is requested
		select {
		case sig := <-sigCh:
			logInfo(fmt.Sprintf("Received %s, initiating graceful shutdown", signalName(sig)))
			shuttingDown = true
		case <-time.After(time.Duration(scrapeInterval) * time.Second):
		}
	}

	// Cleanup on shutdown
	logInfo("Shutting down HTTP server")
	server.Close()
	logInfo("Exporter stopped gracefully")
	return 0
}

func main() {
	loadConfig()
	os.Exit(run(make(chan os.Signal, 1)))
}
