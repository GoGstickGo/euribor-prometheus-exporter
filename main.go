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

	"github.com/GoGstickGo/euribor-exporter/scraper"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

const (
	namespace = "euribor"
	ecbAPIURL = "https://data-api.ecb.europa.eu/service/data/FM"
)

var (
	version = "dev" // Default for local builds
	commit  = "none"
	date    = "unknown"
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

	euriborPubDate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_publication_date",
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

	euriborDailyRate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "daily_rate_percent",
			Help:      "Daily Euribor rate in percent (scraped from euribor-rates.eu)",
		},
		[]string{"maturity"},
	)

	euriborDailyPublicationDate = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "daily_publication_date_timestamp",
			Help:      "ECB publication date of the daily Euribor rate (Unix timestamp)",
		},
		[]string{"maturity"},
	)

	euriborDailyScrapeSuccess = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "daily_scrape_success",
			Help:      "Whether the last daily scrape was successful (1 = success, 0 = failure)",
		},
		[]string{"maturity"},
	)

	euriborDailyScrapeDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "daily_scrape_duration_seconds",
			Help:      "Duration of daily Euribor scrape in seconds",
		},
		[]string{"maturity"},
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

// EuriborExporter handles fetching and exposing Euribor rates from multiple sources
type EuriborExporter struct {
	client     *http.Client
	scraper    *scraper.Scraper
	ecbEnabled bool // Flag to enable/disable ECB source
}

// NewEuriborExporter creates a new exporter instance
func NewEuriborExporter(enableECB bool) *EuriborExporter {
	return &EuriborExporter{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		scraper:    scraper.New(log),
		ecbEnabled: enableECB,
	}
}

