# Recommended Settings Guide

This guide provides recommended configurations for different account sizes and risk tolerances. The system now includes advanced risk management features that should be properly configured.

---

## Option 1: CONSERVATIVE (Recommended for Small Accounts)

**Goal**: Grow account steadily, minimize risk of blowing up

**Best for**: Accounts under $500, beginners, or after a losing streak

### Risk Control Settings
| Setting | Value | Why |
|---------|-------|-----|
| **Max Positions** | 2 | Limits total exposure |
| **BTC/ETH Leverage** | 10x | Lower leverage = safer |
| **Altcoin Leverage** | 10x | Match BTC/ETH for safety |
| **Min Confidence** | 80% | Only high-conviction trades |
| **Min R/R Ratio** | 3.0 | Hard constraint, keep at 3.0 |
| **Daily Loss Limit** | 15% | Standard protection |
| **Noise Zone** | -1.5% to +1.5% | Default, prevents panic selling |

### Advanced Features
| Setting | Value | Why |
|---------|-------|-----|
| **Trailing Stop** | ON | Lock in profits |
| **Trailing Activate** | +1.5% | Start trailing at 1.5% profit |
| **Trailing Distance** | 0.5% | Trail 0.5% behind peak |
| **Max Hold Duration** | ON | Prevent capital lock-up |
| **Max Hold Time** | 240 min | 4 hours max per trade |
| **Smart Loss Cut** | ON | Cut losers early |
| **Loss Cut Time** | 30 min | After 30 minutes losing |
| **Loss Cut Threshold** | -1.0% | Only if loss > 1% |
| **Drawdown Protection** | ON | Protect gains |
| **Max Drawdown** | 40% | Close if 40% drop from peak |

### Trading Behavior
| Setting | Value | Why |
|---------|-------|-----|
| **Trading Interval** | 15 min | Fewer decisions = less churn |
| **Timeframe** | 5m | Standard timeframe |
| **Turbo Mode** | OFF | Prevents over-trading |

### Expected Results
- Risk per trade: **-5% to -8%** of wallet
- Win per trade: **+15% to +24%** of wallet
- Max risk with 2 positions: **-16%**
- Trades per day: 4-10

---

## Option 2: MODERATE (Balanced Growth)

**Goal**: Faster growth with acceptable risk

**Best for**: Accounts $500-$2000, some experience

### Risk Control Settings
| Setting | Value | Why |
|---------|-------|-----|
| **Max Positions** | 2 | Keep exposure manageable |
| **BTC/ETH Leverage** | 10x | Stay conservative on majors |
| **Altcoin Leverage** | 15x | Slightly more aggressive |
| **Min Confidence** | 75% | Accept more trades |
| **Min R/R Ratio** | 3.0 | Hard constraint |
| **Daily Loss Limit** | 15% | Standard protection |
| **Noise Zone** | -1.5% to +1.5% | Default |

### Advanced Features
| Setting | Value | Why |
|---------|-------|-----|
| **Trailing Stop** | ON | Lock in profits |
| **Trailing Activate** | +1.0% | Start earlier |
| **Trailing Distance** | 0.5% | Standard trail |
| **Max Hold Duration** | ON | Prevent lock-up |
| **Max Hold Time** | 180 min | 3 hours max |
| **Smart Loss Cut** | ON | Cut losers |
| **Loss Cut Time** | 30 min | Standard |
| **Loss Cut Threshold** | -1.5% | Slightly more tolerance |
| **Drawdown Protection** | ON | Protect gains |
| **Max Drawdown** | 35% | Tighter protection |

### Trading Behavior
| Setting | Value | Why |
|---------|-------|-----|
| **Trading Interval** | 10 min | More opportunities |
| **Timeframe** | 5m | Standard |
| **Turbo Mode** | OFF | Still avoid over-trading |

### Expected Results
- Risk per trade: **-8% to -12%** of wallet
- Win per trade: **+24% to +36%** of wallet
- Max risk with 2 positions: **-24%**
- Trades per day: 8-15

