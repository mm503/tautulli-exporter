#!/usr/bin/env python3
import os
import sys
import json
import logging
import requests
import time
import signal
from urllib.parse import urlparse, urljoin
from prometheus_client import Gauge, generate_latest, REGISTRY
from http.server import HTTPServer, BaseHTTPRequestHandler
from socketserver import ThreadingMixIn
import threading

# Configure logging
LOG_LEVEL = os.environ.get('LOG_LEVEL', 'INFO').upper()
logging.basicConfig(
    level=getattr(logging, LOG_LEVEL, logging.INFO),
    format='{"timestamp":"%(asctime)s","level":"%(levelname)s","logger":"%(name)s","message":"%(message)s"}',
    datefmt='%Y-%m-%dT%H:%M:%S'
)
logger = logging.getLogger('plex_exporter')

# Configuration from environment
TAUTULLI_URL = os.environ.get('TAUTULLI_URL', '').strip().rstrip('/')
API_KEY = os.environ.get('TAUTULLI_API_KEY', '').strip()
METRICS_PORT = int(os.environ.get('METRICS_PORT', '8000'))
SCRAPE_INTERVAL = int(os.environ.get('SCRAPE_INTERVAL', '30'))
REQUEST_TIMEOUT = int(os.environ.get('REQUEST_TIMEOUT', '10'))

# Prometheus metrics
active_streams_total = Gauge('plex_active_streams_total', 'Total number of active Plex streams')
active_streams_direct = Gauge('plex_active_streams_direct', 'Number of direct play streams')
active_streams_transcode = Gauge('plex_active_streams_transcode', 'Number of transcoding streams')
transcode_video = Gauge('plex_transcode_video_sessions', 'Video transcoding sessions')
transcode_audio = Gauge('plex_transcode_audio_sessions', 'Audio transcoding sessions')
transcode_container = Gauge('plex_transcode_container_sessions', 'Container transcoding sessions')

# Track consecutive failures for circuit breaker pattern
consecutive_failures = 0
MAX_CONSECUTIVE_FAILURES = 5
last_successful_scrape = 0
metrics_lock = threading.Lock()

# Shutdown event for graceful termination
shutdown_event = threading.Event()

class HealthHandler(BaseHTTPRequestHandler):
    """Health check handler with minimal logging"""

    def log_message(self, fmt, *args):
        """Override to suppress access logs except in debug mode"""
        if LOG_LEVEL == 'DEBUG':
            logger.debug(f"Health check: {fmt % args}")

    def do_GET(self):
        if self.path == '/healthz':
            # Liveness probe - always return 200 if service is running
            self.send_response(200)
            self.send_header('Content-type', 'text/plain')
            self.end_headers()
            self.wfile.write(b'OK')

        elif self.path == '/ready':
            # Readiness probe - check if we can scrape data
            with metrics_lock:
                last_scrape = last_successful_scrape
                failures = consecutive_failures

            time_since_last_success = time.time() - last_scrape

            # Consider ready if:
            # 1. Never scraped yet (startup grace period)
            # 2. Last successful scrape was within 2 intervals AND not in circuit breaker state
            if last_scrape == 0 or (time_since_last_success < SCRAPE_INTERVAL * 2 and failures < MAX_CONSECUTIVE_FAILURES):
                self.send_response(200)
                self.send_header('Content-type', 'text/plain')
                self.end_headers()
                self.wfile.write(b'READY')
            else:
                self.send_response(503)
                self.send_header('Content-type', 'text/plain')
                self.end_headers()
                self.wfile.write(f'NOT READY: Last success {int(time_since_last_success)}s ago, failures: {failures}'.encode())

        elif self.path == '/metrics':
            # Generate metrics using prometheus_client
            try:
                output = generate_latest(REGISTRY)
                self.send_response(200)
                self.send_header('Content-type', 'text/plain; version=0.0.4; charset=utf-8')
                self.end_headers()
                self.wfile.write(output)
            except Exception as e:
                logger.error(f"Error generating metrics: {e}")
                self.send_response(500)
                self.send_header('Content-type', 'text/plain')
                self.end_headers()
                self.wfile.write(b'Error generating metrics')

        else:
            self.send_response(404)
            self.send_header('Content-type', 'text/plain')
            self.end_headers()
            self.wfile.write(b'Not Found')

