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
- `/ready` - Kubernetes readiness probe (returns 503 if scraping fails)

## Installation

### Docker

```bash
docker run -d \
  -e TAUTULLI_URL=http://your-tautulli:8181 \
  -e TAUTULLI_API_KEY=your-api-key \
  -p 8000:8000 \
  mm404/tautulli-exporter
```

### Kubernetes

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
        image: mm404/tautulli-exporter:latest
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
            memory: "64Mi"
            cpu: "50m"
          limits:
            memory: "128Mi"
            cpu: "200m"
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

Add to your `prometheus.yml` (update the `target` to reflect your deployment placement):

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
python3 main.py
```

### Testing
```bash
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
