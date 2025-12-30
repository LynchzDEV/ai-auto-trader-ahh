package debate

import (
	"time"

	"auto-trader/decision"
)

// Status represents debate session status
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusVoting    Status = "voting"
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
)

// Personality represents AI personality types
type Personality string

const (
	PersonalityBull        Personality = "bull"         // Optimistic, looks for long opportunities
	PersonalityBear        Personality = "bear"         // Skeptical, focuses on risks
	PersonalityAnalyst     Personality = "analyst"      // Neutral, data-driven
	PersonalityContrarian  Personality = "contrarian"   // Challenges majority opinion
	PersonalityRiskManager Personality = "risk_manager" // Focuses on position sizing
)

// PersonalityColors maps personalities to UI colors
var PersonalityColors = map[Personality]string{
	PersonalityBull:        "#22C55E", // Green
	PersonalityBear:        "#EF4444", // Red
	PersonalityAnalyst:     "#3B82F6", // Blue
	PersonalityContrarian:  "#F59E0B", // Amber
	PersonalityRiskManager: "#8B5CF6", // Purple
}

// PersonalityEmojis maps personalities to emojis
var PersonalityEmojis = map[Personality]string{
	PersonalityBull:        "🐂",
	PersonalityBear:        "🐻",
	PersonalityAnalyst:     "📊",
	PersonalityContrarian:  "🔄",
	PersonalityRiskManager: "🛡️",
}

// GetPersonalityDescription returns the description for a personality
func GetPersonalityDescription(p Personality) string {
	switch p {
	case PersonalityBull:
		return "Aggressive Bull - You are optimistic and look for long opportunities. You believe in upward momentum and trend continuation. Focus on bullish signals and support levels."
	case PersonalityBear:
		return "Cautious Bear - You are skeptical and focus on risks. You look for short opportunities and warning signs. Question bullish narratives and highlight resistance levels."
	case PersonalityAnalyst:
		return "Data Analyst - You are neutral and purely data-driven. Present technical analysis without bias. Let the indicators speak for themselves."
	case PersonalityContrarian:
		return "Contrarian - You challenge majority opinions and look for overlooked opportunities. Question consensus views and find alternative interpretations of the data."
	case PersonalityRiskManager:
		return "Risk Manager - You focus on position sizing, stop losses, and capital preservation. Evaluate risk/reward ratios and warn about potential downsides."
	default:
		return "Market Analyst - Provide balanced technical analysis."
	}
}

// Session represents a debate session
type Session struct {
	ID              string       `json:"id"`
	UserID          string       `json:"user_id"`
	Name            string       `json:"name"`
	Status          Status       `json:"status"`
	Symbols         []string     `json:"symbols"`
	MaxRounds       int          `json:"max_rounds"`
	CurrentRound    int          `json:"current_round"`
	IntervalMinutes int          `json:"interval_minutes"`
	PromptVariant   string       `json:"prompt_variant"`
	FinalDecisions  []*Decision  `json:"final_decisions"`
	AutoExecute     bool         `json:"auto_execute"`
	TraderID        string       `json:"trader_id"`
	Language        string       `json:"language"`
	CreatedAt       time.Time    `json:"created_at"`
	StartedAt       time.Time    `json:"started_at"`
	CompletedAt     time.Time    `json:"completed_at"`
	Error           string       `json:"error,omitempty"`
}

// Participant represents an AI participant in the debate
type Participant struct {
	ID          string      `json:"id"`
	SessionID   string      `json:"session_id"`
	AIModelID   string      `json:"ai_model_id"`
	AIModelName string      `json:"ai_model_name"`
	Provider    string      `json:"provider"`
	Personality Personality `json:"personality"`
	Color       string      `json:"color"`
	SpeakOrder  int         `json:"speak_order"`
	CreatedAt   time.Time   `json:"created_at"`
}

// Message represents a debate message from a participant
type Message struct {
	ID          string       `json:"id"`
	SessionID   string       `json:"session_id"`
	Round       int          `json:"round"`
	AIModelID   string       `json:"ai_model_id"`
	AIModelName string       `json:"ai_model_name"`
	Provider    string       `json:"provider"`
	Personality Personality  `json:"personality"`
	MessageType string       `json:"message_type"` // analysis, rebuttal, final, vote
	Content     string       `json:"content"`
	Decisions   []*Decision  `json:"decisions"`
	Confidence  int          `json:"confidence"`
	CreatedAt   time.Time    `json:"created_at"`
}

// Vote represents a final vote from a participant
type Vote struct {
	ID          string       `json:"id"`
	SessionID   string       `json:"session_id"`
	AIModelID   string       `json:"ai_model_id"`
	AIModelName string       `json:"ai_model_name"`
	Personality Personality  `json:"personality"`
	Decisions   []*Decision  `json:"decisions"`
	Reasoning   string       `json:"reasoning"`
	CreatedAt   time.Time    `json:"created_at"`
}

// Decision represents a trading decision from debate
type Decision struct {
	Symbol          string  `json:"symbol"`
	Action          string  `json:"action"` // open_long, open_short, close_long, close_short, hold, wait
	Confidence      int     `json:"confidence"`
	Leverage        int     `json:"leverage"`
	PositionPct     float64 `json:"position_pct"`
	PositionSizeUSD float64 `json:"position_size_usd"`
	StopLoss        float64 `json:"stop_loss"`
	TakeProfit      float64 `json:"take_profit"`
	Reasoning       string  `json:"reasoning"`
	Executed        bool    `json:"executed"`
	ExecutedAt      time.Time `json:"executed_at,omitempty"`
	OrderID         string  `json:"order_id,omitempty"`
	Error           string  `json:"error,omitempty"`
}

// SessionWithDetails includes participants and messages
type SessionWithDetails struct {
	Session
	Participants []*Participant `json:"participants"`
	Messages     []*Message     `json:"messages"`
	Votes        []*Vote        `json:"votes"`
}

// CreateSessionRequest is the request to create a debate session
type CreateSessionRequest struct {
	Name            string                      `json:"name"`
	Symbols         []string                    `json:"symbols"`
	MaxRounds       int                         `json:"max_rounds"`
	IntervalMinutes int                         `json:"interval_minutes"`
	PromptVariant   string                      `json:"prompt_variant"`
	AutoExecute     bool                        `json:"auto_execute"`
	TraderID        string                      `json:"trader_id"`
	Language        string                      `json:"language"`
	Participants    []CreateParticipantRequest  `json:"participants"`
}

// CreateParticipantRequest is the request to add a participant
type CreateParticipantRequest struct {
	AIModelID   string      `json:"ai_model_id"`
	AIModelName string      `json:"ai_model_name"`
	Provider    string      `json:"provider"`
	Personality Personality `json:"personality"`
}

// Event represents a real-time debate event
type Event struct {
	Type      string      `json:"type"` // round_start, message, round_end, vote, consensus, error
	SessionID string      `json:"session_id"`
	Round     int         `json:"round,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// MarketContext provides market data for debate
type MarketContext struct {
	CurrentTime string                    `json:"current_time"`
	Account     decision.AccountInfo      `json:"account"`
	Positions   []decision.PositionInfo   `json:"positions"`
	MarketData  map[string]*decision.MarketData `json:"market_data"`
}
