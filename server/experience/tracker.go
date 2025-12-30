package experience

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// Tracker handles telemetry and analytics
type Tracker struct {
	enabled       bool
	measurementID string // GA4 Measurement ID
	apiSecret     string // GA4 API Secret
	clientID      string
	sessionID     string
	httpClient    *http.Client
	eventQueue    chan *Event
	done          chan struct{}
	wg            sync.WaitGroup
}

// Event represents a telemetry event
type Event struct {
	Name       string                 `json:"name"`
	Params     map[string]interface{} `json:"params"`
	Timestamp  time.Time              `json:"timestamp"`
}

// GA4 payload structure
type ga4Payload struct {
	ClientID string      `json:"client_id"`
	Events   []ga4Event  `json:"events"`
}

type ga4Event struct {
	Name   string                 `json:"name"`
	Params map[string]interface{} `json:"params"`
}

// NewTracker creates a new experience tracker
func NewTracker() *Tracker {
	t := &Tracker{
		enabled:       os.Getenv("TELEMETRY_ENABLED") == "true",
		measurementID: os.Getenv("GA4_MEASUREMENT_ID"),
		apiSecret:     os.Getenv("GA4_API_SECRET"),
		clientID:      generateClientID(),
		sessionID:     generateSessionID(),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		eventQueue: make(chan *Event, 100),
		done:       make(chan struct{}),
	}

	if t.enabled && t.measurementID != "" && t.apiSecret != "" {
		t.wg.Add(1)
		go t.processQueue()
		log.Println("Experience tracking enabled")
	} else {
		log.Println("Experience tracking disabled")
	}

	return t
}

// Close shuts down the tracker
func (t *Tracker) Close() {
	close(t.done)
	t.wg.Wait()
}

// Track records an event
func (t *Tracker) Track(name string, params map[string]interface{}) {
	if !t.enabled {
		return
	}

	event := &Event{
		Name:      name,
		Params:    params,
		Timestamp: time.Now(),
	}

	select {
	case t.eventQueue <- event:
	default:
		// Queue full, drop event
	}
}

// TrackPageView tracks a page view
func (t *Tracker) TrackPageView(page, title string) {
	t.Track("page_view", map[string]interface{}{
		"page_location": page,
		"page_title":    title,
	})
}

// TrackTrade tracks a trade execution
func (t *Tracker) TrackTrade(traderID, symbol, action string, quantity, pnl float64) {
	t.Track("trade_executed", map[string]interface{}{
		"trader_id": traderID,
		"symbol":    symbol,
		"action":    action,
		"quantity":  quantity,
		"pnl":       pnl,
	})
}

// TrackAIDecision tracks an AI decision
func (t *Tracker) TrackAIDecision(traderID, provider, model string, durationMs int64, success bool) {
	t.Track("ai_decision", map[string]interface{}{
		"trader_id":   traderID,
		"provider":    provider,
		"model":       model,
		"duration_ms": durationMs,
		"success":     success,
	})
}

// TrackBacktest tracks a backtest run
func (t *Tracker) TrackBacktest(userID, runID string, symbols []string, duration int64, finalPnL float64) {
	t.Track("backtest_run", map[string]interface{}{
		"user_id":     userID,
		"run_id":      runID,
		"symbols":     symbols,
		"duration_ms": duration,
		"final_pnl":   finalPnL,
	})
}

// TrackDebate tracks a debate session
func (t *Tracker) TrackDebate(sessionID string, participants int, rounds int, consensusReached bool) {
	t.Track("debate_session", map[string]interface{}{
		"session_id":        sessionID,
		"participants":      participants,
		"rounds":            rounds,
		"consensus_reached": consensusReached,
	})
}

// TrackError tracks an error
func (t *Tracker) TrackError(component, errorType, message string) {
	t.Track("error", map[string]interface{}{
		"component":  component,
		"error_type": errorType,
		"message":    message,
	})
}

// TrackStartup tracks application startup
func (t *Tracker) TrackStartup(version string) {
	t.Track("app_startup", map[string]interface{}{
		"version":    version,
		"session_id": t.sessionID,
	})
}

// TrackShutdown tracks application shutdown
func (t *Tracker) TrackShutdown(uptimeMinutes int64) {
	t.Track("app_shutdown", map[string]interface{}{
		"uptime_minutes": uptimeMinutes,
		"session_id":     t.sessionID,
	})
}

// processQueue processes the event queue
func (t *Tracker) processQueue() {
	defer t.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var batch []*Event

	for {
		select {
		case <-t.done:
			// Flush remaining events
			if len(batch) > 0 {
				t.sendBatch(batch)
			}
			return

		case event := <-t.eventQueue:
			batch = append(batch, event)
			if len(batch) >= 10 {
				t.sendBatch(batch)
				batch = nil
			}

		case <-ticker.C:
			if len(batch) > 0 {
				t.sendBatch(batch)
				batch = nil
			}
		}
	}
}

// sendBatch sends a batch of events to GA4
func (t *Tracker) sendBatch(events []*Event) {
	if len(events) == 0 {
		return
	}

	ga4Events := make([]ga4Event, len(events))
	for i, e := range events {
		params := make(map[string]interface{})
		for k, v := range e.Params {
			params[k] = v
		}
		params["session_id"] = t.sessionID
		params["engagement_time_msec"] = 100

		ga4Events[i] = ga4Event{
			Name:   e.Name,
			Params: params,
		}
	}

	payload := ga4Payload{
		ClientID: t.clientID,
		Events:   ga4Events,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal GA4 payload: %v", err)
		return
	}

	url := fmt.Sprintf("https://www.google-analytics.com/mp/collect?measurement_id=%s&api_secret=%s",
		t.measurementID, t.apiSecret)

	resp, err := t.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("Failed to send GA4 events: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Printf("GA4 returned status %d", resp.StatusCode)
	}
}

// generateClientID generates a unique client ID
func generateClientID() string {
	hostname, _ := os.Hostname()
	return fmt.Sprintf("%s_%d", hostname, time.Now().UnixNano())
}

// generateSessionID generates a unique session ID
func generateSessionID() string {
	return fmt.Sprintf("session_%d", time.Now().UnixNano())
}

// Global tracker instance
var defaultTracker *Tracker
var once sync.Once

// GetTracker returns the default tracker
func GetTracker() *Tracker {
	once.Do(func() {
		defaultTracker = NewTracker()
	})
	return defaultTracker
}

// Track is a convenience function to track events
func Track(name string, params map[string]interface{}) {
	GetTracker().Track(name, params)
}
