package debate

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"auto-trader/decision"
	"auto-trader/mcp"
)

// Engine runs debate sessions
type Engine struct {
	sessions   map[string]*SessionWithDetails
	clients    map[string]mcp.AIClient // provider -> client
	eventChan  map[string]chan *Event  // sessionID -> event channel
	cancels    map[string]context.CancelFunc
	mu         sync.RWMutex
}

// NewEngine creates a new debate engine
func NewEngine() *Engine {
	return &Engine{
		sessions:  make(map[string]*SessionWithDetails),
		clients:   make(map[string]mcp.AIClient),
		eventChan: make(map[string]chan *Event),
		cancels:   make(map[string]context.CancelFunc),
	}
}

// RegisterClient registers an AI client for a provider
func (e *Engine) RegisterClient(provider string, client mcp.AIClient) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.clients[provider] = client
}

// CreateSession creates a new debate session
func (e *Engine) CreateSession(req *CreateSessionRequest) (*SessionWithDetails, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	session := &SessionWithDetails{
		Session: Session{
			ID:              fmt.Sprintf("debate_%d", time.Now().UnixNano()),
			Name:            req.Name,
			Status:          StatusPending,
			Symbols:         req.Symbols,
			MaxRounds:       req.MaxRounds,
			IntervalMinutes: req.IntervalMinutes,
			PromptVariant:   req.PromptVariant,
			AutoExecute:     req.AutoExecute,
			TraderID:        req.TraderID,
			Language:        req.Language,
			CreatedAt:       time.Now(),
		},
		Participants: make([]*Participant, 0),
		Messages:     make([]*Message, 0),
		Votes:        make([]*Vote, 0),
	}

	if session.MaxRounds <= 0 {
		session.MaxRounds = 3
	}
	if session.Language == "" {
		session.Language = "en-US"
	}

	// Add participants
	for i, p := range req.Participants {
		participant := &Participant{
			ID:          fmt.Sprintf("participant_%d_%d", time.Now().UnixNano(), i),
			SessionID:   session.ID,
			AIModelID:   p.AIModelID,
			AIModelName: p.AIModelName,
			Provider:    p.Provider,
			Personality: p.Personality,
			Color:       PersonalityColors[p.Personality],
			SpeakOrder:  i + 1,
			CreatedAt:   time.Now(),
		}
		session.Participants = append(session.Participants, participant)
	}

	e.sessions[session.ID] = session
	e.eventChan[session.ID] = make(chan *Event, 100)

	return session, nil
}

// GetSession returns a session by ID
func (e *Engine) GetSession(id string) (*SessionWithDetails, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	session, exists := e.sessions[id]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	return session, nil
}

