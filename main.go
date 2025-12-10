package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

const (
	namespace = "euribor"
	ecbAPIURL = "https://data-api.ecb.europa.eu/service/data/FM"
)

var (
	log = logrus.New()

	// Command-line flags
	listenAddress  = flag.String("listen-address", ":9100", "Address to listen on for web interface and telemetry")
	metricsPath    = flag.String("metrics-path", "/metrics", "Path under which to expose metrics")
	scrapeInterval = flag.Duration("scrape-interval", 1*time.Hour, "Interval between scrapes")
)

// Prometheus metrics
var (
	euriborRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "rate_percent",
			Help:      "Current Euribor rate in percent",
		},
		[]string{"maturity"},
	)

	euriborLastUpdate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_update_timestamp",
			Help:      "Timestamp of last successful Euribor data fetch",
		},
		[]string{"maturity"},
	)

	euriborScrapeDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "scrape_duration_seconds",
			Help:      "Duration of Euribor data scrape",
		},
		[]string{"maturity"},
	)

	euriborScrapeSuccess = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "scrape_success",
			Help:      "Whether the last scrape was successful (1 = success, 0 = failure)",
		},
		[]string{"maturity"},
	)

	euriborInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "exporter_info",
			Help:      "Information about the Euribor exporter",
		},
		[]string{"version", "source"},
	)
)

// Maturity codes mapping
var maturities = map[string]string{
	"1M":  "1MD_",
	"3M":  "3MD_",
	"6M":  "6MD_",
	"12M": "1YD_",
}

// ECB API response structures
type ECBResponse struct {
	DataSets  []DataSet `json:"dataSets"`
	Structure Structure `json:"structure"`
}

type DataSet struct {
	Series map[string]Series `json:"series"`
}

type Series struct {
	Observations map[string][]float64 `json:"observations"`
}

type Structure struct {
	Dimensions Dimensions `json:"dimensions"`
}

type Dimensions struct {
	Observation []Dimension `json:"observation"`
}

type Dimension struct {
	Values []DimensionValue `json:"values"`
}

type DimensionValue struct {
	ID string `json:"id"`
}

// EuriborExporter handles fetching and exposing Euribor rates
type EuriborExporter struct {
	client *http.Client
}