// FetchRateFromECB fetches the Euribor rate from ECB API (monthly data)
func (e *EuriborExporter) FetchRateFromECB(maturity string) (float64, time.Time, error) {
	maturityCode, exists := maturities[maturity]
	if !exists {
		return 0, time.Time{}, fmt.Errorf("invalid maturity: %s", maturity)
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
		return 0, time.Time{}, fmt.Errorf("failed to fetch data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, time.Time{}, fmt.Errorf("ECB API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("failed to read response: %w", err)
	}

	var ecbResp ECBResponse
	if err := json.Unmarshal(body, &ecbResp); err != nil {
		return 0, time.Time{}, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Extract the rate from the response
	if len(ecbResp.DataSets) == 0 {
		return 0, time.Time{}, fmt.Errorf("no datasets in response")
	}

	series, exists := ecbResp.DataSets[0].Series["0:0:0:0:0:0:0"]
	if !exists {
		return 0, time.Time{}, fmt.Errorf("series not found in response")
	}

	if len(series.Observations) == 0 {
		return 0, time.Time{}, fmt.Errorf("no observations in series")
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
		return 0, time.Time{}, fmt.Errorf("observation is empty")
	}

	rate := observations[0]

	// Parse publication date (monthly format: "2025-11")
	pubDate := time.Now() // Default to current time

	if len(ecbResp.Structure.Dimensions.Observation) > 0 {
		timeDim := ecbResp.Structure.Dimensions.Observation[0]
		if len(timeDim.Values) > 0 {
			// Get the last value (most recent)
			latestIdx := len(timeDim.Values) - 1
			dateStr := timeDim.Values[latestIdx].ID

			// ECB monthly data returns "2025-11" format
			parsed, err := time.Parse("2006-01", dateStr)
			if err != nil {
				log.WithFields(logrus.Fields{
					"maturity": maturity,
					"date_str": dateStr,
					"error":    err,
				}).Warn("Failed to parse ECB publication date, using current time")
			} else {
				// Set to last day of the month for more accuracy
				pubDate = time.Date(parsed.Year(), parsed.Month()+1, 0, 0, 0, 0, 0, time.UTC)

				log.WithFields(logrus.Fields{
					"maturity": maturity,
					"pub_date": pubDate.Format("2006-01"),
				}).Debug("Parsed ECB publication date")
			}
		}
	}

	return rate, pubDate, nil
}

// FetchRateFromWeb fetches the Euribor rate from web scraper (daily data)
func (e *EuriborExporter) FetchRateFromWeb(maturity string) (float64, time.Time, error) {
	data, err := e.scraper.FetchRate(maturity)
	if err != nil {
		return 0, time.Time{}, err
	}

	return data.Rate, data.PublicationDate, nil
}

// UpdateMetrics fetches latest rates from both sources and updates Prometheus metrics
func (e *EuriborExporter) UpdateMetrics() {
	maturitiesList := scraper.GetSupportedMaturities()

	for _, maturity := range maturitiesList {
		// Fetch from daily web scraper
		e.updateDailyMetrics(maturity)

		// Fetch from ECB (monthly) if enabled
		if e.ecbEnabled {
			e.updateECBMetrics(maturity)
		}
	}
}

// updateDailyMetrics fetches and updates daily scraped metrics
func (e *EuriborExporter) updateDailyMetrics(maturity string) {
	startTime := time.Now()

	rate, pubDate, err := e.FetchRateFromWeb(maturity)
	duration := time.Since(startTime).Seconds()

	euriborDailyScrapeDuration.WithLabelValues(maturity).Set(duration)

	if err != nil {
		log.WithFields(logrus.Fields{
			"maturity": maturity,
			"source":   "daily-scraper",
			"error":    err,
		}).Error("Failed to fetch daily Euribor rate")
		euriborDailyScrapeSuccess.WithLabelValues(maturity).Set(0)
		return
	}

	// Update daily metrics
	euriborDailyRate.WithLabelValues(maturity).Set(rate)
	euriborDailyPublicationDate.WithLabelValues(maturity).Set(float64(pubDate.Unix()))
	euriborDailyScrapeSuccess.WithLabelValues(maturity).Set(1)

	log.WithFields(logrus.Fields{
		"maturity": maturity,
		"source":   "daily-scraper",
		"rate":     rate,
		"pub_date": pubDate.Format("2006-01-02"),
		"duration": duration,
	}).Info("Updated daily Euribor metric")
}

// updateECBMetrics fetches and updates ECB monthly metrics
func (e *EuriborExporter) updateECBMetrics(maturity string) {
	// Only fetch ECB data if maturity exists in maturities map
	if _, exists := maturities[maturity]; !exists {
		log.WithFields(logrus.Fields{
			"maturity": maturity,
			"source":   "ecb",
		}).Debug("Skipping ECB fetch - maturity not supported by ECB API")
		return
	}

	startTime := time.Now()

	rate, pubDate, err := e.FetchRateFromECB(maturity)
	duration := time.Since(startTime).Seconds()

	euriborScrapeDuration.WithLabelValues(maturity).Set(duration)

	if err != nil {
		log.WithFields(logrus.Fields{
			"maturity": maturity,
			"source":   "ecb",
			"error":    err,
		}).Error("Failed to fetch ECB Euribor rate")
		euriborScrapeSuccess.WithLabelValues(maturity).Set(0)
		return
	}

	// Update ECB metrics
	euriborRate.WithLabelValues(maturity).Set(rate)
	euriborPubDate.WithLabelValues(maturity).Set(float64(pubDate.Unix()))
	euriborScrapeSuccess.WithLabelValues(maturity).Set(1)

	log.WithFields(logrus.Fields{
		"maturity": maturity,
		"source":   "ecb",
		"rate":     rate,
		"pub_date": pubDate.Format("2006-01-02"),
		"duration": duration,
	}).Info("Updated ECB Euribor metric")
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
	prometheus.MustRegister(euriborPubDate)

	prometheus.MustRegister(euriborDailyRate)
	prometheus.MustRegister(euriborDailyPublicationDate)
	prometheus.MustRegister(euriborDailyScrapeSuccess)
	prometheus.MustRegister(euriborDailyScrapeDuration)

	// Set exporter info
	euriborInfo.WithLabelValues(version, "dual-source: ECB + daily scraper").Set(1)

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

	// Check if ECB source should be enabled (default: true for backward compatibility)
	enableECB := os.Getenv("ENABLE_ECB") != "false"

	log.WithFields(logrus.Fields{
		"version":         version,
		"listen_address":  *listenAddress,
		"metrics_path":    *metricsPath,
		"scrape_interval": *scrapeInterval,
		"ecb_enabled":     enableECB,
	}).Info("Starting Euribor Prometheus Exporter")

	// Create exporter
	exporter := NewEuriborExporter(enableECB)

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