---

## Option 3: AGGRESSIVE (Experienced Traders Only)

**Goal**: Maximum growth, accept high risk

**Best for**: Accounts $2000+, experienced traders, active monitoring

### Risk Control Settings
| Setting | Value | Why |
|---------|-------|-----|
| **Max Positions** | 2 | Never go above 2! |
| **BTC/ETH Leverage** | 15x | Higher leverage |
| **Altcoin Leverage** | 20x | Maximum leverage |
| **Min Confidence** | 70% | Accept more trades |
| **Min R/R Ratio** | 3.0 | NEVER go below 3.0 |
| **Daily Loss Limit** | 20% | Slightly more tolerance |
| **Noise Zone** | -1.5% to +1.5% | Keep default |

### Advanced Features
| Setting | Value | Why |
|---------|-------|-----|
| **Trailing Stop** | ON | Essential at high leverage |
| **Trailing Activate** | +0.8% | Start early |
| **Trailing Distance** | 0.4% | Tighter trail |
| **Max Hold Duration** | ON | Critical |
| **Max Hold Time** | 120 min | 2 hours max |
| **Smart Loss Cut** | ON | Essential |
| **Loss Cut Time** | 20 min | Cut faster |
| **Loss Cut Threshold** | -1.0% | Lower tolerance |
| **Drawdown Protection** | ON | Essential |
| **Max Drawdown** | 30% | Tight protection |

### Trading Behavior
| Setting | Value | Why |
|---------|-------|-----|
| **Trading Interval** | 5 min | More frequent |
| **Timeframe** | 5m | Standard |
| **Turbo Mode** | Optional | Use with extreme caution |

### Expected Results
- Risk per trade: **-12% to -15%** of wallet
- Win per trade: **+36% to +45%** of wallet
- Max risk with 2 positions: **-30%**
- Trades per day: 15-25

### WARNING
- You can lose 30% in a bad session
- Requires active monitoring
- One mistake = significant loss
- Not recommended for small accounts

---

## Critical Settings to NEVER Change

These settings are hard constraints for a reason:

| Setting | Required Value | Why |
|---------|----------------|-----|
| **Min R/R Ratio** | 3.0 | Mathematical edge requirement |
| **Max Positions** | 2 (max) | 3+ = excessive exposure |
| **Emergency Shutdown** | ON | Account protection |
| **Emergency Min Balance** | $60+ | Prevents total wipeout |

---

## Feature Explanations

### Noise Zone Protection
**What**: Blocks closing positions when PnL is between -1.5% and +1.5%

**Why**: At high leverage, small price movements cause large PnL swings. Without this, the bot might panic-sell during normal market noise, only to see the price recover moments later.

**Exception**: Can override if AI confidence > 95%

### Trailing Stop Loss
**What**: Automatically moves stop-loss upward as price increases

**How**:
1. Position reaches activation threshold (e.g., +1% profit)
2. Trail starts at configured distance (e.g., 0.5%) below peak price
3. As price increases, stop-loss follows
4. If price drops to trailing stop, position closes with locked profit

**Example**: Entry at $100, peak at $103, trail at 0.5%
- Trail stop at $102.485
- If price drops to $102.485, close with +2.485% profit locked

### Smart Loss Cut
**What**: Force closes losing positions after a time threshold

**Why**: Positions that stay negative for extended periods rarely recover. Better to cut early and try again.

**How**:
1. Position is losing (PnL < 0)
2. Has been losing for configured minutes (e.g., 30 min)
3. Loss exceeds threshold (e.g., -1%)
4. Force close the position

### Max Hold Duration
**What**: Force closes any position held longer than configured time

**Why**: Prevents capital from being locked in stagnant trades. Markets that don't move in your direction within 4 hours are unlikely to suddenly move significantly.

### Drawdown Protection
**What**: Closes position if it drops significantly from its peak profit

**Why**: Protects gains. If you were up +10% and now only +6%, that's a 40% drawdown from peak. The system closes to preserve remaining profit.