class ThreadedHTTPServer(ThreadingMixIn, HTTPServer):
    """Handle requests in separate threads"""
    daemon_threads = True

def validate_config():
    """Validate required configuration"""
    errors = []

    if not TAUTULLI_URL:
        errors.append("TAUTULLI_URL environment variable is required")
    else:
        try:
            parsed = urlparse(TAUTULLI_URL)
            if not parsed.scheme:
                errors.append(f"TAUTULLI_URL '{TAUTULLI_URL}' must include http:// or https://")
            elif parsed.scheme not in ['http', 'https']:
                errors.append(f"TAUTULLI_URL scheme must be http or https, not '{parsed.scheme}'")
            elif not parsed.netloc:
                errors.append(f"TAUTULLI_URL '{TAUTULLI_URL}' is not a valid URL")
        except Exception as e:
            errors.append(f"TAUTULLI_URL validation error: {e}")

    if not API_KEY:
        errors.append("TAUTULLI_API_KEY environment variable is required")
    elif len(API_KEY) < 16 or not API_KEY.replace('_', '').isalnum():
        errors.append("TAUTULLI_API_KEY appears to be invalid format")

    if METRICS_PORT < 1 or METRICS_PORT > 65535:
        errors.append(f"METRICS_PORT {METRICS_PORT} is not valid (must be 1-65535)")

    if SCRAPE_INTERVAL < 5:
        errors.append(f"SCRAPE_INTERVAL {SCRAPE_INTERVAL} is too low (minimum 5 seconds)")

    if errors:
        for error in errors:
            logger.error(error)
        sys.exit(1)

def get_tautulli_activity():
    """Fetch activity data from Tautulli API"""
    global consecutive_failures, last_successful_scrape

    with metrics_lock:
        if consecutive_failures >= MAX_CONSECUTIVE_FAILURES:
            logger.error(f"Circuit breaker active: {consecutive_failures} consecutive failures")
            return

    # Properly construct the API URL
    api_endpoint = urljoin(TAUTULLI_URL + '/', 'api/v2')
    params = {
        'apikey': API_KEY,
        'cmd': 'get_activity'
    }

    try:
        logger.debug(f"Fetching activity from {api_endpoint}")
        response = requests.get(api_endpoint, params=params, timeout=REQUEST_TIMEOUT)
        response.raise_for_status()

        data = response.json()

        if data.get('response', {}).get('result') != 'success':
            error_msg = data.get('response', {}).get('message', 'Unknown error')
            raise Exception(f"API returned error: {error_msg}")

        sessions = data['response']['data'].get('sessions', [])

        # Reset failure counter on success
        with metrics_lock:
            consecutive_failures = 0
            last_successful_scrape = time.time()

        # Initialize counters
        total_streams = len(sessions)
        direct_streams = 0
        transcode_streams = 0
        video_transcodes = 0
        audio_transcodes = 0
        container_transcodes = 0

        # Analyze each session
        for session in sessions:
            video_decision = session.get('transcode_video_decision', 'direct play')
            audio_decision = session.get('transcode_audio_decision', 'direct play')
            container_decision = session.get('transcode_container_decision', 'direct play')

            # Count transcode types
            if video_decision == 'transcode':
                video_transcodes += 1
            if audio_decision == 'transcode':
                audio_transcodes += 1
            if container_decision == 'transcode':
                container_transcodes += 1

            # Overall stream classification
            if any(decision == 'transcode' for decision in
                   [video_decision, audio_decision, container_decision]):
                transcode_streams += 1
            else:
                direct_streams += 1

        # Update metrics
        active_streams_total.set(total_streams)
        active_streams_direct.set(direct_streams)
        active_streams_transcode.set(transcode_streams)
        transcode_video.set(video_transcodes)
        transcode_audio.set(audio_transcodes)
        transcode_container.set(container_transcodes)

        logger.debug(
            "Metrics updated",
            extra={
                "total_streams": total_streams,
                "direct_streams": direct_streams,
                "transcode_streams": transcode_streams,
                "video_transcodes": video_transcodes,
                "audio_transcodes": audio_transcodes,
                "container_transcodes": container_transcodes
            }
        )

    except requests.exceptions.Timeout:
        with metrics_lock:
            consecutive_failures += 1
            failure_count = consecutive_failures
        logger.error(f"Request timeout after {REQUEST_TIMEOUT}s (failure {failure_count}/{MAX_CONSECUTIVE_FAILURES})")
    except requests.exceptions.ConnectionError as e:
        with metrics_lock:
            consecutive_failures += 1
            failure_count = consecutive_failures
        logger.error(f"Connection error: {e} (failure {failure_count}/{MAX_CONSECUTIVE_FAILURES})")
    except requests.exceptions.HTTPError as e:
        with metrics_lock:
            consecutive_failures += 1
            failure_count = consecutive_failures
        logger.error(f"HTTP error: {e} (failure {failure_count}/{MAX_CONSECUTIVE_FAILURES})")
    except json.JSONDecodeError as e:
        with metrics_lock:
            consecutive_failures += 1
            failure_count = consecutive_failures
        logger.error(f"Invalid JSON response: {e} (failure {failure_count}/{MAX_CONSECUTIVE_FAILURES})")
    except Exception as e:
        with metrics_lock:
            consecutive_failures += 1
            failure_count = consecutive_failures
        logger.error(f"Unexpected error: {e} (failure {failure_count}/{MAX_CONSECUTIVE_FAILURES})")