// NewEuriborExporter creates a new exporter instance
func NewEuriborExporter() *EuriborExporter {
	return &EuriborExporter{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// FetchRate fetches the Euribor rate for a specific maturity from ECB
func (e *EuriborExporter) FetchRate(maturity string) (float64, error) {
	maturityCode, exists := maturities[maturity]
	if !exists {
		return 0, fmt.Errorf("invalid maturity: %s", maturity)
	}

	// Build the query
	key := fmt.Sprintf("M.U2.EUR.RT.MM.EURIBOR%s.HSTA", maturityCode)
	url := fmt.Sprintf("%s/%s?format=jsondata&detail=dataonly&lastNObservations=1", ecbAPIURL, key)

	log.WithFields(logrus.Fields{
		"maturity": maturity,
		"url":      url,
	}).Debug("Fetching Euribor rate from ECB")

	resp, err := e.client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("ECB API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	var ecbResp ECBResponse
	if err := json.Unmarshal(body, &ecbResp); err != nil {
		return 0, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Extract the rate from the response
	if len(ecbResp.DataSets) == 0 {
		return 0, fmt.Errorf("no datasets in response")
	}

	series, exists := ecbResp.DataSets[0].Series["0:0:0:0:0:0:0"]
	if !exists {
		return 0, fmt.Errorf("series not found in response")
	}

	if len(series.Observations) == 0 {
		return 0, fmt.Errorf("no observations in series")
	}

	// Find the latest observation
	var latestKey string
	for key := range series.Observations {
		if latestKey == "" || key > latestKey {
			latestKey = key
		}
	}

	observations := series.Observations[latestKey]
	if len(observations) == 0 {
		return 0, fmt.Errorf("observation is empty")
	}

	rate := observations[0]
	log.WithFields(logrus.Fields{
		"maturity": maturity,
		"rate":     rate,
	}).Info("Successfully fetched Euribor rate")

	return rate, nil
}

// UpdateMetrics fetches latest rates and updates Prometheus metrics
func (e *EuriborExporter) UpdateMetrics() {
	for maturity := range maturities {
		startTime := time.Now()

		rate, err := e.FetchRate(maturity)
		duration := time.Since(startTime).Seconds()

		euriborScrapeDuration.WithLabelValues(maturity).Set(duration)

		if err != nil {
			log.WithFields(logrus.Fields{
				"maturity": maturity,
				"error":    err,
			}).Error("Failed to fetch Euribor rate")
			euriborScrapeSuccess.WithLabelValues(maturity).Set(0)
			continue
		}

		// Update metrics
		euriborRate.WithLabelValues(maturity).Set(rate)
		euriborLastUpdate.WithLabelValues(maturity).Set(float64(time.Now().Unix()))
		euriborScrapeSuccess.WithLabelValues(maturity).Set(1)

		log.WithFields(logrus.Fields{
			"maturity": maturity,
			"rate":     rate,
			"duration": duration,
		}).Info("Updated Euribor metric")
	}
}

// Run starts the periodic metric updates
func (e *EuriborExporter) Run(interval time.Duration, stopCh <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Initial update
	log.Info("Performing initial metrics update")
	e.UpdateMetrics()

	for {
		select {
		case <-ticker.C:
			log.Info("Performing scheduled metrics update")
			e.UpdateMetrics()
		case <-stopCh:
			log.Info("Stopping exporter")
			return
		}
	}
}

func init() {
	// Register metrics
	prometheus.MustRegister(euriborRate)
	prometheus.MustRegister(euriborLastUpdate)
	prometheus.MustRegister(euriborScrapeDuration)
	prometheus.MustRegister(euriborScrapeSuccess)
	prometheus.MustRegister(euriborInfo)

	// Set exporter info
	euriborInfo.WithLabelValues("1.0.0", "ECB Statistical Data Warehouse").Set(1)

	// Configure logging
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	log.SetLevel(logrus.InfoLevel)
}

func main() {
	flag.Parse()

	// Set log level from environment
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		if lvl, err := logrus.ParseLevel(level); err == nil {
			log.SetLevel(lvl)
		}
	}

	log.WithFields(logrus.Fields{
		"listen_address":  *listenAddress,
		"metrics_path":    *metricsPath,
		"scrape_interval": *scrapeInterval,
	}).Info("Starting Euribor Prometheus Exporter")

	// Create exporter
	exporter := NewEuriborExporter()

	// Setup signal handling for graceful shutdown
	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Start the exporter in a goroutine
	go exporter.Run(*scrapeInterval, stopCh)

	// Setup HTTP server
	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html>
<head><title>Euribor Exporter</title></head>
<body>
<h1>Euribor Prometheus Exporter</h1>
<p><a href="%s">Metrics</a></p>
<h2>Configuration</h2>
<ul>
<li>Scrape Interval: %s</li>
<li>Maturities: 1M, 3M, 6M, 12M</li>
<li>Data Source: ECB Statistical Data Warehouse</li>
</ul>
</body>
</html>`, *metricsPath, *scrapeInterval)
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
	})

	// Start HTTP server in a goroutine
	server := &http.Server{
		Addr:         *listenAddress,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.WithField("address", *listenAddress).Info("Starting HTTP server")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.WithError(err).Fatal("Failed to start HTTP server")
		}
	}()

	// Wait for shutdown signal
	<-sigCh
	log.Info("Received shutdown signal")

	// Graceful shutdown
	close(stopCh)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.WithError(err).Error("Server shutdown error")
	}

	log.Info("Exporter stopped")
}
