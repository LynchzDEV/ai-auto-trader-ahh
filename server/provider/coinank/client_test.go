package coinank

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVolumeRank(t *testing.T) {
	// Mock JSON response
	mockResp := `{
		"code": 0,
		"msg": "success",
		"data": [
			{"symbol": "LOWVOL", "turnover24h": 100, "openInterest": 500},
			{"symbol": "HIGHVOL", "turnover24h": 9000, "openInterest": 300},
			{"symbol": "MIDVOL", "turnover24h": 5000, "openInterest": 100}
		]
	}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockResp))
	}))
	defer ts.Close()

	client := NewClient("dummy-key")
	client.baseURL = ts.URL // Inject mock server URL

	// Test Volume Ranking (Should sort DESC by turnover24h)
	items, err := client.VolumeRank(context.Background(), 3)
	if err != nil {
		t.Fatalf("VolumeRank failed: %v", err)
	}

	if len(items) != 3 {
		t.Fatalf("Expected 3 items, got %d", len(items))
	}

	// Verify Order: HIGHVOL(9000) -> MIDVOL(5000) -> LOWVOL(100)
	expectedOrder := []string{"HIGHVOL", "MIDVOL", "LOWVOL"}
	for i, symbol := range expectedOrder {
		if items[i].Symbol != symbol {
			t.Errorf("Rank %d: expected %s, got %s (Vol: %.0f)", i, symbol, items[i].Symbol, items[i].Volume24H)
		}
	}
}

func TestPriceRank(t *testing.T) {
	// Mock JSON response (volatility/price change)
	mockResp := `{
		"code": 0,
		"msg": "success",
		"data": [
			{"symbol": "STABLE", "priceChangePercent": 0.1},
			{"symbol": "PUMP", "priceChangePercent": 15.0},
			{"symbol": "DUMP", "priceChangePercent": -20.0}
		]
	}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockResp))
	}))
	defer ts.Close()

	client := NewClient("dummy-key")
	client.baseURL = ts.URL

	// Test Price Rank (Should sort by ABSOLUTE change desc)
	// DUMP(-20) -> PUMP(15) -> STABLE(0.1)
	items, err := client.PriceRank(context.Background(), 3)
	if err != nil {
		t.Fatalf("PriceRank failed: %v", err)
	}

	expectedOrder := []string{"DUMP", "PUMP", "STABLE"}
	for i, symbol := range expectedOrder {
		if items[i].Symbol != symbol {
			t.Errorf("Rank %d: expected %s, got %s (Chg: %.1f)", i, symbol, items[i].Symbol, items[i].PriceChange24H)
		}
	}
}

func TestGetOIRanking_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate API response that indicates error code (not HTTP error)
		w.Write([]byte(`{"code": 500, "msg": "Internal Error", "data": null}`))
	}))
	defer ts.Close()

	client := NewClient("dummy-key")
	client.baseURL = ts.URL

	_, err := client.GetOIRanking(context.Background(), 10)
	if err == nil {
		t.Error("Expected error from failed API response, got nil")
	}
}
