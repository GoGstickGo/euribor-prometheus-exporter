# Euribor Prometheus Exporter

A high-performance Prometheus exporter written in Go that fetches Euribor interest rates from the European Central Bank (ECB) and exposes them as metrics.

## Features

- ✅ **High Performance**: Built in Go for minimal resource usage
- ✅ **Official Data Source**: Fetches from ECB Statistical Data Warehouse
- ✅ **All Maturities**: Supports 1M, 3M, 6M, 12M
- ✅ **Prometheus Native**: Uses official Prometheus client library
- ✅ **Graceful Shutdown**: Handles SIGTERM/SIGINT properly
- ✅ **Health Checks**: Built-in `/health` endpoint
- ✅ **Configurable**: Command-line flags and environment variables
- ✅ **Docker Ready**: Multi-stage builds for minimal image size

## Installation

### Option 1: Pre-built Binary

Download from releases and extract:
```bash
wget https://github.com/yourusername/euribor-exporter/releases/latest/download/euribor-exporter-linux-amd64
chmod +x euribor-exporter-linux-amd64
mv euribor-exporter-linux-amd64 /usr/local/bin/euribor-exporter
```

### Option 2: Build from Source

Requirements:
- Go 1.21 or higher
- Make (optional, for convenience)

```bash
# Clone repository
git clone https://github.com/yourusername/euribor-exporter
cd euribor-exporter

# Download dependencies
go mod download

# Build
go build -o euribor-exporter main.go

# Or use Make
make build
```

### Option 3: Docker

```bash
# Using docker-compose (easiest)
docker-compose -f docker-compose.go.yml up -d

# Or build and run manually
docker build -f Dockerfile.go -t euribor-exporter .
docker run -p 9100:9100 euribor-exporter
```

## Usage

### Command-Line Options

```bash
./euribor-exporter [options]

Options:
  --listen-address string
        Address to listen on for web interface and telemetry (default ":9100")
  --metrics-path string
        Path under which to expose metrics (default "/metrics")
  --scrape-interval duration
        Interval between scrapes (default 1h)

Environment Variables:
  LOG_LEVEL    Set logging level: debug, info, warn, error (default: info)
```

### Examples

```bash
# Run with default settings (port 9100, scrape every hour)
./euribor-exporter

# Custom port and shorter interval
./euribor-exporter --listen-address=:8080 --scrape-interval=30m

# Debug mode
LOG_LEVEL=debug ./euribor-exporter

# Production mode with custom settings
./euribor-exporter \
    --listen-address=:9100 \
    --scrape-interval=1h \
    --metrics-path=/metrics
```

## Endpoints

- `http://localhost:9100/metrics` - Prometheus metrics
- `http://localhost:9100/health` - Health check endpoint
- `http://localhost:9100/` - Information page

## Metrics

```promql
# Current Euribor rate for each maturity
euribor_rate_percent{maturity="1M|3M|6M|12M"}

# Timestamp of last successful fetch
euribor_last_update_timestamp{maturity="1M|3M|6M|12M"}

# Duration of scrape operation in seconds
euribor_scrape_duration_seconds{maturity="1M|3M|6M|12M"}

# Scrape success indicator (1=success, 0=failure)
euribor_scrape_success{maturity="1M|3M|6M|12M"}

# Exporter information
euribor_exporter_info{version="1.0.0", source="ECB Statistical Data Warehouse"}
```

## Prometheus Configuration

Add to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'euribor'
    scrape_interval: 60s
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

## Development

### Building

```bash
# Build for current platform
make build

# Build for all platforms
make build-all

# Run in development mode (debug logs, 30s interval)
make run-dev

# Run tests
make test

# Run tests with coverage
make test-coverage

# Format code
make fmt

# Clean build artifacts
make clean
```

## Monitoring Your Mortgage

### Calculate Monthly Payment Impact

```promql
# Monthly payment change per 0.1% rate increase
(euribor_rate_percent{maturity="3M"} * 260000) / 1200
```

### Alert Examples

```yaml
# Alert when rate crosses threshold
- alert: EuriborHighRate
  expr: euribor_rate_percent{maturity="3M"} > 3.5
  for: 2h
  annotations:
    summary: "Euribor rate above 3.5%"

# Alert on significant change
- alert: EuriborRateSpike
  expr: abs(rate(euribor_rate_percent{maturity="3M"}[24h])) > 0.2
  for: 1h
  annotations:
    summary: "Euribor changed significantly"
```

## Troubleshooting

### Test ECB connectivity

```bash
# Test direct API access
curl "https://data-api.ecb.europa.eu/service/data/FM/M.U2.EUR.RT.MM.EURIBOR3MD_.HSTA?format=jsondata&lastNObservations=1"
```

### Common Issues

**Problem**: Cannot bind to port 9100
```bash
# Check what's using the port
sudo lsof -i :9100

# Run on different port
./euribor-exporter --listen-address=:8080
```

**Problem**: Permission denied
```bash
# Check file permissions
ls -la euribor-exporter

# Make executable
chmod +x euribor-exporter
```

**Problem**: Connection timeout to ECB
```bash
# Check network connectivity
curl -I https://data-api.ecb.europa.eu

# Check DNS resolution
nslookup data-api.ecb.europa.eu

# Check firewall
sudo iptables -L | grep OUTPUT
```

## Security

The exporter implements several security best practices:

- ✅ Runs as non-root user in Docker
- ✅ Read-only root filesystem support
- ✅ No new privileges
- ✅ Minimal Alpine base image
- ✅ CA certificates for HTTPS
- ✅ Resource limits (systemd)
- ✅ Graceful shutdown handling
- ✅ No sensitive data logging

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

MIT License - see LICENSE file for details

## Data Source

- **Provider**: European Central Bank (ECB)
- **API**: Statistical Data Warehouse
- **URL**: https://data-api.ecb.europa.eu
- **Publisher**: European Money Markets Institute (EMMI)
- **Update Frequency**: Daily (~11:00 CET)

## Acknowledgments

- [Prometheus](https://prometheus.io/) for the excellent monitoring system
- [ECB](https://www.ecb.europa.eu/) for providing open data access
- [Go Prometheus Client](https://github.com/prometheus/client_golang) for the metrics library
