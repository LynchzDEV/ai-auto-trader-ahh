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

	"auto-trader-ahh/decision"
	"auto-trader-ahh/mcp"
)

// MarketContextProvider is a function that provides fresh market context
type MarketContextProvider func(symbols []string) (*MarketContext, error)

// TradeExecutor is a function that executes trades based on debate decisions
type TradeExecutor func(decisions []*Decision) error

// Engine runs debate sessions
type Engine struct {
	sessions            map[string]*SessionWithDetails
	clients             map[string]mcp.AIClient // provider -> client
	eventChan           map[string]chan *Event  // sessionID -> event channel
	cancels             map[string]context.CancelFunc
	mu                  sync.RWMutex
	marketCtxProvider   MarketContextProvider
	tradeExecutor       TradeExecutor
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

// SetMarketContextProvider sets the function to get fresh market context
func (e *Engine) SetMarketContextProvider(provider MarketContextProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.marketCtxProvider = provider
}

// SetTradeExecutor sets the function to execute trades
func (e *Engine) SetTradeExecutor(executor TradeExecutor) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tradeExecutor = executor
}

// CreateSession creates a new debate session
func (e *Engine) CreateSession(req *CreateSessionRequest) (*SessionWithDetails, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	session := &SessionWithDetails{
		Session: Session{
			ID:                   fmt.Sprintf("debate_%d", time.Now().UnixNano()),
			Name:                 req.Name,
			Status:               StatusPending,
			Symbols:              req.Symbols,
			MaxRounds:            req.MaxRounds,
			IntervalMinutes:      req.IntervalMinutes,
			PromptVariant:        req.PromptVariant,
			AutoExecute:          req.AutoExecute,
			TraderID:             req.TraderID,
			Language:             req.Language,
			CreatedAt:            time.Now(),
			AutoCycle:            req.AutoCycle,
			CycleIntervalMinutes: req.CycleIntervalMinutes,
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
	if session.CycleIntervalMinutes <= 0 {
		session.CycleIntervalMinutes = 5 // Default 5 minutes between cycles
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

// ListSessions returns all sessions
func (e *Engine) ListSessions() []*SessionWithDetails {
	e.mu.RLock()
	defer e.mu.RUnlock()

	sessions := make([]*SessionWithDetails, 0, len(e.sessions))
	for _, s := range e.sessions {
		sessions = append(sessions, s)
	}
	return sessions
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

	if session.Status != StatusPending && session.Status != StatusCompleted {
		e.mu.Unlock()
		return fmt.Errorf("session already running")
	}

	session.Status = StatusRunning
	session.StartedAt = time.Now()

	ctx, cancel := context.WithCancel(ctx)
	e.cancels[sessionID] = cancel
	e.mu.Unlock()

	// Run debate in background
	go func() {
		if session.AutoCycle {
			e.runAutoCycle(ctx, session, marketCtx)
		} else {
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
		}
	}()

	return nil
}

// runAutoCycle runs the debate in continuous cycles
func (e *Engine) runAutoCycle(ctx context.Context, session *SessionWithDetails, initialMarketCtx *MarketContext) {
	log.Printf("[Debate] Starting auto-cycle for session %s (interval: %d minutes)", session.ID, session.CycleIntervalMinutes)

	marketCtx := initialMarketCtx
	cycleInterval := time.Duration(session.CycleIntervalMinutes) * time.Minute

	for {
		select {
		case <-ctx.Done():
			log.Printf("[Debate] Auto-cycle stopped for session %s", session.ID)
			return
		default:
		}

		// Get fresh market context if provider is available
		e.mu.RLock()
		provider := e.marketCtxProvider
		e.mu.RUnlock()

		if provider != nil {
			freshCtx, err := provider(session.Symbols)
			if err != nil {
				log.Printf("[Debate] Error getting market context: %v", err)
			} else {
				marketCtx = freshCtx
			}
		}

		// Reset session state for new cycle
		e.mu.Lock()
		session.Status = StatusRunning
		session.CurrentRound = 0
		session.Messages = make([]*Message, 0)
		session.Votes = make([]*Vote, 0)
		session.FinalDecisions = nil
		session.Error = ""
		e.mu.Unlock()

		// Send cycle start event
		session.CycleCount++
		e.sendEvent(session.ID, &Event{
			Type:      "cycle_start",
			SessionID: session.ID,
			Data: map[string]interface{}{
				"cycle_number": session.CycleCount,
				"symbols":      session.Symbols,
			},
			Timestamp: time.Now(),
		})

		log.Printf("[Debate] Starting cycle #%d for session %s", session.CycleCount, session.ID)

		// Run the debate
		if err := e.runDebate(ctx, session, marketCtx); err != nil {
			if ctx.Err() != nil {
				// Context cancelled, stop cycling
				return
			}
			log.Printf("[Debate] Cycle #%d error: %v", session.CycleCount, err)
			e.sendEvent(session.ID, &Event{
				Type:      "cycle_error",
				SessionID: session.ID,
				Data:      err.Error(),
				Timestamp: time.Now(),
			})
		} else {
			// Execute trades if enabled
			if session.AutoExecute && len(session.FinalDecisions) > 0 {
				e.executeDecisions(session)
			}
		}

		// Send cycle complete event
		e.sendEvent(session.ID, &Event{
			Type:      "cycle_complete",
			SessionID: session.ID,
			Data: map[string]interface{}{
				"cycle_number": session.CycleCount,
				"decisions":    session.FinalDecisions,
			},
			Timestamp: time.Now(),
		})

		// Calculate next cycle time
		e.mu.Lock()
		session.NextCycleAt = time.Now().Add(cycleInterval)
		session.Status = StatusCompleted // Mark as completed between cycles
		e.mu.Unlock()

		log.Printf("[Debate] Cycle #%d complete. Next cycle at %s", session.CycleCount, session.NextCycleAt.Format("15:04:05"))

		// Wait for next cycle
		select {
		case <-ctx.Done():
			log.Printf("[Debate] Auto-cycle stopped for session %s", session.ID)
			return
		case <-time.After(cycleInterval):
			// Continue to next cycle
		}
	}
}

// executeDecisions executes the final decisions from the debate
func (e *Engine) executeDecisions(session *SessionWithDetails) {
	e.mu.RLock()
	executor := e.tradeExecutor
	e.mu.RUnlock()

	if executor == nil {
		log.Printf("[Debate] No trade executor configured, skipping execution")
		return
	}

	log.Printf("[Debate] Executing %d decisions from cycle #%d", len(session.FinalDecisions), session.CycleCount)

	if err := executor(session.FinalDecisions); err != nil {
		log.Printf("[Debate] Trade execution error: %v", err)
		e.sendEvent(session.ID, &Event{
			Type:      "execution_error",
			SessionID: session.ID,
			Data:      err.Error(),
			Timestamp: time.Now(),
		})
	} else {
		// Mark decisions as executed
		for _, d := range session.FinalDecisions {
			d.Executed = true
			d.ExecutedAt = time.Now()
		}
		e.sendEvent(session.ID, &Event{
			Type:      "execution_complete",
			SessionID: session.ID,
			Data:      session.FinalDecisions,
			Timestamp: time.Now(),
		})
	}
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

// buildDebateSystemPrompt builds personality-enhanced system prompt (NOFX-style exact copy)
func (e *Engine) buildDebateSystemPrompt(basePrompt string, participant *Participant, round, maxRounds int) string {
	personality := GetPersonalityDescription(participant.Personality)
	emoji := PersonalityEmojis[participant.Personality]

	debateInstructions := fmt.Sprintf(`You are a professional quantitative trading AI assistant participating in a multi-AI market debate.

## DEBATE MODE - ROUND %d/%d

You are %s %s.

### Your Debate Role:
%s

### Debate Rules:
1. Analyze ALL candidate symbols provided in the market data
2. Support your arguments with specific data points and indicators
3. If this is round 2 or later, respond to other participants' arguments
4. Be persuasive but data-driven
5. Your personality should influence your analysis bias but not override data

## Decision Principles

### Risk First
- Margin usage must not exceed 30%%
- Must stop-loss when single position loss reaches -5%%
- Capital protection first, profit second

### Trailing Take-Profit
- Consider partial/full profit-taking when PnL pulls back 30%% from peak
- Example: Peak PnL +5%%, Current PnL +3.5%% → 30%% drawdown, should take profit

### Trend Following
- Only enter when trends align across multiple timeframes
- Use Open Interest (OI) changes to validate capital flow authenticity
- OI up + Price up = Strong bullish trend
- OI down + Price up = Shorts covering (potential reversal)

### Scale Operations
- Scale-in: First entry max 50%% of target position
- Scale-out: Close 33%% at +3%%, 50%% at +5%%, 100%% at +8%%
- Only add to winning positions, never average down losers

## Output Format (Strictly Follow)

**Must use XML tags <reasoning> and <decision> to separate chain of thought and decision JSON**

<reasoning>
Your chain of thought analysis...
- Analyze market conditions
- Evaluate technical indicators
- Consider risk factors
</reasoning>

<decision>
`+"```json"+`
[
  {
    "symbol": "BTCUSDT",
    "action": "open_long",
    "leverage": 5,
    "position_size_usd": 1000,
    "stop_loss": 42000,
    "take_profit": 48000,
    "confidence": 85,
    "reasoning": "Detailed reasoning explaining why this decision was made"
  }
]
`+"```"+`
</decision>

### Field Descriptions

- **symbol**: Trading pair (required)
- **action**: Action type (required)
  - open_long: Open long position
  - open_short: Open short position
  - close_long: Close long position
  - close_short: Close short position
  - hold: Hold current position
  - wait: Wait, take no action
- **leverage**: Leverage multiplier (required for new positions)
- **position_size_usd**: Position size in USDT (required for new positions)
- **stop_loss**: Stop-loss price (required for new positions)
- **take_profit**: Take-profit price (required for new positions)
- **confidence**: Confidence level (0-100, opening recommended ≥70)
- **reasoning**: Detailed reasoning (required, must explain decision basis)

**IMPORTANT**: All numeric values must be calculated numbers, NOT formulas/expressions

## Critical Reminders

1. **Never** confuse realized and unrealized P&L
2. **Always remember** leverage amplifies both gains and losses
3. **Always watch** Peak PnL - it's key for take-profit decisions
4. **Always combine** OI changes to validate trend authenticity
5. **Always follow** risk management rules - capital protection is priority #1

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

**Use the same format as before with <reasoning> and <decision> tags:**

<reasoning>
Your final analysis summarizing the key points from the debate...
</reasoning>

<decision>
` + "```json" + `
[
  {"symbol": "BTCUSDT", "action": "open_long", "confidence": 80, "leverage": 5, "position_size_usd": 1000, "stop_loss": 42000, "take_profit": 48000, "reasoning": "Final reasoning"}
]
` + "```" + `
</decision>
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

// Pre-compiled regex patterns for better performance (NOFX-style)
var (
	// Safe regex: precisely match ```json code blocks
	reJSONFence      = regexp.MustCompile(`(?is)` + "```json\\s*(\\[\\s*\\{.*?\\}\\s*\\])\\s*```")
	reJSONArray      = regexp.MustCompile(`(?is)\[\s*\{.*?\}\s*\]`)
	reArrayHead      = regexp.MustCompile(`^\[\s*\{`)
	reInvisibleRunes = regexp.MustCompile("[\u200B\u200C\u200D\uFEFF]")

	// XML tag extraction (supports any characters in reasoning chain)
	reReasoningTag = regexp.MustCompile(`(?s)<reasoning>(.*?)</reasoning>`)
	reDecisionTag  = regexp.MustCompile(`(?s)<decision>(.*?)</decision>`)
	reFinalVoteTag = regexp.MustCompile(`(?s)<final_vote>\s*(.*?)\s*</final_vote>`)
)

// fixMissingQuotes replaces curly/smart quotes and Chinese punctuation (NOFX-style)
func fixMissingQuotes(s string) string {
	// Smart quotes
	s = strings.ReplaceAll(s, "\u201c", "\"") // "
	s = strings.ReplaceAll(s, "\u201d", "\"") // "
	s = strings.ReplaceAll(s, "\u2018", "'")  // '
	s = strings.ReplaceAll(s, "\u2019", "'")  // '

	// Chinese punctuation
	s = strings.ReplaceAll(s, "［", "[")
	s = strings.ReplaceAll(s, "］", "]")
	s = strings.ReplaceAll(s, "｛", "{")
	s = strings.ReplaceAll(s, "｝", "}")
	s = strings.ReplaceAll(s, "：", ":")
	s = strings.ReplaceAll(s, "，", ",")

	// Chinese brackets
	s = strings.ReplaceAll(s, "【", "[")
	s = strings.ReplaceAll(s, "】", "]")
	s = strings.ReplaceAll(s, "〔", "[")
	s = strings.ReplaceAll(s, "〕", "]")
	s = strings.ReplaceAll(s, "、", ",")

	// Full-width space
	s = strings.ReplaceAll(s, "　", " ")

	return s
}

// removeInvisibleRunes removes invisible Unicode characters
func removeInvisibleRunes(s string) string {
	return reInvisibleRunes.ReplaceAllString(s, "")
}

// parseDecisions extracts decisions from AI response (NOFX-style robust parsing)
func parseDecisions(response string) ([]*Decision, int) {
	// Step 1: Clean up the response
	s := removeInvisibleRunes(response)
	s = strings.TrimSpace(s)
	s = fixMissingQuotes(s)

	var jsonPart string

	// Step 2: Try to extract from <decision> tag first (NOFX style)
	if match := reDecisionTag.FindStringSubmatch(s); match != nil && len(match) > 1 {
		jsonPart = strings.TrimSpace(match[1])
		log.Printf("✓ Extracted JSON using <decision> tag")
	} else if match := reFinalVoteTag.FindStringSubmatch(s); match != nil && len(match) > 1 {
		jsonPart = strings.TrimSpace(match[1])
		log.Printf("✓ Extracted JSON using <final_vote> tag")
	} else {
		jsonPart = s
		log.Printf("⚠️  <decision> tag not found, searching JSON in full text")
	}

	// Apply quote fixes again
	jsonPart = fixMissingQuotes(jsonPart)

	// Step 3: Try to extract from ```json code fence
	if m := reJSONFence.FindStringSubmatch(jsonPart); m != nil && len(m) > 1 {
		jsonContent := strings.TrimSpace(m[1])
		jsonContent = fixMissingQuotes(jsonContent)

		var rawDecisions []struct {
			Symbol          string  `json:"symbol"`
			Action          string  `json:"action"`
			Confidence      int     `json:"confidence"`
			Leverage        int     `json:"leverage"`
			PositionPct     float64 `json:"position_pct"`
			PositionSizeUSD float64 `json:"position_size_usd"`
			StopLoss        float64 `json:"stop_loss"`
			TakeProfit      float64 `json:"take_profit"`
			Reasoning       string  `json:"reasoning"`
		}

		if err := json.Unmarshal([]byte(jsonContent), &rawDecisions); err == nil && len(rawDecisions) > 0 {
			return convertRawDecisions(rawDecisions)
		}
		log.Printf("⚠️  JSON parse error from code fence: %v", jsonContent[:min(100, len(jsonContent))])
	}

	// Step 4: Try to find raw JSON array
	jsonContent := strings.TrimSpace(reJSONArray.FindString(jsonPart))
	if jsonContent == "" {
		// Fallback: Safe wait mode
		log.Printf("⚠️  [SafeFallback] AI didn't output JSON decision, entering safe wait mode")

		cotSummary := jsonPart
		if len(cotSummary) > 240 {
			cotSummary = cotSummary[:240] + "..."
		}

		return []*Decision{{
			Symbol:     "ALL",
			Action:     "wait",
			Confidence: 50,
			Reasoning:  fmt.Sprintf("Model didn't output structured JSON decision, entering safe wait; summary: %s", cotSummary),
		}}, 50
	}

	jsonContent = fixMissingQuotes(jsonContent)

	var rawDecisions []struct {
		Symbol          string  `json:"symbol"`
		Action          string  `json:"action"`
		Confidence      int     `json:"confidence"`
		Leverage        int     `json:"leverage"`
		PositionPct     float64 `json:"position_pct"`
		PositionSizeUSD float64 `json:"position_size_usd"`
		StopLoss        float64 `json:"stop_loss"`
		TakeProfit      float64 `json:"take_profit"`
		Reasoning       string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(jsonContent), &rawDecisions); err != nil {
		log.Printf("⚠️  JSON parse error: %v", err)
		log.Printf("JSON content: %s", jsonContent[:min(200, len(jsonContent))])

		return []*Decision{{
			Symbol:     "ALL",
			Action:     "wait",
			Confidence: 50,
			Reasoning:  fmt.Sprintf("JSON parsing failed: %v", err),
		}}, 50
	}

	return convertRawDecisions(rawDecisions)
}

// convertRawDecisions converts raw parsed decisions to Decision structs
func convertRawDecisions(rawDecisions []struct {
	Symbol          string  `json:"symbol"`
	Action          string  `json:"action"`
	Confidence      int     `json:"confidence"`
	Leverage        int     `json:"leverage"`
	PositionPct     float64 `json:"position_pct"`
	PositionSizeUSD float64 `json:"position_size_usd"`
	StopLoss        float64 `json:"stop_loss"`
	TakeProfit      float64 `json:"take_profit"`
	Reasoning       string  `json:"reasoning"`
}) ([]*Decision, int) {
	var decisions []*Decision
	totalConf := 0

	for _, rd := range rawDecisions {
		// Use position_size_usd if available, otherwise convert position_pct
		posPct := rd.PositionPct
		if posPct == 0 && rd.PositionSizeUSD > 0 {
			posPct = rd.PositionSizeUSD / 10000.0 // Assuming $10k account
		}
		if posPct == 0 {
			posPct = 0.2 // Default 20%
		}

		d := &Decision{
			Symbol:      rd.Symbol,
			Action:      rd.Action,
			Confidence:  rd.Confidence,
			Leverage:    rd.Leverage,
			PositionPct: posPct,
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

	log.Printf("✓ Parsed %d decisions, avg confidence: %d%%", len(decisions), avgConf)
	return decisions, avgConf
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// extractReasoning extracts reasoning from response (NOFX-style)
func extractReasoning(response string) string {
	// Try <reasoning> tag first
	if match := reReasoningTag.FindStringSubmatch(response); match != nil && len(match) > 1 {
		log.Printf("✓ Extracted reasoning chain using <reasoning> tag")
		return strings.TrimSpace(match[1])
	}

	// Try content before <decision> tag
	if decisionIdx := strings.Index(response, "<decision>"); decisionIdx > 0 {
		log.Printf("✓ Extracted content before <decision> tag as reasoning chain")
		return strings.TrimSpace(response[:decisionIdx])
	}

	// Fallback: content before JSON
	jsonStart := strings.Index(response, "[")
	if jsonStart > 0 {
		log.Printf("⚠️  Extracted reasoning chain using old format ([ character separator)")
		return strings.TrimSpace(response[:jsonStart])
	}

	if len(response) > 500 {
		return response[:500] + "..."
	}
	return strings.TrimSpace(response)
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
