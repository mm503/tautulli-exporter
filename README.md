# Tautulli Exporter

A Prometheus exporter for Plex Media Server metrics via Tautulli API. Designed for Kubernetes deployment with proper health checks, structured logging, and graceful error handling.

## Features

- **Prometheus Metrics** - Exposes Plex streaming metrics in Prometheus format
- **Kubernetes Ready** - Health probes, structured JSON logging, configurable via environment variables
- **Circuit Breaker** - Stops attempting failed requests after threshold
- **Graceful Degradation** - Continues operating when Tautulli is temporarily unavailable

## Metrics Exposed

| Metric | Type | Description |
|--------|------|-------------|
| `plex_active_streams_total` | Gauge | Total number of active Plex streams |
| `plex_active_streams_direct` | Gauge | Number of non-transcoding streams (direct play + direct stream) |
| `plex_active_streams_direct_play` | Gauge | Number of direct play sessions |
| `plex_active_streams_direct_stream` | Gauge | Number of direct stream sessions |
| `plex_active_streams_transcode` | Gauge | Number of transcoding streams |
| `plex_transcode_video_sessions` | Gauge | Video transcoding sessions |
| `plex_transcode_audio_sessions` | Gauge | Audio transcoding sessions |
| `plex_transcode_container_sessions` | Gauge | Container transcoding sessions |
| `plex_bandwidth_total_kbps` | Gauge | Total Plex streaming bandwidth (kbps) |
| `plex_bandwidth_lan_kbps` | Gauge | LAN streaming bandwidth (kbps) |
| `plex_bandwidth_wan_kbps` | Gauge | WAN streaming bandwidth (kbps) |
| `plex_up` | Gauge | 1 if the last Tautulli scrape succeeded, 0 otherwise |
| `plex_last_successful_scrape_timestamp_seconds` | Gauge | Unix timestamp of the last successful Tautulli scrape |
| `plex_scrape_failures_total` | Counter | Total number of failed Tautulli scrapes |

## Configuration

All configuration is done via environment variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TAUTULLI_URL` | Yes | - | Tautulli server URL (e.g., `http://192.168.1.100:8181`) |
| `TAUTULLI_API_KEY` | Yes | - | Tautulli API key from Settings → Web Interface |
| `METRICS_PORT` | No | `8000` | Port for metrics/health endpoints |
| `SCRAPE_INTERVAL` | No | `30` | Seconds between Tautulli API calls |
| `REQUEST_TIMEOUT` | No | `10` | HTTP request timeout in seconds |
| `LOG_LEVEL` | No | `INFO` | Logging level (DEBUG, INFO, WARNING, ERROR) |

## Endpoints

- `/metrics` - Prometheus metrics
- `/healthz` - Kubernetes liveness probe (always returns 200 if running)
- `/ready` - Kubernetes readiness probe (200 whenever the process can serve `/metrics`)

`/ready` intentionally does **not** fail when Tautulli is unreachable. Gating
readiness on an upstream removes the pod from its Service endpoints, which stops
Prometheus from scraping the exporter exactly when the outage needs reporting.
Alert on the `plex_up` metric instead:

```yaml
- alert: PlexExporterCannotReachTautulli
  expr: plex_up == 0
  for: 5m
```

## Container Images

Released images are published to both Docker Hub and GitHub Container Registry:

| Registry | Image | Tags |
|----------|-------|------|
| Docker Hub | `mm404/tautulli-exporter` | `latest`, `<version>` (e.g. `1.2.3`) |
| GHCR | `ghcr.io/mm503/tautulli-exporter` | `latest`, `<version>` (e.g. `1.2.3`) |

Both are multi-arch (`linux/amd64`, `linux/arm64`) and built from the same release commit.

### Development Images

Every push to a non-`main` branch publishes an unstable image to GHCR only:

| Registry | Image | Tags |
|----------|-------|------|
| GHCR | `ghcr.io/mm503/tautulli-exporter-dev` | `dev-<version>-<branch>-<sha>` |

These are for testing branches before release and are not intended for production use.

## Installation

### Docker

```bash
docker run -d \
  -e TAUTULLI_URL=http://your-tautulli:8181 \
  -e TAUTULLI_API_KEY=your-api-key \
  -p 8000:8000 \
  mm404/tautulli-exporter
```

Or from GHCR:

```bash
docker run -d \
  -e TAUTULLI_URL=http://your-tautulli:8181 \
  -e TAUTULLI_API_KEY=your-api-key \
  -p 8000:8000 \
  ghcr.io/mm503/tautulli-exporter
```

### Helm (recommended for Kubernetes)

```bash
helm repo add tautulli-exporter https://mm503.github.io/tautulli-exporter
helm install tautulli-exporter tautulli-exporter/tautulli-exporter \
  --set config.tautulliUrl=http://tautulli.default.svc.cluster.local:8181 \
  --set config.apiKey=your-tautulli-api-key
```

To use a pre-existing Secret instead of putting the API key in values:

```bash
helm install tautulli-exporter tautulli-exporter/tautulli-exporter \
  --set config.tautulliUrl=http://tautulli.default.svc.cluster.local:8181 \
  --set config.existingSecret.name=tautulli-credentials \
  --set config.existingSecret.key=api-key
```

Notable values (see [charts/tautulli-exporter/values.yaml](charts/tautulli-exporter/values.yaml) for all options):

