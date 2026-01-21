package exchange

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestCreateListenKey tests the listenKey creation endpoint
func TestCreateListenKey(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if r.Header.Get("X-MBX-APIKEY") == "" {
			t.Error("Missing API key header")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"listenKey": "test_listen_key_123",
		})
	}))
	defer server.Close()

	client := &BinanceClient{
		apiKey:     "test_api_key",
		secretKey:  "test_secret",
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()
	listenKey, err := client.CreateListenKey(ctx)

	if err != nil {
		t.Fatalf("CreateListenKey failed: %v", err)
	}

	if listenKey != "test_listen_key_123" {
		t.Errorf("Expected listenKey 'test_listen_key_123', got '%s'", listenKey)
	}
}

// TestRenewListenKey tests the listenKey renewal endpoint
func TestRenewListenKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("Expected PUT request, got %s", r.Method)
		}
		if r.Header.Get("X-MBX-APIKEY") == "" {
			t.Error("Missing API key header")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer server.Close()

	client := &BinanceClient{
		apiKey:     "test_api_key",
		secretKey:  "test_secret",
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()
	err := client.RenewListenKey(ctx)

	if err != nil {
		t.Fatalf("RenewListenKey failed: %v", err)
	}
}

// TestDeleteListenKey tests the listenKey deletion endpoint
func TestDeleteListenKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("Expected DELETE request, got %s", r.Method)
		}
		if r.Header.Get("X-MBX-APIKEY") == "" {
			t.Error("Missing API key header")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer server.Close()

	client := &BinanceClient{
		apiKey:     "test_api_key",
		secretKey:  "test_secret",
		baseURL:    server.URL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()
	err := client.DeleteListenKey(ctx)

	if err != nil {
		t.Fatalf("DeleteListenKey failed: %v", err)
	}
}

// TestWebSocketMessageParsing tests parsing of different WebSocket message types
func TestWebSocketMessageParsing(t *testing.T) {
	client := &BinanceClient{
		WsUpdateCh: make(chan *PositionUpdate, 10),
	}

	tests := []struct {
		name        string
		message     string
		expectError bool
		validate    func(*testing.T, *BinanceClient)
	}{
		{
			name: "ACCOUNT_UPDATE with position",
			message: `{"e":"ACCOUNT_UPDATE","E":1234567890,"a":{"P":[{"s":"BTCUSDT","ps":"BOTH","pa":"0.001","ep":"50000.0","mp":"51000.0","up":"100.0"}]}}`,
			expectError: false,
			validate: func(t *testing.T, c *BinanceClient) {
				select {
				case update := <-c.WsUpdateCh:
					if update.Symbol != "BTCUSDT" {
						t.Errorf("Expected symbol BTCUSDT, got %s", update.Symbol)
					}
					if update.PositionAmt != 0.001 {
						t.Errorf("Expected position amt 0.001, got %f", update.PositionAmt)
					}
					if update.MarkPrice != 51000.0 {
						t.Errorf("Expected mark price 51000.0, got %f", update.MarkPrice)
					}
					if update.UnrealizedPnL != 100.0 {
						t.Errorf("Expected unrealized PnL 100.0, got %f", update.UnrealizedPnL)
					}
				case <-time.After(1 * time.Second):
					t.Error("Timeout waiting for position update")
				}
			},
		},
		{
			name: "ORDER_TRADE_UPDATE",
			message: `{"e":"ORDER_TRADE_UPDATE","E":1234567890}`,
			expectError: false,
			validate: func(t *testing.T, c *BinanceClient) {
				// Should not produce position update
				select {
				case <-c.WsUpdateCh:
					t.Error("Unexpected position update for ORDER_TRADE_UPDATE")
				case <-time.After(100 * time.Millisecond):
					// Expected - no update
				}
			},
		},
		{
			name: "Invalid JSON",
			message: `{invalid json}`,
			expectError: true,
			validate: func(t *testing.T, c *BinanceClient) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.parseWebSocketMessage([]byte(tt.message))

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			tt.validate(t, client)
		})
	}
}

// TestWebSocketReconnection tests the reconnection logic
func TestWebSocketReconnection(t *testing.T) {
	t.Skip("Requires mock WebSocket server - integration test")
}