def signal_handler(signum, frame):
    """Handle shutdown signals gracefully"""
    signal_name = signal.Signals(signum).name
    logger.info(f"Received {signal_name}, initiating graceful shutdown")
    shutdown_event.set()

def main():
    """Main entry point"""
    logger.info("Starting Plex exporter", extra={
        "tautulli_url": TAUTULLI_URL,
        "metrics_port": METRICS_PORT,
        "scrape_interval": SCRAPE_INTERVAL,
        "log_level": LOG_LEVEL
    })

    # Validate configuration
    validate_config()

    # Register signal handlers for graceful shutdown
    # Note: SIGKILL and SIGSTOP cannot be caught
    signal.signal(signal.SIGTERM, signal_handler)
    signal.signal(signal.SIGINT, signal_handler)
    if hasattr(signal, 'SIGHUP'):
        signal.signal(signal.SIGHUP, signal_handler)
    logger.info("Signal handlers registered for graceful shutdown")

    # Start HTTP server with health endpoints
    try:
        httpd = ThreadedHTTPServer(('', METRICS_PORT), HealthHandler)
        server_thread = threading.Thread(target=httpd.serve_forever)
        server_thread.daemon = True
        server_thread.start()

        logger.info(f"Metrics server started on port {METRICS_PORT}")
        logger.info("Health endpoints: /healthz (liveness), /ready (readiness), /metrics")
    except Exception as e:
        logger.error(f"Failed to start metrics server: {e}")
        sys.exit(1)

    # Main loop
    while not shutdown_event.is_set():
        try:
            get_tautulli_activity()
        except Exception as e:
            logger.error(f"Unexpected error in main loop: {e}")

        # Wait for SCRAPE_INTERVAL or until shutdown is requested
        if shutdown_event.wait(timeout=SCRAPE_INTERVAL):
            break

    # Cleanup on shutdown
    logger.info("Shutting down HTTP server")
    httpd.shutdown()
    logger.info("Exporter stopped gracefully")

if __name__ == '__main__':
    main()