| Value | Default | Description |
|-------|---------|-------------|
| `config.tautulliUrl` | - | Tautulli server URL (required) |
| `config.apiKey` | - | API key, stored in a chart-managed Secret |
| `config.existingSecret.name` | - | Use an existing Secret for the API key instead |
| `config.logLevel` | `INFO` | Exporter log level |
| `config.scrapeInterval` | `30` | Seconds between Tautulli API calls |
| `serviceMonitor.enabled` | `false` | Create a ServiceMonitor for the Prometheus Operator |
| `service.port` | `8000` | Metrics/health port |
| `image.repository` | `ghcr.io/mm503/tautulli-exporter` | Image repository; set to `mm404/tautulli-exporter` to pull from Docker Hub |
| `image.tag` | chart `appVersion` | Image tag override |

The chart pulls from GHCR by default. To use Docker Hub instead:

```bash
helm install tautulli-exporter tautulli-exporter/tautulli-exporter \
  --set config.tautulliUrl=http://tautulli.default.svc.cluster.local:8181 \
  --set config.apiKey=your-tautulli-api-key \
  --set image.repository=mm404/tautulli-exporter
```

### Kubernetes (plain manifests)

Remember to update both URL to reflect your Tautulli deployment name and namespace.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tautulli-exporter
spec:
  replicas: 1
  selector:
    matchLabels:
      app: tautulli-exporter
  template:
    metadata:
      labels:
        app: tautulli-exporter
    spec:
      containers:
      - name: tautulli-exporter
        # or ghcr.io/mm503/tautulli-exporter:latest
        image: mm404/tautulli-exporter:latest
        securityContext:
          runAsNonRoot: true
          runAsUser: 1000
          runAsGroup: 1000
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          seccompProfile:
            type: RuntimeDefault
          capabilities:
            drop: ["ALL"]
        ports:
        - containerPort: 8000
          name: metrics
        env:
        - name: TAUTULLI_URL
          value: "http://tautulli.default.svc.cluster.local:8181"
        - name: TAUTULLI_API_KEY
          valueFrom:
            secretKeyRef:
              name: tautulli-credentials
              key: api-key
        - name: LOG_LEVEL
          value: "INFO"
        # Keep below limits.memory so the Go GC applies back-pressure on a
        # heap spike instead of the container being OOM-killed.
        - name: GOMEMLIMIT
          value: "24MiB"
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8000
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /ready
            port: 8000
          initialDelaySeconds: 5
          periodSeconds: 10
        resources:
          requests:
            memory: "16Mi"
            cpu: "50m"
          limits:
            memory: "32Mi"
---
apiVersion: v1
kind: Service
metadata:
  name: tautulli-exporter
  labels:
    app: tautulli-exporter
spec:
  ports:
  - port: 8000
    targetPort: 8000
    name: metrics
  selector:
    app: tautulli-exporter
---
apiVersion: v1
kind: Secret
metadata:
  name: tautulli-credentials
type: Opaque
stringData:
  api-key: "your-tautulli-api-key"
```

### Prometheus Configuration

If you installed via Helm with `serviceMonitor.enabled=true`, the Prometheus Operator discovers the exporter automatically. Otherwise, add to your `prometheus.yml` (update the `target` to reflect your deployment placement):

```yaml
scrape_configs:
  - job_name: 'plex'
    static_configs:
      - targets: ['tautulli-exporter.default.svc.cluster.local:8000']
    scrape_interval: 30s
```

## Grafana Dashboard

Example queries for Grafana:

**Active Streams Panel:**
```promql
plex_active_streams_total
```

**Stream Types Pie Chart:**
```promql
plex_active_streams_direct
plex_active_streams_transcode
```

**Direct Play vs Direct Stream:**
```promql
plex_active_streams_direct_play
plex_active_streams_direct_stream
```

**Transcoding Breakdown:**
```promql
plex_transcode_video_sessions
plex_transcode_audio_sessions
plex_transcode_container_sessions
```

**Bandwidth:**
```promql
plex_bandwidth_total_kbps
plex_bandwidth_lan_kbps
plex_bandwidth_wan_kbps
```

## Troubleshooting

### Exporter won't start
- Check `TAUTULLI_URL` is accessible from the container
- Verify `TAUTULLI_API_KEY` is correct (found in Tautulli Settings → Web Interface)
- Look for validation errors in logs

### Metrics show as 0
- Ensure Tautulli has API access enabled
- Check if there are active streams in Plex
- Verify network connectivity between exporter and Tautulli

### Readiness probe failing
- Check logs for connection errors
- Verify Tautulli is running and accessible
- Ensure API key has proper permissions

### Recovering from an outage
Failed scrapes are logged at `ERROR` with a `(failure N/5)` counter. When a scrape
succeeds again, the exporter logs a single `INFO` line so recovery is explicit:

```json
{"timestamp":"2026-08-10T03:28:55","level":"INFO","logger":"plex_exporter","message":"Collection resumed after 2 consecutive failures over 60s"}
```

The duration spans the first failure through the recovering scrape, so at the
default 30s `SCRAPE_INTERVAL` two consecutive failures cover roughly 60s. If the
circuit breaker had opened (5 consecutive failures), the same line ends with
`; circuit breaker closed`.

This line is emitted at `INFO`, while the failures themselves are at `ERROR`. Run
with `LOG_LEVEL=INFO` (the default) or `DEBUG` to see it — at `WARNING` or `ERROR`
the failures are logged but the recovery is filtered out, making a resolved outage
look ongoing.

### Debug logging
Set `LOG_LEVEL=DEBUG` to see detailed information including:
- API request URL
- Health check requests
- Metrics update confirmation

## Development

### Running locally
```bash
export TAUTULLI_URL=http://localhost:8181
export TAUTULLI_API_KEY=your-key
export LOG_LEVEL=DEBUG
go run .
```

### Testing
```bash
# Run unit tests
go test ./... -cover

# Check metrics
curl http://localhost:8000/metrics

# Check health
curl http://localhost:8000/healthz
curl http://localhost:8000/ready
```

## License

MIT License

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request