// TestConcurrentPositionUpdates tests that multiple concurrent updates don't cause race conditions
func TestConcurrentPositionUpdates(t *testing.T) {
	client := &BinanceClient{
		WsUpdateCh: make(chan *PositionUpdate, 100),
	}

	// Simulate concurrent position updates
	var wg sync.WaitGroup
	numUpdates := 100

	wg.Add(numUpdates)
	for i := 0; i < numUpdates; i++ {
		go func(idx int) {
			defer wg.Done()

			update := &PositionUpdate{
				Symbol:      "BTCUSDT",
				PositionAmt: float64(idx),
				MarkPrice:   50000.0,
			}

			select {
			case client.WsUpdateCh <- update:
				// Successfully sent
			case <-time.After(1 * time.Second):
				t.Error("Timeout sending update")
			}
		}(i)
	}

	// Wait for all goroutines to finish
	wg.Wait()

	// Verify all updates were received
	receivedCount := len(client.WsUpdateCh)
	if receivedCount != numUpdates {
		t.Errorf("Expected %d updates, got %d", numUpdates, receivedCount)
	}
}

// TestChannelBufferOverflow tests behavior when update channel buffer is full
func TestChannelBufferOverflow(t *testing.T) {
	// Small buffer to test overflow
	client := &BinanceClient{
		WsUpdateCh: make(chan *PositionUpdate, 5),
	}

	// Fill the buffer
	for i := 0; i < 5; i++ {
		client.WsUpdateCh <- &PositionUpdate{
			Symbol:      "BTCUSDT",
			PositionAmt: float64(i),
		}
	}

	// Try to parse a message when buffer is full
	message := `{"e":"ACCOUNT_UPDATE","E":1234567890,"a":{"P":[{"s":"ETHUSDT","ps":"BOTH","pa":"1.0","ep":"3000.0","mp":"3100.0","up":"100.0"}]}}`

	// Should not block or panic - message should be dropped with warning log
	err := client.parseWebSocketMessage([]byte(message))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Buffer should still have 5 items (original ones)
	if len(client.WsUpdateCh) != 5 {
		t.Errorf("Expected buffer size 5, got %d", len(client.WsUpdateCh))
	}
}

// TestWebSocketConnectionState tests the wsConnected atomic bool
func TestWebSocketConnectionState(t *testing.T) {
	client := &BinanceClient{}

	// Initially should be false
	if client.wsConnected.Load() {
		t.Error("Expected wsConnected to be false initially")
	}

	// Set to true
	client.wsConnected.Store(true)
	if !client.wsConnected.Load() {
		t.Error("Expected wsConnected to be true after Store(true)")
	}

	// Set back to false
	client.wsConnected.Store(false)
	if client.wsConnected.Load() {
		t.Error("Expected wsConnected to be false after Store(false)")
	}
}

// TestParseFloat tests the parseFloat helper function
func TestParseFloat(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected float64
	}{
		{"123.45", 123.45},
		{"0.001", 0.001},
		{float64(100.5), 100.5},
		{"invalid", 0},
		{nil, 0},
	}

	for _, tt := range tests {
		result := parseFloat(tt.input)
		if result != tt.expected {
			t.Errorf("parseFloat(%v) = %f, expected %f", tt.input, result, tt.expected)
		}
	}
}

// BenchmarkWebSocketMessageParsing benchmarks the message parsing performance
func BenchmarkWebSocketMessageParsing(b *testing.B) {
	client := &BinanceClient{
		WsUpdateCh: make(chan *PositionUpdate, 1000),
	}

	message := []byte(`{
		"e": "ACCOUNT_UPDATE",
		"E": 1234567890,
		"a": {
			"P": [{
				"s": "BTCUSDT",
				"ps": "BOTH",
				"pa": "0.001",
				"ep": "50000.0",
				"mp": "51000.0",
				"up": "100.0"
			}]
		}
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client.parseWebSocketMessage(message)
	}
}

// BenchmarkConcurrentUpdates benchmarks concurrent position updates
func BenchmarkConcurrentUpdates(b *testing.B) {
	client := &BinanceClient{
		WsUpdateCh: make(chan *PositionUpdate, 1000),
	}

	// Drain channel in background
	go func() {
		for range client.WsUpdateCh {
		}
	}()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			update := &PositionUpdate{
				Symbol:      "BTCUSDT",
				PositionAmt: 0.001,
				MarkPrice:   50000.0,
			}
			client.WsUpdateCh <- update
		}
	})
}