### Emergency Shutdown
**What**: Halts all trading if account balance drops below threshold

**Why**: Last line of defense against total account wipeout. Even if everything else fails, this prevents complete loss.

---

## Comparison Table

| Setting | Conservative | Moderate | Aggressive |
|---------|--------------|----------|------------|
| **Max Positions** | 2 | 2 | 2 |
| **BTC/ETH Leverage** | 10x | 10x | 15x |
| **Altcoin Leverage** | 10x | 15x | 20x |
| **Min Confidence** | 80% | 75% | 70% |
| **Min R/R Ratio** | 3.0 | 3.0 | 3.0 |
| **Trading Interval** | 15 min | 10 min | 5 min |
| **Turbo Mode** | OFF | OFF | Optional |
| **Trailing Stop** | ON | ON | ON |
| **Max Hold** | 4 hours | 3 hours | 2 hours |
| **Daily Loss Limit** | 15% | 15% | 20% |
| **Max Drawdown** | 40% | 35% | 30% |
| **Risk/Trade** | -5% to -8% | -8% to -12% | -12% to -15% |
| **Reward/Trade** | +15% to +24% | +24% to +36% | +36% to +45% |
| **Max Drawdown (2 pos)** | -16% | -24% | -30% |
| **Trades/Day** | 4-10 | 8-15 | 15-25 |

---

## Getting Started Recommendations

### For New Users
1. Start with **CONSERVATIVE** settings
2. Run for at least 1 week to understand bot behavior
3. Monitor daily and review decisions
4. Only increase aggressiveness after consistent profits

### For Experienced Users Coming from Losses
1. Reset to **CONSERVATIVE** settings
2. Verify all advanced features are ON
3. Ensure Min R/R Ratio is 3.0 (not lower)
4. Consider reducing account size until consistent

### For Testing New Strategies
1. Use **Binance Testnet** first
2. Run with CONSERVATIVE settings
3. Analyze at least 20 trades before going live
4. Use backtesting feature to validate

---

## Expected Growth Scenarios

### Conservative Settings ($250 Account)

**Assumptions**:
- Win rate: 55% (with 3:1 R/R)
- Avg win: +18% wallet ($45)
- Avg loss: -6% wallet ($15)

**After 20 trades (1-2 weeks)**:
- 11 wins = +$495
- 9 losses = -$135
- **Net**: +$360 → **$610 total** (+144%)

### Moderate Settings ($500 Account)

**Assumptions**:
- Win rate: 50% (with 3:1 R/R)
- Avg win: +30% wallet ($150)
- Avg loss: -10% wallet ($50)

**After 20 trades (1-2 weeks)**:
- 10 wins = +$1500
- 10 losses = -$500
- **Net**: +$1000 → **$1500 total** (+200%)

### Aggressive Settings ($1000 Account)

**Assumptions**:
- Win rate: 45% (with 3:1 R/R)
- Avg win: +40% wallet ($400)
- Avg loss: -13% wallet ($130)

**After 20 trades (1 week)**:
- 9 wins = +$3600
- 11 losses = -$1430
- **Net**: +$2170 → **$3170 total** (+217%)

**Note**: Higher variance means worse results are possible. The aggressive profile can also lose 30%+ in bad sessions.

---

## Troubleshooting

### Bot Not Opening Trades
**Check**:
1. Is confidence threshold too high?
2. Is the market sideways (failing trend strength gate)?
3. Are there already max positions open?
4. Is the daily loss limit hit?

### Bot Closing Too Early
**Check**:
1. Is noise zone enabled? (Should be ON)
2. Is trailing stop activating too early?
3. Is max hold duration too short?

### Bot Not Closing Losers
**Check**:
1. Is PnL in noise zone? (Expected behavior)
2. Is smart loss cut enabled?
3. Is loss cut threshold realistic?

### Excessive Trading (Churn)
**Check**:
1. Is Turbo Mode OFF?
2. Is trading interval at least 5 minutes?
3. Is min confidence high enough (75%+)?
