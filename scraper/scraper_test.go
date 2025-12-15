package scraper

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func TestParseRate(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
		wantErr  bool
	}{
		{"2.524 %", 2.524, false},
		{"2,524%", 2.524, false},
		{"2.524", 2.524, false},
		{"2,524", 2.524, false},
		{" 2.524 % ", 2.524, false},
		{"3.141", 3.141, false},
		{"-0.123 %", -0.123, false},
		{"0.000 %", 0.000, false},
		{"invalid", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseRate(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRate(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("parseRate(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"12/13/2025", false},
		{"13/12/2025", false},
		{"2025-12-13", false},
		{"13-12-2025", false},
		{"13.12.2025", false},
		{"Dec 13, 2025", false},
		{"13 Dec 2025", false},
		{"invalid", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := parseDate(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDate(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestGetSupportedMaturities(t *testing.T) {
	maturities := GetSupportedMaturities()

	expectedCount := 5 // 1W, 1M, 3M, 6M, 12M
	if len(maturities) != expectedCount {
		t.Errorf("Expected %d maturities, got %d", expectedCount, len(maturities))
	}

	// Check that expected maturities are present
	expectedMaturities := map[string]bool{
		"1W":  true,
		"1M":  true,
		"3M":  true,
		"6M":  true,
		"12M": true,
	}

	for _, m := range maturities {
		if !expectedMaturities[m] {
			t.Errorf("Unexpected maturity: %s", m)
		}
	}
}

// TestFetchRate_Live is an integration test that actually hits the website
// Run with: go test -tags=integration
func TestFetchRate_Live(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	s := New(log)

	// Test fetching 3M rate
	data, err := s.FetchRate("3M")
	if err != nil {
		t.Fatalf("Failed to fetch 3M rate: %v", err)
	}

	if data.Rate <= 0 || data.Rate > 10 {
		t.Errorf("Rate %f seems unrealistic", data.Rate)
	}

	if data.PublicationDate.IsZero() {
		t.Error("Publication date is zero")
	}

	t.Logf("Successfully fetched: 3M = %f%% (published %s)",
		data.Rate, data.PublicationDate.Format("2006-01-02"))
}

// BenchmarkParseRate benchmarks the rate parsing function
func BenchmarkParseRate(b *testing.B) {
	input := "2.524 %"
	for i := 0; i < b.N; i++ {
		_, _ = parseRate(input)
	}
}

// BenchmarkParseDate benchmarks the date parsing function
func BenchmarkParseDate(b *testing.B) {
	input := "12/13/2025"
	for i := 0; i < b.N; i++ {
		_, _ = parseDate(input)
	}
}
