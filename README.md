# Euribor Prometheus Exporter - Complete Documentation

A high-performance Prometheus exporter written in Go that fetches **daily** Euribor interest rates by scraping [euribor-rates.eu](https://www.euribor-rates.eu/), with optional ECB monthly data support.


## 🎯 Features

- ✅ **Daily Data**: Scrapes actual daily Euribor rates (24h delayed per EMMI requirements)
- ✅ **All Maturities**: 1W, 1M, 3M, 6M, 12M support
- ✅ **Dual Source**: Optional ECB monthly data for comparison
- ✅ **ECB Publication Dates**: Tracks actual rate publication dates, not scrape times
- ✅ **Free**: No API keys required
- ✅ **Prometheus Native**: Official Prometheus client library
- ✅ **Production Ready**: Health checks, graceful shutdown, resource limits
- ✅ **Kubernetes Native**: ServiceMonitor, auto-discovery with kube-prometheus-stack
- ✅ **Docker Ready**: Multi-stage builds, non-root user, minimal Alpine image

---

## 📊 Data Sources

### Primary: Daily Web Scraper (euribor-rates.eu)
- **Update Frequency**: Daily (business days only: Mon-Fri)
- **Delay**: 24 hours (per EMMI requirements)
- **Reliability**: High (free, public data)
- **Metrics Prefix**: `euribor_daily_*`
- **Data Quality**: Parsed from HTML tables

### Optional: ECB Monthly Data
- **Update Frequency**: Monthly
- **Source**: ECB Statistical Data Warehouse API
- **Reliability**: Official data from European Central Bank
- **Metrics Prefix**: `euribor_*`
- **Use Case**: Cross-validation, trend analysis

**Default behavior**: Daily scraper only (ECB optional via `ENABLE_ECB=true`)

---

## ⚙️ Configuration

### Command-Line Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--listen-address` | `:9100` | Address to listen on for web interface |
| `--metrics-path` | `/metrics` | Path under which to expose metrics |
| `--scrape-interval` | `1h` | Interval between scrapes (e.g., 30m, 1h, 2h) |

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `LOG_LEVEL` | `info` | Logging level: `debug`, `info`, `warn`, `error` |
| `ENABLE_ECB` | `true` | Enable ECB monthly data fetching (`true`/`false`) |

### Configuration Examples

```bash
# Daily scraper only (recommended for most users)
./euribor-exporter

# Daily + ECB monthly data for comparison
ENABLE_ECB=true ./euribor-exporter

# Debug mode with 30-minute scraping
LOG_LEVEL=debug ./euribor-exporter --scrape-interval=30m

# Custom port
./euribor-exporter --listen-address=:8080

# Production setup
./euribor-exporter \
  --listen-address=:9100 \
  --scrape-interval=1h \
  --metrics-path=/metrics
```

---

## 📈 Metrics

### Daily Scraped Metrics (Primary)

These are your main metrics from the daily web scraper:

```promql
# Current Euribor rate for each maturity (percent)
euribor_daily_rate_percent{maturity="1W|1M|3M|6M|12M"}
# Example value: 2.294 (means 2.294%)

# ECB publication date (Unix timestamp in seconds)
euribor_daily_publication_date_timestamp{maturity="1W|1M|3M|6M|12M"}
# Example: 1734048000 (2024-12-13 00:00:00 UTC)

# Scrape success indicator
euribor_daily_scrape_success{maturity="1W|1M|3M|6M|12M"}
# 1 = success, 0 = failure

# Duration of scrape operation (seconds)
euribor_daily_scrape_duration_seconds{maturity="1W|1M|3M|6M|12M"}
# Example: 0.523 (523 milliseconds)
```

### ECB Monthly Metrics (Optional)

Only exposed if `ENABLE_ECB=true`:

```promql
# Monthly Euribor rate from ECB API (percent)
euribor_rate_percent{maturity="1M|3M|6M|12M"}

# ECB publication date (Unix timestamp)
euribor_ecb_publication_date_timestamp{maturity="1M|3M|6M|12M"}

# Scrape success indicator
euribor_ecb_scrape_success{maturity="1M|3M|6M|12M"}

# Scrape duration (seconds)
euribor_ecb_scrape_duration_seconds{maturity="1M|3M|6M|12M"}
```

### Info Metric

```promql
# Exporter version and source information
euribor_exporter_info{version="1.1.0", source="dual-source: ECB + daily scraper"}
# Value is always 1
```

### Metric Label Values

**maturity label values:**
- `1W` - 1 week
- `1M` - 1 month
- `3M` - 3 months
- `6M` - 6 months
- `12M` - 12 months (1 year)

---

## 🌐 Endpoints

| Endpoint | Description |
|----------|-------------|
| `http://localhost:9100/metrics` | Prometheus metrics in text format |
| `http://localhost:9100/health` | Health check (returns `OK`) |
| `http://localhost:9100/` | Information page with exporter details |

---

## 📊 Prometheus Configuration

### Standalone Prometheus

Add to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'euribor'
    scrape_interval: 1h      # Match exporter scrape interval
    scrape_timeout: 30s
    static_configs:
      - targets: ['localhost:9100']
        labels:
          service: 'euribor-exporter'
          environment: 'production'
```

Reload Prometheus:
```bash
curl -X POST http://localhost:9090/-/reload
# or
systemctl reload prometheus
```

### Kubernetes (kube-prometheus-stack)

The exporter includes a `ServiceMonitor` for automatic discovery.

Already included in `k8s-deployment.yaml`:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: euribor-exporter-daily
  namespace: monitoring
  labels:
    release: prometheus  # Must match your Prometheus release name
spec:
  selector:
    matchLabels:
      app: euribor-exporter
      source: daily-scraper
  endpoints:
  - port: metrics
    interval: 1h
    scrapeTimeout: 30s
    path: /metrics
```

**Prometheus will automatically discover and scrape the exporter** if labels match.

**Verify it's working:**
```bash
# Check ServiceMonitor exists
kubectl get servicemonitor -n monitoring euribor-exporter-daily

# Check Prometheus targets
# Go to: http://localhost:9090/targets
# Look for: monitoring/euribor-exporter-daily/0 (should be UP)
```
---

## 🏗️ Architecture

```
┌─────────────────────┐
│ euribor-rates.eu    │  Primary source (daily data)
│ (HTML page)         │
└──────────┬──────────┘
           │
           │ HTTP GET (scrape every hour)
           │ Parse HTML tables
           ▼
┌─────────────────────┐
│ Euribor Exporter    │
│ - Web scraper       │
│ - ECB API (opt)     │◄─────────┐
│ - Exposes metrics   │           │
└──────────┬──────────┘           │ Optional monthly data
           │                       │
           │ :9100/metrics         │
           ▼                       │
┌─────────────────────┐           │
│ Prometheus          │           │
│ - Scrapes metrics   │           │
│ - Stores timeseries │           │
│ - Evaluates alerts  │           │
└──────────┬──────────┘    ┌──────┴──────┐
           │                │ ECB API     │
           ▼                │ (Monthly)   │
┌─────────────────────┐    └─────────────┘
│ Grafana/Alertmanager│
│ - Dashboards        │
│ - Notifications     │
└─────────────────────┘
```

**Data Flow:**
1. Exporter scrapes euribor-rates.eu every hour
2. Parses HTML tables for rates and dates
3. Optionally fetches ECB monthly data
4. Exposes metrics on `/metrics` endpoint
5. Prometheus scrapes metrics every hour
6. Grafana visualizes data
7. Alertmanager sends notifications

---

## 📚 Data Sources

### Primary: euribor-rates.eu
- **Provider**: Triami Media
- **Data**: Daily Euribor rates (24h delayed)
- **URL**: https://www.euribor-rates.eu/
- **Usage**: Non-commercial, personal use
- **Update**: Daily on business days (Mon-Fri)
- **Publication time**: ~11:00 CET

### Secondary: ECB Statistical Data Warehouse
- **Provider**: European Central Bank (ECB)
- **Data**: Monthly Euribor rates
- **URL**: https://data-api.ecb.europa.eu/
- **Publisher**: European Money Markets Institute (EMMI)
- **Update**: Monthly (end of month)
- **Official**: Yes (authoritative source)

---