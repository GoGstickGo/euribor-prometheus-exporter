package scraper

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/sirupsen/logrus"
)

const (
	euriborBaseURL = "https://www.euribor-rates.eu/en/current-euribor-rates"
)

// Maturity URL mappings
var maturityURLs = map[string]string{
	"1W":  euriborBaseURL + "/5/euribor-rate-1-week/",
	"1M":  euriborBaseURL + "/1/euribor-rate-1-month/",
	"3M":  euriborBaseURL + "/2/euribor-rate-3-months/",
	"6M":  euriborBaseURL + "/3/euribor-rate-6-months/",
	"12M": euriborBaseURL + "/4/euribor-rate-12-months/",
}

// EuriborData holds the scraped rate and publication date
type EuriborData struct {
	Rate            float64
	PublicationDate time.Time
}

// Scraper handles fetching Euribor rates from euribor-rates.eu
type Scraper struct {
	client *http.Client
	log    *logrus.Logger
}

// New creates a new scraper instance
func New(log *logrus.Logger) *Scraper {
	return &Scraper{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log,
	}
}

// FetchRate scrapes Euribor rate from euribor-rates.eu
func (s *Scraper) FetchRate(maturity string) (*EuriborData, error) {
	url, exists := maturityURLs[maturity]
	if !exists {
		return nil, fmt.Errorf("invalid maturity: %s", maturity)
	}

	s.log.WithFields(logrus.Fields{
		"maturity": maturity,
		"url":      url,
	}).Debug("Fetching Euribor rate from web")

	// Fetch the page
	resp, err := s.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Extract rate and date from the HTML table
	data, err := s.extractData(doc, maturity)
	if err != nil {
		return nil, err
	}

	s.log.WithFields(logrus.Fields{
		"maturity": maturity,
		"rate":     data.Rate,
		"date":     data.PublicationDate.Format("2006-01-02"),
	}).Info("Successfully scraped Euribor rate")

	return data, nil
}

// extractData parses the HTML document and extracts rate and date
func (s *Scraper) extractData(doc *goquery.Document, maturity string) (*EuriborData, error) {
	// The structure is typically:
	// <table class="table_historiek">
	//   <tbody>
	//     <tr>
	//       <td>12/13/2025</td>
	//       <td>2.524 %</td>
	//     </tr>
	//   </tbody>
	// </table>

	var data EuriborData
	var dateStr, rateStr string
	var found bool

	// Strategy 1: Look for table with class "table_historiek"
	doc.Find("table.table_historiek tbody tr").First().Each(func(i int, s *goquery.Selection) {
		cells := s.Find("td")
		if cells.Length() >= 2 {
			dateStr = strings.TrimSpace(cells.Eq(0).Text())
			rateStr = strings.TrimSpace(cells.Eq(1).Text())
			found = true
		}
	})

	// Strategy 2: If not found, try any table in main content
	if !found {
		doc.Find("table tr").First().Each(func(i int, s *goquery.Selection) {
			cells := s.Find("td")
			if cells.Length() >= 2 {
				dateStr = strings.TrimSpace(cells.Eq(0).Text())
				rateStr = strings.TrimSpace(cells.Eq(1).Text())
				found = true
			}
		})
	}

	if !found {
		return nil, fmt.Errorf("could not find rate data in HTML")
	}

	// Parse rate
	rate, err := parseRate(rateStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse rate '%s': %w", rateStr, err)
	}
	data.Rate = rate

	// Parse date
	pubDate, err := parseDate(dateStr)
	if err != nil {
		s.log.WithFields(logrus.Fields{
			"maturity": maturity,
			"date_str": dateStr,
			"error":    err,
		}).Warn("Failed to parse publication date, using current time")
		pubDate = time.Now()
	}
	data.PublicationDate = pubDate

	return &data, nil
}

// parseRate extracts the numeric rate from strings like "2.524 %", "2,524%", etc.
func parseRate(s string) (float64, error) {
	// Remove percentage sign and whitespace
	s = strings.TrimSpace(s)
	s = strings.Replace(s, "%", "", -1)
	s = strings.TrimSpace(s)

	// Replace comma with dot (European format)
	s = strings.Replace(s, ",", ".", -1)

	// Remove any non-numeric characters except dot and minus
	// This handles cases like "2.524 " or "-0.123"
	var cleaned strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' {
			cleaned.WriteRune(r)
		}
	}

	cleanedStr := cleaned.String()
	if cleanedStr == "" {
		return 0, fmt.Errorf("no numeric data found in '%s'", s)
	}

	// Parse the float
	rate, err := strconv.ParseFloat(cleanedStr, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse '%s' as float: %w", cleanedStr, err)
	}

	return rate, nil
}

// parseDate tries multiple date formats commonly used in EU
func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)

	// Try multiple common date formats
	formats := []string{
		"01/02/2006",      // MM/DD/YYYY (US format)
		"02/01/2006",      // DD/MM/YYYY (EU format)
		"2006-01-02",      // YYYY-MM-DD (ISO)
		"02-01-2006",      // DD-MM-YYYY
		"01-02-2006",      // MM-DD-YYYY
		"2.1.2006",        // D.M.YYYY (short EU)
		"02.01.2006",      // DD.MM.YYYY (EU with dots)
		"Jan 2, 2006",     // Month name format
		"2 Jan 2006",      // EU month name format
		"January 2, 2006", // Full month name
		"2 January 2006",  // EU full month name
	}

	var lastErr error
	for _, format := range formats {
		t, err := time.Parse(format, s)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}

	// If all formats fail, return the last error
	return time.Time{}, fmt.Errorf("could not parse date '%s': %w", s, lastErr)
}

// GetSupportedMaturities returns list of supported maturities
func GetSupportedMaturities() []string {
	maturities := make([]string, 0, len(maturityURLs))
	for m := range maturityURLs {
		maturities = append(maturities, m)
	}
	return maturities
}
