# Tautulli Exporter

## Quick Start

```bash
docker run -d \
  -e TAUTULLI_URL=http://your-tautulli:8181 \
  -e TAUTULLI_API_KEY=your-api-key \
  -p 8000:8000 \
  mm404/tautulli-exporter
```

## Endpoints

- `/metrics` - Prometheus metrics
- `/healthz` - Liveness probe (always 200 if running)
- `/ready` - Readiness probe (503 if scraping fails)

## Metrics

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

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TAUTULLI_URL` | Yes | - | Tautulli server URL (e.g., `http://192.168.1.100:8181`) |
| `TAUTULLI_API_KEY` | Yes | - | Tautulli API key from Settings → Web Interface |
| `METRICS_PORT` | No | `8000` | Port for metrics/health endpoints |
| `SCRAPE_INTERVAL` | No | `30` | Seconds between Tautulli API calls |
| `REQUEST_TIMEOUT` | No | `10` | HTTP request timeout in seconds |
| `LOG_LEVEL` | No | `INFO` | Logging level (DEBUG, INFO, WARNING, ERROR) |

## Prometheus configuration

```yaml
scrape_configs:
  - job_name: 'tautulli'
    static_configs:
      - targets: ['_IP_OF_YOUR_EXPORTER_:8000']
    metrics_path: '/metrics'
    scrape_interval: 30s
```

See [README.md](https://github.com/mm503/tautulli-exporter/blob/main/README.md) for more details.
