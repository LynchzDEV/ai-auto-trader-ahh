# AI Entry Timing Fix - Implementation Plan

## Problem Summary

The AI trading system has poor entry timing, causing losses despite correctly identifying market direction. The core issue: **LLMs excel at pattern recognition but fail at knowing WHEN to act**.

### Evidence
- 39.2% win rate despite "high confidence" signals
- SHORT trades consistently worse than LONG (AI following crowd into shorts at bottoms)
- Before filters: AI opened many positions → caught in reversals → losses
- After filters: Either blocks too much, or still loses when trades go through

---

## Root Causes Identified

### RC-1: LLMs Unsuited for Sequential Decision-Making (CRITICAL)
- **Source**: [Frontiers AI Research](https://www.frontiersin.org/journals/artificial-intelligence/articles/10.3389/frai.2025.1608365/full)
- **Issue**: AI receives snapshot data, has no memory of price trajectory or momentum decay
- **Location**: `decision/prompt_builder.go:336-433`

### RC-2: Distraction Effect from Prompt Overload (HIGH)
- **Source**: [PMC Research](https://pmc.ncbi.nlm.nih.gov/articles/PMC12421730/)
- **Issue**: 400+ lines of conflicting instructions overwhelm the AI
- **Location**: `ai/openrouter.go:294-406`, custom prompt in strategy JSON

### RC-3: No Signal + Trigger Separation (HIGH)
- **Source**: [Traders Mastermind](https://tradersmastermind.com/fix-your-trade-timing-signal-trigger/)
- **Issue**: AI decides IF, WHEN, and HOW to trade all at once
- **Location**: `trader/engine.go:1459-1498`

### RC-4: LLM Overconfidence & Herding Behavior (HIGH)
- **Source**: [arXiv LLM Trading Analysis](https://arxiv.org/html/2502.15800v1)
- **Issue**: AI confidently enters trades without considering if it's chasing the crowd

### RC-5: Missing Pullback Wait Logic (CRITICAL)
- **Source**: [HighStrike Pullback Trading](https://highstrike.com/pullback-trading/)
- **Issue**: No mechanism to wait for pullback after identifying signal
- **Impact**: Always enters at extended prices with worse risk/reward

### RC-6: No Market Regime Detection (MEDIUM)
- **Source**: [ATLAS Framework](https://arxiv.org/html/2510.15949v2)
- **Issue**: Same strategy used in trending, ranging, and volatile markets
- **Location**: `market/data.go:274-290`

### RC-7: Counter-Trend Filter is BACKWARDS (CRITICAL)
- **Issue**: Current filter PENALIZES entries when price is below EMA9, but professional pullback trading WANTS this
- **Location**: `trader/engine.go` in `checkEntrySafety`
- **Evidence**:
  ```go
  // Current logic (WRONG):
  if isLong && currentPrice < (ema9-emaTolerance) {
      addWarning("counter-trend", "price is below EMA9", 10, "medium")
  }
  ```
- **Impact**: Blocks good pullback entries, allows bad extended entries

---

## Existing Filters to REMOVE or CHANGE

### Filters to REMOVE

| Filter | Location | Current Behavior | Why Remove |
|--------|----------|------------------|------------|
| `counter-trend` | `engine.go:1750-1757` | Penalizes price below EMA9 for longs | **BACKWARDS** - blocks pullback entries which are the BEST entries |

### Filters to REDUCE/CHANGE

| Filter | Location | Current | Change To | Reason |
|--------|----------|---------|-----------|--------|
| `weak-trend` | `engine.go:1769-1771` | 0.3% EMA spread threshold | 0.15% | Too strict for volatile altcoins |
| `resistance-fomo` | `engine.go:1874-1878` | 15% penalty near 40-candle high | 10% or add context | In strong trends, new highs ARE valid entries |

### Filters to KEEP (Working Correctly)

| Filter | Location | Why Keep |
|--------|----------|----------|
| `trend-mismatch` | `engine.go:1774-1779` | Entering LONG when EMA9 < EMA21 is genuinely bad |
| `momentum-exhaustion` | `engine.go:1785-1793` | Extended price + declining MACD = real reversal risk |
| `liquidation` | `engine.go:1919-1929` | Truly dangerous, must stay as hard block |
| `oi-crowding` | `engine.go:1911-1916` | Herding indicator, valuable for avoiding crowd |
| `wick-rejection` | `engine.go:1832-1838` | Multiple rejection wicks signal reversal |

### Task 0.1: Fix Backwards Filters (IMMEDIATE) ✅ COMPLETED
- [x] **REMOVE** `counter-trend` check entirely
- [x] **REDUCE** `weak-trend` threshold from 0.3% to 0.15%
- [x] **ADD** `late-entry` check: penalize price EXTENDED >1.5% ABOVE EMA9 (the opposite of current)
- **File**: `trader/engine.go` in `checkEntrySafety`
- **Effort**: 30 minutes
- **Priority**: Do this FIRST before any other changes
- **Commit**: `365f293` - feat: Implement weighted scoring and fix backwards entry filters

```go
// REMOVE this (lines 1750-1757):
// if isLong && currentPrice < (ema9-emaTolerance) { ... }
// if !isLong && currentPrice > (ema9+emaTolerance) { ... }

// ADD this instead:
if isLong && data.EMA9 > 0 {
    extension := (data.CurrentPrice - data.EMA9) / data.EMA9 * 100
    if extension > 1.5 {
        addWarning("late-entry", fmt.Sprintf("Price extended %.1f%% ABOVE EMA9 - chasing the move", extension), 25, "critical")
    }
}
if !isLong && data.EMA9 > 0 {
    extension := (data.EMA9 - data.CurrentPrice) / data.EMA9 * 100
    if extension > 1.5 {
        addWarning("late-entry", fmt.Sprintf("Price extended %.1f%% BELOW EMA9 - chasing the dump", extension), 25, "critical")
    }
}
```

---

## Implementation Tasks

### Phase 1: Quick Wins (Low Effort, High Impact)

#### Task 1.1: Add Late Entry Detection
- [ ] Add `isLateEntry()` function to detect price extended >1.5% from EMA9
- [ ] Add heavy penalty (25%) in weighted scoring system for late entries
- **File**: `trader/engine.go` (in `checkEntrySafety`)
- **Effort**: 1 hour

```go
// Add after existing checks in checkEntrySafety
if data.EMA9 > 0 {
    extension := math.Abs((data.CurrentPrice - data.EMA9) / data.EMA9 * 100)
    if extension > 1.5 {
        addWarning("late-entry", fmt.Sprintf("Price extended %.1f%% from EMA9 - chasing", extension), 25, "critical")
    }
}
```

#### Task 1.2: Add Move Maturity Indicator ✅ COMPLETED
- [x] Calculate how many candles since EMA crossover (move start)
- [x] Classify as EARLY (1-5), MID (6-12), LATE (13-20), EXHAUSTED (>20)
- [x] Add to market data fed to AI
- **File**: `market/data.go`
- **Effort**: 2 hours
- **Commit**: `656881b` - feat: Add move maturity indicator to identify trend age

#### Task 1.3: Simplify AI Prompt for Entry Decisions ✅ COMPLETED
- [x] Create new minimal prompt focused ONLY on "Is this a good entry?"
- [x] Remove conflicting rules about position management, trailing stops, etc.
- [x] AI should output `wait_for` field describing ideal trigger
- **File**: `ai/openrouter.go`
- **Effort**: 2 hours
- **Commit**: `6fb09e9` - feat: Add entry-focused AI assessment with simplified prompt
- **New function**: `GetEntryAssessment()` returns `EntryAssessment` with direction, entry_quality, wait_for fields

---

### Phase 2: Two-Phase Entry System (Medium Effort, Critical Impact)

#### Task 2.1: Create Signal State Machine ✅ COMPLETED
- [x] Define new states: `WAITING_TRIGGER`, `TRIGGERED`, `EXPIRED`, `EXECUTED`, `CANCELED`
- [x] Store pending signals with expiry time (default 30 minutes)
- [x] Create TriggerCondition struct with types: pullback_to_ema9/21, rsi_below/above, etc.
- [x] Create SignalStore with thread-safe operations
- [x] Add CreateSignalFromAssessment() to bridge AI output
- **File**: New file `trader/signal_state.go`
- **Effort**: 4 hours
- **Commit**: `e6cc817` - feat: Add signal state machine for two-phase entry system

```go
type SignalState struct {
    Symbol          string
    Direction       string    // "LONG" or "SHORT"
    SignalTime      time.Time
    ExpiresAt       time.Time
    TriggerConditions TriggerConditions
    Status          string    // WAITING, TRIGGERED, EXPIRED
}

type TriggerConditions struct {
    WaitForPullbackToEMA  bool
    TargetRSI             float64   // e.g., RSI drops to 45
    RequireBullishCandle  bool
    RequireVolumeIncrease bool
}
```

#### Task 2.2: Implement Trigger Checker ✅ COMPLETED
- [x] Create function to check if trigger conditions are met
- [x] Run every tick/candle for pending signals
- [x] Execute trade when all trigger conditions satisfied
- **File**: `trader/trigger_checker.go`
- **Effort**: 4 hours
- **Commit**: `f6a6c84` - feat: Add trigger checker and signal integration for two-phase entry system

#### Task 2.3: Modify AI Output Format ✅ COMPLETED
- [x] Change AI to output `signal` + `trigger_conditions` instead of immediate action
- [x] Parse new format and create SignalState
- [x] Create SignalIntegration layer to bridge AI assessment to signal/trigger system
- **Files**: `ai/openrouter.go`, `trader/signal_integration.go`
- **Effort**: 3 hours
- **Commit**: `f6a6c84` - feat: Add trigger checker and signal integration for two-phase entry system

---

### Phase 3: Enhanced Market Data (Medium Effort, High Impact)

#### Task 3.1: Add Entry Timing Indicators
- [ ] `DistanceFromEMA9Pct` - How far price is from EMA9
- [ ] `RSI_RateOfChange` - Is RSI rising or falling?
- [ ] `CandlesSinceExtreme` - Candles since RSI > 70 or < 30
- [ ] `PullbackDepthPct` - Current pullback depth from high/low
- [ ] `MomentumDecay` - Is MACD histogram declining while price rises?
- **File**: `market/data.go` (MarketData struct + calculations)
- **Effort**: 4 hours

#### Task 3.2: Add Fibonacci Retracement Detection
- [ ] Calculate recent swing high/low
- [ ] Determine if price is at 38.2%, 50%, or 61.8% retracement
- [ ] Add to market data and AI prompt
- **File**: `market/data.go`
- **Effort**: 3 hours

#### Task 3.3: Add Volume Profile on Pullback
- [ ] Detect if volume is decreasing during pullback (healthy)
- [ ] Or increasing during pullback (unhealthy, may continue)
- **File**: `market/data.go`
- **Effort**: 2 hours

---

### Phase 4: Market Regime Detection (Medium Effort, High Impact)

#### Task 4.1: Implement Regime Detection
- [ ] Calculate ADX for trend strength
- [ ] Detect Bollinger squeeze for ranging
- [ ] Detect ATR spikes for volatility
- [ ] Classify: TRENDING, RANGING, VOLATILE, EXHAUSTED
- **File**: New file `market/regime.go`
- **Effort**: 4 hours

```go
type MarketRegime string

const (
    RegimeTrending  MarketRegime = "TRENDING"   // ADX > 25
    RegimeRanging   MarketRegime = "RANGING"    // ADX < 20, Bollinger squeeze
    RegimeVolatile  MarketRegime = "VOLATILE"   // ATR > 2x average
    RegimeExhausted MarketRegime = "EXHAUSTED"  // Extended + declining momentum
)
```

#### Task 4.2: Add Regime-Based Rules
- [ ] In EXHAUSTED regime: Only allow pullback entries
- [ ] In VOLATILE regime: Reduce position size or skip
- [ ] In RANGING regime: Use mean reversion instead of momentum
- **File**: `trader/engine.go`
- **Effort**: 3 hours

#### Task 4.3: Feed Regime to AI with Context
- [ ] Add regime to prompt with specific rules for each
- [ ] Make regime a hard constraint in some cases
- **File**: `decision/prompt_builder.go`
- **Effort**: 2 hours

---

### Phase 5: Layered Decision System (High Effort, Critical Impact)

#### Task 5.1: Design Decision Pipeline
```
Layer 1: Market Regime (Code)
    → Pass/Block + constraints
Layer 2: Entry Timing (Code)
    → "Wait for pullback" or "Ready"
Layer 3: AI Signal Check
    → Confirm opportunity exists
Layer 4: Trigger Conditions (Code)
    → Final execution gate
```
- **Effort**: Design 2 hours

#### Task 5.2: Implement Pipeline Executor
- [ ] Create pipeline that runs layers in sequence
- [ ] Each layer can block, pass, or add constraints
- [ ] AI becomes ADVISOR, code has final say
- **File**: New file `trader/decision_pipeline.go`
- **Effort**: 6 hours

#### Task 5.3: Integrate with Existing Engine
- [ ] Replace current single-step AI decision with pipeline
- [ ] Maintain backward compatibility with config flag
- **File**: `trader/engine.go`
- **Effort**: 4 hours

---

## Priority Order

| Priority | Task | Effort | Impact | Dependencies |
|----------|------|--------|--------|--------------|
| **0** | **Task 0.1: Fix Backwards Filters** | **30m** | **CRITICAL** | **None** |
| 1 | Task 1.1: Late Entry Detection | 1h | High | 0.1 (merged) |
| 2 | Task 1.2: Move Maturity Indicator | 2h | Medium | None |
| 3 | Task 1.3: Simplify AI Prompt | 2h | High | None |
| 4 | Task 2.1: Signal State Machine | 4h | Critical | None |
| 5 | Task 2.2: Trigger Checker | 4h | Critical | 2.1 |
| 6 | Task 2.3: Modify AI Output | 3h | Critical | 2.1 |
| 7 | Task 3.1: Entry Timing Indicators | 4h | High | None |
| 8 | Task 4.1: Regime Detection | 4h | High | None |
| 9 | Task 3.2: Fibonacci Detection | 3h | Medium | 3.1 |
| 10 | Task 4.2: Regime-Based Rules | 3h | High | 4.1 |

> **START HERE**: Task 0.1 fixes a backwards filter that is actively hurting performance. Do this first.

---

## Success Metrics

After implementation, track:

1. **Win Rate**: Target > 50% (from current 39.2%)
2. **Late Entry Rate**: % of trades entering >1.5% from EMA9 (target < 10%)
3. **Pullback Entry Rate**: % of trades entering on pullback (target > 60%)
4. **Average Entry Distance from EMA9**: Target < 0.8%
5. **Regime-Appropriate Trades**: % matching regime strategy (target > 80%)

---

## References & Resources

### LLM Trading Limitations (Why AI Makes Bad Timing Decisions)

| Resource | Key Insight | Relevance |
|----------|-------------|-----------|
| [Frontiers AI - LLM in Equity Markets](https://www.frontiersin.org/journals/artificial-intelligence/articles/10.3389/frai.2025.1608365/full) | "LLMs are primarily trained for static text generation, making them ill-suited for sequential decision-making in trading" | RC-1: Core limitation |
| [PMC - LLM Applications in Finance](https://pmc.ncbi.nlm.nih.gov/articles/PMC12421730/) | "Distraction Effect—where extraneous company information skews the sentiment assessment" | RC-2: Prompt overload |
| [arXiv - LLM Agent Behavior Analysis](https://arxiv.org/html/2502.15800v1) | "LLM-based agents display less trading strategy variance... overconfidence and herding behavior" | RC-4: Overconfidence |
| [arXiv - Can LLMs Trade?](https://arxiv.org/pdf/2504.10789) | Testing financial theories with LLM agents in market simulations | General LLM trading research |
| [arXiv - LLM Limitations Survey](https://arxiv.org/html/2505.19240v1) | "Reasoning remains the most studied limitation, followed by generalization, hallucination, bias" | Understanding LLM failure modes |
| [OpenReview - Algorithmic Trading with LLMs](https://openreview.net/pdf?id=w7BGq6ozOL) | RL approach for stock market optimization | Alternative approaches |

### Professional Entry Timing Strategies

| Resource | Key Insight | Relevance |
|----------|-------------|-----------|
| [HighStrike - Pullback Trading Guide 2025](https://highstrike.com/pullback-trading/) | "Buy that dip, not the high. Wait for RSI to fall to 40-45, bullish engulfing pattern with volume" | RC-5: Pullback logic |
| [Traders Mastermind - Signal + Trigger Method](https://tradersmastermind.com/fix-your-trade-timing-signal-trigger/) | "Separate trigger and signal bars: Filter > Signal > Trigger" | RC-3: Two-phase entry |
| [Capital.com - Pullback Strategy](https://capital.com/en-int/learn/trading-strategies/pullback-trading) | "Wait for a retracement, enter closer to support, set tighter stop losses" | Task 2.1-2.3 |
| [Altrady - Entry and Exit Points](https://www.altrady.com/blog/crypto-trading-strategies/pullback-trading-strategy-entry-exit-points) | "Combine support/resistance with Fibonacci 38.2%, 50%, 61.8% levels" | Task 3.2 |
| [HighStrike - Momentum Trading 2025](https://highstrike.com/momentum-trading/) | "Momentum works best in clear bull/bear phases. In choppy markets, cut position size or wait" | RC-6: Regime detection |
| [Kraken - Futures Trading Strategies](https://www.kraken.com/learn/futures-trading-strategies) | 11 strategies including pullback and momentum | General reference |
| [Mudrex - Professional Crypto Indicators](https://mudrex.com/learn/professional-crypto-futures-trading-indicators/) | 9 proven indicators for futures trading | Task 3.1 |

### Fixing Algorithmic Entry Timing Problems

| Resource | Key Insight | Relevance |
|----------|-------------|-----------|
| [Stock Forecast - Avoiding Late Entries](https://www.stockforecasttoday.com/post/swing-trading-etfs-with-cycle-timing-how-to-avoid-late-entries-near-market-tops) | "Cycle timing gives traders a map—a projection of where momentum is heading based on structure" | Task 1.2: Move maturity |
| [Trading Pedia - Missing Entries](https://www.tradingpedia.com/price-action-trading/missing-entries-entering-trades-late/) | "Late entries reduce margin for error. Any pullback can knock you out" | Task 0.1: Late entry |
| [Tradetron - Algorithm Optimization](https://tradetron.tech/blog/how-to-optimize-your-trading-algorithm-for-maximum-roi) | "Small improvements in entry/exit timing significantly impact ROI" | General optimization |
| [Medium - Psychology of Timing](https://medium.com/@fxmbrand/the-psychology-of-timing-why-retail-traders-trade-too-early-230aa01dcaf2) | "FOMO causes traders to rush in without confirmation" | Understanding bad entries |
| [NURP - Algorithmic Trading Mistakes](https://nurp.com/wisdom/common-algorithmic-trading-errors-and-solutions/) | 5 common mistakes and how to fix them | General reference |

### Multi-Agent & Advanced Frameworks

| Resource | Key Insight | Relevance |
|----------|-------------|-----------|
| [TradingAgents Framework](https://tradingagents-ai.github.io/) | "Multi-agent framework with fundamental analysts, sentiment experts, technical analysts, risk managers" | Phase 5: Layered system |
| [GitHub - TradingAgents](https://github.com/TauricResearch/TradingAgents) | Open source multi-agent LLM trading framework | Implementation reference |
| [ATLAS Framework](https://arxiv.org/html/2510.15949v2) | "Adaptive Trading with LLM AgentS Through Dynamic Prompt Optimization" | Task 1.3: Prompt optimization |
| [GitHub - FinMem LLM Trading](https://github.com/pipiku915/FinMem-LLM-StockTrading) | "Layered memory and character design for LLM trading agent" | Memory/context ideas |
| [FlowHunt - LLM Trading Bots Comparison](https://www.flowhunt.io/blog/llm-trading-bots-comparison/) | "Prioritize judging 'IF we should trade' before deciding 'HOW to trade'" | Core philosophy |
| [Springer - GPT-4 Financial Decision Making](https://link.springer.com/article/10.1007/s00521-024-10613-4) | "Can Large Language Models beat wall street?" | Academic validation |

### Technical Indicators & Implementation

| Resource | Key Insight | Relevance |
|----------|-------------|-----------|
| [Investopedia - ADX Indicator](https://www.investopedia.com/terms/a/adx.asp) | ADX > 25 = trending, < 20 = ranging | Task 4.1: Regime detection |
| [Investopedia - Fibonacci Retracement](https://www.investopedia.com/terms/f/fibonacciretracement.asp) | 38.2%, 50%, 61.8% levels for pullback entries | Task 3.2 |
| [Investopedia - RSI](https://www.investopedia.com/terms/r/rsi.asp) | Overbought/oversold and momentum | Task 3.1 |
| [TradingView - EMA Strategy](https://www.tradingview.com/wiki/Moving_Average) | EMA crossovers and pullback to EMA | Task 0.1: Entry timing |

### Code References (This Project)

| File | Purpose | Related Tasks |
|------|---------|---------------|
| `trader/engine.go:1755-1993` | `checkEntrySafety` - weighted scoring filters | Task 0.1 |
| `trader/engine.go:1460-1498` | Entry safety check caller (first) | Task 0.1 |
| `trader/engine.go:1686-1720` | Entry safety check caller (post-confirm) | Task 0.1 |
| `market/data.go:167-260` | `FormatForAI` - market data formatting | Task 3.1 |
| `market/data.go:14-45` | `MarketData` struct definition | Task 3.1 |
| `decision/prompt_builder.go:50-155` | System prompt (English) | Task 1.3 |
| `ai/openrouter.go:293-442` | `GetTradingDecision` with system prompt | Task 1.3 |

---

## Notes

- The weighted scoring system implemented earlier helps but doesn't solve the core timing issue
- The two-phase entry system (Signal → Trigger) is the most impactful change
- Start with Phase 1 quick wins to see immediate improvement
- Consider A/B testing new system against old before full rollout