// GetEvents returns the event channel for a session
func (e *Engine) GetEvents(sessionID string) (<-chan *Event, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ch, exists := e.eventChan[sessionID]
	if !exists {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return ch, nil
}

// Start begins a debate session
func (e *Engine) Start(ctx context.Context, sessionID string, marketCtx *MarketContext) error {
	e.mu.Lock()
	session, exists := e.sessions[sessionID]
	if !exists {
		e.mu.Unlock()
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if session.Status != StatusPending {
		e.mu.Unlock()
		return fmt.Errorf("session already started or completed")
	}

	session.Status = StatusRunning
	session.StartedAt = time.Now()

	ctx, cancel := context.WithCancel(ctx)
	e.cancels[sessionID] = cancel
	e.mu.Unlock()

	// Run debate in background
	go func() {
		if err := e.runDebate(ctx, session, marketCtx); err != nil {
			log.Printf("Debate error: %v", err)
			e.mu.Lock()
			session.Status = StatusCancelled
			session.Error = err.Error()
			e.mu.Unlock()
			e.sendEvent(sessionID, &Event{
				Type:      "error",
				SessionID: sessionID,
				Data:      err.Error(),
				Timestamp: time.Now(),
			})
		}
	}()

	return nil
}

// Stop cancels a running debate
func (e *Engine) Stop(sessionID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	cancel, exists := e.cancels[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	cancel()

	if session, ok := e.sessions[sessionID]; ok {
		session.Status = StatusCancelled
	}

	return nil
}

// runDebate executes the debate process
func (e *Engine) runDebate(ctx context.Context, session *SessionWithDetails, marketCtx *MarketContext) error {
	lang := decision.LangEnglish
	if session.Language == "zh-CN" {
		lang = decision.LangChinese
	}

	// Build base prompts
	promptBuilder := decision.NewPromptBuilder(lang)
	baseSystemPrompt := promptBuilder.BuildSystemPrompt()

	decisionCtx := &decision.Context{
		CurrentTime:     marketCtx.CurrentTime,
		Account:         marketCtx.Account,
		Positions:       marketCtx.Positions,
		MarketDataMap:   marketCtx.MarketData,
		BTCETHLeverage:  20,
		AltcoinLeverage: 10,
		BTCETHPosRatio:  0.3,
		AltcoinPosRatio: 0.15,
	}
	userPrompt := promptBuilder.BuildUserPrompt(decisionCtx)

	// Run debate rounds
	for round := 1; round <= session.MaxRounds; round++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		session.CurrentRound = round
		e.sendEvent(session.ID, &Event{
			Type:      "round_start",
			SessionID: session.ID,
			Round:     round,
			Timestamp: time.Now(),
		})

		// Get response from each participant
		for _, participant := range session.Participants {
			// Build personality-enhanced prompt
			systemPrompt := e.buildDebateSystemPrompt(baseSystemPrompt, participant, round, session.MaxRounds)
			debateUserPrompt := e.buildDebateUserPrompt(userPrompt, session.Messages, participant, round)

			// Get AI client
			client := e.clients[participant.Provider]
			if client == nil {
				// Try default client
				for _, c := range e.clients {
					client = c
					break
				}
			}
			if client == nil {
				return fmt.Errorf("no AI client available for %s", participant.Provider)
			}

			// Call AI
			response, err := client.CallWithMessages(systemPrompt, debateUserPrompt)
			if err != nil {
				log.Printf("AI call failed for %s: %v", participant.AIModelName, err)
				continue
			}

			// Parse decisions
			decisions, confidence := parseDecisions(response)

			// Create message
			msgType := "analysis"
			if round > 1 {
				msgType = "rebuttal"
			}

			msg := &Message{
				ID:          fmt.Sprintf("msg_%d", time.Now().UnixNano()),
				SessionID:   session.ID,
				Round:       round,
				AIModelID:   participant.AIModelID,
				AIModelName: participant.AIModelName,
				Provider:    participant.Provider,
				Personality: participant.Personality,
				MessageType: msgType,
				Content:     response,
				Decisions:   decisions,
				Confidence:  confidence,
				CreatedAt:   time.Now(),
			}

			e.mu.Lock()
			session.Messages = append(session.Messages, msg)
			e.mu.Unlock()

			e.sendEvent(session.ID, &Event{
				Type:      "message",
				SessionID: session.ID,
				Round:     round,
				Data:      msg,
				Timestamp: time.Now(),
			})

			// Wait between participants
			time.Sleep(time.Duration(session.IntervalMinutes) * time.Minute / time.Duration(len(session.Participants)))
		}

		e.sendEvent(session.ID, &Event{
			Type:      "round_end",
			SessionID: session.ID,
			Round:     round,
			Timestamp: time.Now(),
		})
	}

	// Voting phase
	session.Status = StatusVoting
	e.sendEvent(session.ID, &Event{
		Type:      "voting_start",
		SessionID: session.ID,
		Timestamp: time.Now(),
	})

	// Collect votes
	votes, err := e.collectVotes(ctx, session, baseSystemPrompt, userPrompt)
	if err != nil {
		return fmt.Errorf("voting failed: %w", err)
	}

	e.mu.Lock()
	session.Votes = votes
	e.mu.Unlock()

	// Determine consensus
	finalDecisions := e.determineConsensus(votes)

	e.mu.Lock()
	session.FinalDecisions = finalDecisions
	session.Status = StatusCompleted
	session.CompletedAt = time.Now()
	e.mu.Unlock()

	e.sendEvent(session.ID, &Event{
		Type:      "consensus",
		SessionID: session.ID,
		Data:      finalDecisions,
		Timestamp: time.Now(),
	})

	return nil
}

// buildDebateSystemPrompt builds personality-enhanced system prompt
func (e *Engine) buildDebateSystemPrompt(basePrompt string, participant *Participant, round, maxRounds int) string {
	personality := GetPersonalityDescription(participant.Personality)
	emoji := PersonalityEmojis[participant.Personality]

	debateInstructions := fmt.Sprintf(`
## DEBATE MODE - ROUND %d/%d

You are participating in a multi-AI market debate as %s %s.

### Your Debate Role:
%s

### Debate Rules:
1. Analyze ALL candidate symbols provided in the market data
2. Support your arguments with specific data points and indicators
3. If this is round 2 or later, respond to other participants' arguments
4. Be persuasive but data-driven
5. Your personality should influence your analysis bias but not override data

### Output Format

First write your analysis:
<reasoning>
- Your market analysis for each symbol with specific data references
- Your main trading thesis and arguments
- Response to other participants (if round > 1)
</reasoning>

Then output your decisions:
<decision>
[
  {"symbol": "BTCUSDT", "action": "open_long", "confidence": 75, "leverage": 5, "position_pct": 0.3, "stop_loss": 0.02, "take_profit": 0.04, "reasoning": "Brief explanation"}
]
</decision>

---

`, round, maxRounds, emoji, participant.Personality, personality)

	return debateInstructions + basePrompt
}

// buildDebateUserPrompt builds user prompt with previous messages
func (e *Engine) buildDebateUserPrompt(basePrompt string, messages []*Message, participant *Participant, round int) string {
	if round == 1 || len(messages) == 0 {
		return basePrompt
	}

	var sb strings.Builder
	sb.WriteString(basePrompt)
	sb.WriteString("\n\n---\n\n## Previous Round Messages\n\n")

	for _, msg := range messages {
		if msg.Round < round {
			emoji := PersonalityEmojis[msg.Personality]
			sb.WriteString(fmt.Sprintf("### %s %s (%s)\n", emoji, msg.AIModelName, msg.Personality))
			// Include summary, not full content
			if len(msg.Content) > 500 {
				sb.WriteString(msg.Content[:500] + "...\n\n")
			} else {
				sb.WriteString(msg.Content + "\n\n")
			}
		}
	}

	sb.WriteString("---\n\nNow provide your analysis and respond to the above arguments.\n")

	return sb.String()
}

// collectVotes collects final votes from all participants
func (e *Engine) collectVotes(ctx context.Context, session *SessionWithDetails, systemPrompt, userPrompt string) ([]*Vote, error) {
	var votes []*Vote

	votePrompt := `
## FINAL VOTE

The debate has concluded. Based on all the discussions, cast your final vote.

Provide your final trading decisions in this format:
<final_vote>
[
  {"symbol": "BTCUSDT", "action": "open_long", "confidence": 80, "leverage": 5, "position_pct": 0.25, "stop_loss": 0.02, "take_profit": 0.06, "reasoning": "Final reasoning"}
]
</final_vote>
`

	for _, participant := range session.Participants {
		client := e.clients[participant.Provider]
		if client == nil {
			for _, c := range e.clients {
				client = c
				break
			}
		}
		if client == nil {
			continue
		}

		// Build vote context with all messages
		fullPrompt := userPrompt + "\n\n## Debate Summary\n\n"
		for _, msg := range session.Messages {
			fullPrompt += fmt.Sprintf("**%s**: %s\n\n", msg.AIModelName, summarizeMessage(msg.Content))
		}
		fullPrompt += votePrompt

		response, err := client.CallWithMessages(systemPrompt, fullPrompt)
		if err != nil {
			log.Printf("Vote failed for %s: %v", participant.AIModelName, err)
			continue
		}

		decisions, _ := parseDecisions(response)

		vote := &Vote{
			ID:          fmt.Sprintf("vote_%d", time.Now().UnixNano()),
			SessionID:   session.ID,
			AIModelID:   participant.AIModelID,
			AIModelName: participant.AIModelName,
			Personality: participant.Personality,
			Decisions:   decisions,
			Reasoning:   extractReasoning(response),
			CreatedAt:   time.Now(),
		}

		votes = append(votes, vote)

		e.sendEvent(session.ID, &Event{
			Type:      "vote",
			SessionID: session.ID,
			Data:      vote,
			Timestamp: time.Now(),
		})
	}

	return votes, nil
}

// determineConsensus determines the final consensus from votes
func (e *Engine) determineConsensus(votes []*Vote) []*Decision {
	type actionData struct {
		score     float64
		totalConf int
		totalLev  int
		totalPos  float64
		totalSL   float64
		totalTP   float64
		count     int
		reasons   []string
	}

	symbolActions := make(map[string]map[string]*actionData)

	// Aggregate votes
	for _, vote := range votes {
		for _, d := range vote.Decisions {
			if symbolActions[d.Symbol] == nil {
				symbolActions[d.Symbol] = make(map[string]*actionData)
			}
			if symbolActions[d.Symbol][d.Action] == nil {
				symbolActions[d.Symbol][d.Action] = &actionData{}
			}

			ad := symbolActions[d.Symbol][d.Action]
			weight := float64(d.Confidence) / 100.0
			if weight < 0.5 {
				weight = 0.5
			}

			ad.score += weight
			ad.totalConf += d.Confidence
			ad.totalLev += d.Leverage
			ad.totalPos += d.PositionPct
			ad.totalSL += d.StopLoss
			ad.totalTP += d.TakeProfit
			ad.count++
			if d.Reasoning != "" {
				ad.reasons = append(ad.reasons, d.Reasoning)
			}
		}
	}

	// Determine winning action per symbol
	var results []*Decision
	for symbol, actions := range symbolActions {
		var winningAction string
		var maxScore float64
		var winningData *actionData

		for action, ad := range actions {
			if ad.score > maxScore {
				maxScore = ad.score
				winningAction = action
				winningData = ad
			}
		}

		if winningData == nil || winningData.count == 0 {
			continue
		}

		// Calculate averages
		avgConf := winningData.totalConf / winningData.count
		avgLev := winningData.totalLev / winningData.count
		avgPos := winningData.totalPos / float64(winningData.count)
		avgSL := winningData.totalSL / float64(winningData.count)
		avgTP := winningData.totalTP / float64(winningData.count)

		// Apply defaults
		if avgLev <= 0 {
			avgLev = 5
		}
		if avgPos <= 0 {
			avgPos = 0.2
		}

		decision := &Decision{
			Symbol:      symbol,
			Action:      winningAction,
			Confidence:  avgConf,
			Leverage:    avgLev,
			PositionPct: avgPos,
			StopLoss:    avgSL,
			TakeProfit:  avgTP,
			Reasoning:   strings.Join(winningData.reasons, "; "),
		}

		results = append(results, decision)
	}

	return results
}

// sendEvent sends an event to subscribers
func (e *Engine) sendEvent(sessionID string, event *Event) {
	e.mu.RLock()
	ch, exists := e.eventChan[sessionID]
	e.mu.RUnlock()

	if exists {
		select {
		case ch <- event:
		default:
			// Channel full, skip
		}
	}
}

// parseDecisions extracts decisions from AI response
func parseDecisions(response string) ([]*Decision, int) {
	decisionPattern := regexp.MustCompile(`(?s)<decision>\s*(.*?)\s*</decision>`)
	finalVotePattern := regexp.MustCompile(`(?s)<final_vote>\s*(.*?)\s*</final_vote>`)

	var jsonContent string
	if matches := decisionPattern.FindStringSubmatch(response); len(matches) > 1 {
		jsonContent = strings.TrimSpace(matches[1])
	} else if matches := finalVotePattern.FindStringSubmatch(response); len(matches) > 1 {
		jsonContent = strings.TrimSpace(matches[1])
	}

	if jsonContent != "" {
		// Try JSON array
		var rawDecisions []struct {
			Symbol      string  `json:"symbol"`
			Action      string  `json:"action"`
			Confidence  int     `json:"confidence"`
			Leverage    int     `json:"leverage"`
			PositionPct float64 `json:"position_pct"`
			StopLoss    float64 `json:"stop_loss"`
			TakeProfit  float64 `json:"take_profit"`
			Reasoning   string  `json:"reasoning"`
		}

		if err := json.Unmarshal([]byte(jsonContent), &rawDecisions); err == nil && len(rawDecisions) > 0 {
			var decisions []*Decision
			totalConf := 0
			for _, rd := range rawDecisions {
				d := &Decision{
					Symbol:      rd.Symbol,
					Action:      rd.Action,
					Confidence:  rd.Confidence,
					Leverage:    rd.Leverage,
					PositionPct: rd.PositionPct,
					StopLoss:    rd.StopLoss,
					TakeProfit:  rd.TakeProfit,
					Reasoning:   rd.Reasoning,
				}
				decisions = append(decisions, d)
				totalConf += rd.Confidence
			}
			avgConf := 50
			if len(decisions) > 0 {
				avgConf = totalConf / len(decisions)
			}
			return decisions, avgConf
		}
	}

	// Fallback
	return []*Decision{{
		Symbol:     "ALL",
		Action:     "wait",
		Confidence: 50,
		Reasoning:  "Failed to parse decisions",
	}}, 50
}

// extractReasoning extracts reasoning from response
func extractReasoning(response string) string {
	reasoningPattern := regexp.MustCompile(`(?s)<reasoning>\s*(.*?)\s*</reasoning>`)
	if matches := reasoningPattern.FindStringSubmatch(response); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	if len(response) > 500 {
		return response[:500] + "..."
	}
	return response
}

// summarizeMessage creates a brief summary of a message
func summarizeMessage(content string) string {
	// Extract reasoning if available
	reasoning := extractReasoning(content)
	if reasoning != "" && reasoning != content {
		if len(reasoning) > 200 {
			return reasoning[:200] + "..."
		}
		return reasoning
	}
	if len(content) > 200 {
		return content[:200] + "..."
	}
	return content
}
