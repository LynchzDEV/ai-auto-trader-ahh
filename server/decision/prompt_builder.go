package decision

import (
	"fmt"
	"strings"
)

// PromptBuilder constructs prompts for AI trading decisions
type PromptBuilder struct {
	lang Language
}

// NewPromptBuilder creates a new prompt builder
func NewPromptBuilder(lang Language) *PromptBuilder {
	return &PromptBuilder{lang: lang}
}

// BuildSystemPrompt builds the system prompt
func (pb *PromptBuilder) BuildSystemPrompt() string {
	if pb.lang == LangChinese {
		return pb.buildSystemPromptZH()
	}
	return pb.buildSystemPromptEN()
}

// BuildUserPrompt builds the user prompt with trading context
func (pb *PromptBuilder) BuildUserPrompt(ctx *Context) string {
	formattedData := FormatContextForAI(ctx, pb.lang)

	if pb.lang == LangChinese {
		return formattedData + pb.getDecisionRequirementsZH()
	}
	return formattedData + pb.getDecisionRequirementsEN()
}

// buildSystemPromptEN builds the English system prompt
func (pb *PromptBuilder) buildSystemPromptEN() string {
	return `You are a professional cryptocurrency futures trading analyst. Your task is to analyze market data and current positions, then make trading decisions.

## Role Definition
You are a disciplined, risk-first trading decision maker. You prioritize capital preservation over profit maximization.

## Core Decision Principles

### 1. Risk-First Philosophy
- Never risk more than the specified position limits
- Always set stop-loss before considering take-profit
- You MAY close losing positions identifying invalidation of the trade thesis
- Preserve capital - missing opportunities is better than losing capital

### 2. Trailing Take-Profit Strategy
- For profitable positions: Move stop-loss to breakeven when +5% profit
- Trail stops to lock in profits as price moves favorably
- Let winners run but protect unrealized gains
- Consider partial exits at key resistance/support levels

### 3. Trend-Following Approach
- Trade in the direction of the larger timeframe trend
- Don't fight strong momentum
- Wait for pullbacks to enter rather than chasing
- Use multiple timeframe confirmation

### CRITICAL: Trend Strength Gate
- **DO NOT OPEN** new positions when EMA9 vs EMA21 spread is below 0.2%
- Very weak trends (< 0.2% EMA spread) lead to choppy price action and stop-outs
- If you see "VERY WEAK TREND" or "SIDEWAYS MARKET" warnings, use action: "wait"
- Only enter when trend strength shows "Moderate" (> 0.2%) or "Strong" (> 0.5%)

### 4. Position Management
- Scale into positions gradually, not all at once
- Keep total margin usage below risk limits
- Diversify across uncorrelated assets when possible
- Reduce exposure during high uncertainty

## CRITICAL RULE: Smart Loss Management

**The Three Zones:**

1. **Significant Loss Zone** (Below -0.5%)
   - ✅ You CAN recommend close_long/close_short
   - Purpose: Cut losses early (at 20x leverage, -0.5% price = -10% equity)
   - Use when: Trade thesis is invalidated

2. **Noise Zone** (-0.5% to +1.5%)
   - ❌ You CANNOT close positions here...
   - **EXCEPTION**: If Confidence >= 80%, you CAN close (Override)
   - Purpose: Prevent churn on weak signals, but allow decisive action if sure

3. **Profit Zone** (Above +1.5%)
   - ✅ You CAN recommend close to lock in profits
   - Purpose: Secure gains (at 20x leverage, +1.5% price = +30% equity)
   - But prefer letting TP order reach the target if momentum is strong

**Key Guidelines:**
- Focus on finding high-quality ENTRY points with 3:1 R:R
- Trust the exchange SL/TP orders to manage routine exits
- Only intervene to cut significant losses or lock in strong profits
- HOLD positions for 30-60 minutes unless there's major invalidation
- If you just opened/closed a position, recommend HOLD for next few cycles

## Output Format Requirements

You MUST output your decisions in valid JSON format wrapped in <decision> tags:

<decision>
[
  {
    "symbol": "<THE_SYMBOL_YOU_ARE_ANALYZING>",
    "action": "open_long",
    "leverage": 10,
    "position_size_usd": 500,
    "stop_loss": 95000,
    "take_profit": 105000,
    "confidence": 75,
    "reasoning": "Strong bullish momentum on daily, breaking key resistance"
  }
]
</decision>

## Field Descriptions

- symbol: The EXACT trading pair you are analyzing (use the symbol from the market data provided, e.g., "BTCUSDT", "ETHUSDT", "DOGEUSDT", etc.)
- action: One of "open_long", "open_short", "close_long", "close_short", "hold", "wait"
- leverage: Leverage multiplier (1-20 for BTC/ETH, 1-10 for altcoins)
- position_size_usd: Position size in USDT
- stop_loss: Stop-loss price level
- take_profit: Take-profit price level
- confidence: Confidence level 0-100
- reasoning: Brief explanation of the decision

## Critical Reminders

1. ALL numeric values must be precise single numbers - NO ranges like "100-200"
2. stop_loss and take_profit must be valid price levels (not percentages)
3. For LONG positions: stop_loss < current_price < take_profit
4. For SHORT positions: take_profit < current_price < stop_loss
5. Risk/Reward ratio must be at least 3:1
6. If no good opportunities exist, use action: "wait" with symbol: "ALL"
7. Always output valid JSON - use straight quotes, not curly quotes
8. You CAN close positions with losses below -1.5%, but CANNOT close in the -1.5% to +3% noise zone`
}

// buildSystemPromptZH builds the Chinese system prompt
func (pb *PromptBuilder) buildSystemPromptZH() string {
	return `你是专业的加密货币合约交易分析师。你的任务是分析市场数据和当前持仓，然后做出交易决策。

## 角色定义
你是一个纪律严明、风险优先的交易决策者。你把资本保护放在利润最大化之上。

## 核心决策原则

### 1. 风险优先理念
- 永远不要超过指定的仓位限制
- 总是先设置止损再考虑止盈
- 果断平掉亏损仓位，不要摊平成本
- 保护本金 - 错过机会比亏损本金更好

### 2. 移动止盈策略
- 盈利仓位：当盈利达到+5%时，将止损移至保本位
- 随着价格有利变动，移动止损锁定利润
- 让盈利仓位继续运行，但保护未实现收益
- 在关键阻力/支撑位考虑部分平仓

### 3. 趋势跟随方法
- 顺着更大时间框架的趋势交易
- 不要逆势操作
- 等待回调进场而不是追高
- 使用多时间框架确认

### 重要：趋势强度门槛
- **禁止开仓** 当EMA9与EMA21差距低于0.2%时
- 非常弱的趋势（<0.2% EMA差距）会导致震荡行情和止损
- 如果看到"非常弱趋势"或"横盘市场"警告，使用action: "wait"
- 只在趋势强度显示"中等"（>0.2%）或"强"（>0.5%）时入场

### 4. 仓位管理
- 逐步建仓，不要一次性全仓
- 保持总保证金使用率在风险限制之下
- 尽可能在不相关的资产间分散
- 在高度不确定时减少敞口

## 输出格式要求

你必须以有效的JSON格式输出决策，包裹在<decision>标签中：

<decision>
[
  {
    "symbol": "<THE_SYMBOL_YOU_ARE_ANALYZING>",
    "action": "open_long",
    "leverage": 10,
    "position_size_usd": 500,
    "stop_loss": 95000,
    "take_profit": 105000,
    "confidence": 75,
    "reasoning": "日线强势看涨动能，突破关键阻力位"
  }
]
</decision>

## 字段说明

- symbol: The EXACT trading pair you are analyzing (use the symbol from the market data, e.g., "BTCUSDT", "ETHUSDT", "DOGEUSDT")
- action: "open_long", "open_short", "close_long", "close_short", "hold", "wait" 之一
- leverage: 杠杆倍数 (BTC/ETH 1-20，山寨币 1-10)
- position_size_usd: 仓位大小（USDT）
- stop_loss: 止损价格
- take_profit: 止盈价格
- confidence: 信心度 0-100
- reasoning: 决策的简要说明

## 重要提醒

1. 所有数值必须是精确的单一数字 - 不要使用范围如"100-200"
2. stop_loss和take_profit必须是有效价格（不是百分比）
3. 做多：止损 < 当前价格 < 止盈
4. 做空：止盈 < 当前价格 < 止损
5. 风险回报比必须至少3:1
6. 如果没有好机会，使用 action: "wait"，symbol: "ALL"
7. 总是输出有效JSON - 使用直引号，不要用弯引号`
}

// getDecisionRequirementsEN returns English decision requirements
func (pb *PromptBuilder) getDecisionRequirementsEN() string {
	return `

---

## Decision Steps

1. **Analyze Market Context**: Review account status, current positions, and market conditions
2. **Assess Risk**: Check margin usage, unrealized PnL, and potential exposure
3. **Evaluate Opportunities**: Look for high-probability setups with favorable risk/reward
4. **Make Decisions**: Output specific, actionable decisions with clear parameters

## Your Response

First, provide your reasoning in a <reasoning> tag:

<reasoning>
Your chain of thought analysis here...
</reasoning>

Then output your decisions in <decision> tags as shown in the format above.

If there are no actionable opportunities, output:
<decision>
[{"symbol": "ALL", "action": "wait", "reasoning": "No favorable setups identified"}]
</decision>`
}

// getDecisionRequirementsZH returns Chinese decision requirements
func (pb *PromptBuilder) getDecisionRequirementsZH() string {
	return `

---

## 决策步骤

1. **分析市场背景**：审查账户状态、当前持仓和市场情况
2. **评估风险**：检查保证金使用率、未实现盈亏和潜在敞口
3. **评估机会**：寻找高概率、风险回报比有利的设置
4. **做出决策**：输出具体、可执行的决策，包含明确参数

## 你的回复

首先，在<reasoning>标签中提供你的推理：

<reasoning>
你的思维链分析...
</reasoning>

然后按上述格式在<decision>标签中输出你的决策。

如果没有可操作的机会，输出：
<decision>
[{"symbol": "ALL", "action": "wait", "reasoning": "未发现有利设置"}]
</decision>`
}

// FormatContextForAI formats the trading context for AI consumption
func FormatContextForAI(ctx *Context, lang Language) string {
	var sb strings.Builder

	if lang == LangChinese {
		sb.WriteString(formatContextDataZH(ctx))
	} else {
		sb.WriteString(formatContextDataEN(ctx))
	}

	return sb.String()
}

// formatContextDataEN formats context data in English
func formatContextDataEN(ctx *Context) string {
	var sb strings.Builder

	// Header
	sb.WriteString("# Current Trading Context\n\n")
	sb.WriteString(fmt.Sprintf("**Time**: %s\n", ctx.CurrentTime))
	sb.WriteString(fmt.Sprintf("**Runtime**: %d minutes\n", ctx.RuntimeMinutes))
	sb.WriteString(fmt.Sprintf("**Analysis Count**: #%d\n\n", ctx.CallCount))

	// Account Info
	sb.WriteString("## Account Status\n\n")
	sb.WriteString(fmt.Sprintf("- Total Equity: $%.2f\n", ctx.Account.TotalEquity))
	sb.WriteString(fmt.Sprintf("- Available Balance: $%.2f\n", ctx.Account.AvailableBalance))
	sb.WriteString(fmt.Sprintf("- Unrealized PnL: $%.2f\n", ctx.Account.UnrealizedPnL))
	sb.WriteString(fmt.Sprintf("- Total PnL: $%.2f (%.2f%%)\n", ctx.Account.TotalPnL, ctx.Account.TotalPnLPct))
	sb.WriteString(fmt.Sprintf("- Margin Used: $%.2f (%.2f%%)\n", ctx.Account.MarginUsed, ctx.Account.MarginUsedPct))
	sb.WriteString(fmt.Sprintf("- Position Count: %d\n\n", ctx.Account.PositionCount))

	// Risk Warnings
	if ctx.Account.MarginUsedPct > 50 {
		sb.WriteString("**WARNING: High margin usage! Consider reducing positions.**\n\n")
	}
	if ctx.Account.UnrealizedPnL < -ctx.Account.TotalEquity*0.05 {
		sb.WriteString("**WARNING: Significant unrealized losses! Review positions carefully.**\n\n")
	}

	// Trading Stats
	if ctx.TradingStats != nil {
		sb.WriteString("## Trading Statistics\n\n")
		sb.WriteString(fmt.Sprintf("- Total Trades: %d\n", ctx.TradingStats.TotalTrades))
		sb.WriteString(fmt.Sprintf("- Win Rate: %.1f%%\n", ctx.TradingStats.WinRate))
		sb.WriteString(fmt.Sprintf("- Profit Factor: %.2f\n", ctx.TradingStats.ProfitFactor))
		sb.WriteString(fmt.Sprintf("- Sharpe Ratio: %.2f\n", ctx.TradingStats.SharpeRatio))
		sb.WriteString(fmt.Sprintf("- Total PnL: $%.2f\n", ctx.TradingStats.TotalPnL))
		sb.WriteString(fmt.Sprintf("- Avg Win: $%.2f | Avg Loss: $%.2f\n", ctx.TradingStats.AvgWin, ctx.TradingStats.AvgLoss))
		sb.WriteString(fmt.Sprintf("- Max Drawdown: %.2f%%\n\n", ctx.TradingStats.MaxDrawdownPct))
	}

	// Current Positions
	if len(ctx.Positions) > 0 {
		sb.WriteString("## Current Positions\n\n")
		for _, pos := range ctx.Positions {
			sb.WriteString(fmt.Sprintf("### %s %s\n", pos.Symbol, strings.ToUpper(pos.Side)))
			sb.WriteString(fmt.Sprintf("- Entry: $%.4f | Mark: $%.4f\n", pos.EntryPrice, pos.MarkPrice))
			sb.WriteString(fmt.Sprintf("- Quantity: %.4f | Leverage: %dx\n", pos.Quantity, pos.Leverage))
			sb.WriteString(fmt.Sprintf("- Unrealized PnL: $%.2f (%.2f%%)\n", pos.UnrealizedPnL, pos.UnrealizedPnLPct))
			sb.WriteString(fmt.Sprintf("- Peak PnL: %.2f%%\n", pos.PeakPnLPct))
			sb.WriteString(fmt.Sprintf("- Liquidation Price: $%.4f\n", pos.LiquidationPrice))
			sb.WriteString(fmt.Sprintf("- Margin Used: $%.2f\n\n", pos.MarginUsed))

			// Position-specific alerts
			if pos.UnrealizedPnLPct < -5 {
				sb.WriteString("**ALERT: Position down >5%! Consider cutting losses.**\n\n")
			}
			if pos.PeakPnLPct > 10 && pos.UnrealizedPnLPct < pos.PeakPnLPct-5 {
				sb.WriteString("**ALERT: Position retraced significantly from peak! Consider trailing stop.**\n\n")
			}
		}
	} else {
		sb.WriteString("## Current Positions\n\nNo open positions.\n\n")
	}

	// Recent Orders
	if len(ctx.RecentOrders) > 0 {
		sb.WriteString("## Recent Trades\n\n")
		for _, order := range ctx.RecentOrders {
			sb.WriteString(fmt.Sprintf("- %s %s: Entry $%.4f -> Exit $%.4f | PnL: $%.2f (%.2f%%) | Duration: %s\n",
				order.Symbol, order.Side, order.EntryPrice, order.ExitPrice,
				order.RealizedPnL, order.PnLPct, order.HoldDuration))
		}
		sb.WriteString("\n")
	}

	// Candidate Coins
	if len(ctx.CandidateCoins) > 0 {
		sb.WriteString("## Candidate Coins for Analysis\n\n")
		for _, coin := range ctx.CandidateCoins {
			sb.WriteString(fmt.Sprintf("- %s (Sources: %s)\n", coin.Symbol, strings.Join(coin.Sources, ", ")))
		}
		sb.WriteString("\n")
	}

	// Market Data
	if len(ctx.MarketDataMap) > 0 {
		sb.WriteString("## Market Data\n\n")
		for symbol, data := range ctx.MarketDataMap {
			sb.WriteString(fmt.Sprintf("### %s\n", symbol))
			sb.WriteString(fmt.Sprintf("- Price: $%.4f | 24h Change: %.2f%%\n", data.Price, data.Change24h))
			sb.WriteString(fmt.Sprintf("- 24h High: $%.4f | Low: $%.4f\n", data.HighPrice24h, data.LowPrice24h))
			sb.WriteString(fmt.Sprintf("- 24h Volume: $%.2f\n", data.Volume24h))
			sb.WriteString(fmt.Sprintf("- Open Interest: $%.2f | OI Change: %.2f%%\n", data.OpenInterest, data.OIChange24h))
			sb.WriteString(fmt.Sprintf("- Funding Rate: %.4f%%\n\n", data.FundingRate*100))
		}
	}

	// Position Limits
	sb.WriteString("## Position Limits\n\n")
	sb.WriteString(fmt.Sprintf("- BTC/ETH: Max %dx leverage, Max %.0f%% of equity per position\n",
		ctx.BTCETHLeverage, ctx.BTCETHPosRatio*100))
	sb.WriteString(fmt.Sprintf("- Altcoins: Max %dx leverage, Max %.0f%% of equity per position\n\n",
		ctx.AltcoinLeverage, ctx.AltcoinPosRatio*100))

	return sb.String()
}

// formatContextDataZH formats context data in Chinese
func formatContextDataZH(ctx *Context) string {
	var sb strings.Builder

	// Header
	sb.WriteString("# 当前交易环境\n\n")
	sb.WriteString(fmt.Sprintf("**时间**: %s\n", ctx.CurrentTime))
	sb.WriteString(fmt.Sprintf("**运行时间**: %d 分钟\n", ctx.RuntimeMinutes))
	sb.WriteString(fmt.Sprintf("**分析次数**: #%d\n\n", ctx.CallCount))

	// Account Info
	sb.WriteString("## 账户状态\n\n")
	sb.WriteString(fmt.Sprintf("- 总权益: $%.2f\n", ctx.Account.TotalEquity))
	sb.WriteString(fmt.Sprintf("- 可用余额: $%.2f\n", ctx.Account.AvailableBalance))
	sb.WriteString(fmt.Sprintf("- 未实现盈亏: $%.2f\n", ctx.Account.UnrealizedPnL))
	sb.WriteString(fmt.Sprintf("- 总盈亏: $%.2f (%.2f%%)\n", ctx.Account.TotalPnL, ctx.Account.TotalPnLPct))
	sb.WriteString(fmt.Sprintf("- 已用保证金: $%.2f (%.2f%%)\n", ctx.Account.MarginUsed, ctx.Account.MarginUsedPct))
	sb.WriteString(fmt.Sprintf("- 持仓数量: %d\n\n", ctx.Account.PositionCount))

	// Risk Warnings
	if ctx.Account.MarginUsedPct > 50 {
		sb.WriteString("**警告: 保证金使用率过高！考虑减少仓位。**\n\n")
	}
	if ctx.Account.UnrealizedPnL < -ctx.Account.TotalEquity*0.05 {
		sb.WriteString("**警告: 未实现亏损较大！请仔细审查持仓。**\n\n")
	}

	// Trading Stats
	if ctx.TradingStats != nil {
		sb.WriteString("## 交易统计\n\n")
		sb.WriteString(fmt.Sprintf("- 总交易次数: %d\n", ctx.TradingStats.TotalTrades))
		sb.WriteString(fmt.Sprintf("- 胜率: %.1f%%\n", ctx.TradingStats.WinRate))
		sb.WriteString(fmt.Sprintf("- 盈亏比: %.2f\n", ctx.TradingStats.ProfitFactor))
		sb.WriteString(fmt.Sprintf("- 夏普比率: %.2f\n", ctx.TradingStats.SharpeRatio))
		sb.WriteString(fmt.Sprintf("- 总盈亏: $%.2f\n", ctx.TradingStats.TotalPnL))
		sb.WriteString(fmt.Sprintf("- 平均盈利: $%.2f | 平均亏损: $%.2f\n", ctx.TradingStats.AvgWin, ctx.TradingStats.AvgLoss))
		sb.WriteString(fmt.Sprintf("- 最大回撤: %.2f%%\n\n", ctx.TradingStats.MaxDrawdownPct))
	}

	// Current Positions
	if len(ctx.Positions) > 0 {
		sb.WriteString("## 当前持仓\n\n")
		for _, pos := range ctx.Positions {
			direction := "多"
			if pos.Side == "short" {
				direction = "空"
			}
			sb.WriteString(fmt.Sprintf("### %s %s\n", pos.Symbol, direction))
			sb.WriteString(fmt.Sprintf("- 入场价: $%.4f | 标记价: $%.4f\n", pos.EntryPrice, pos.MarkPrice))
			sb.WriteString(fmt.Sprintf("- 数量: %.4f | 杠杆: %dx\n", pos.Quantity, pos.Leverage))
			sb.WriteString(fmt.Sprintf("- 未实现盈亏: $%.2f (%.2f%%)\n", pos.UnrealizedPnL, pos.UnrealizedPnLPct))
			sb.WriteString(fmt.Sprintf("- 峰值盈亏: %.2f%%\n", pos.PeakPnLPct))
			sb.WriteString(fmt.Sprintf("- 强平价格: $%.4f\n", pos.LiquidationPrice))
			sb.WriteString(fmt.Sprintf("- 占用保证金: $%.2f\n\n", pos.MarginUsed))

			if pos.UnrealizedPnLPct < -5 {
				sb.WriteString("**警报: 仓位下跌超过5%！考虑止损。**\n\n")
			}
			if pos.PeakPnLPct > 10 && pos.UnrealizedPnLPct < pos.PeakPnLPct-5 {
				sb.WriteString("**警报: 仓位从峰值大幅回撤！考虑移动止损。**\n\n")
			}
		}
	} else {
		sb.WriteString("## 当前持仓\n\n无持仓。\n\n")
	}

	// Recent Orders
	if len(ctx.RecentOrders) > 0 {
		sb.WriteString("## 近期交易\n\n")
		for _, order := range ctx.RecentOrders {
			sb.WriteString(fmt.Sprintf("- %s %s: 入场 $%.4f -> 平仓 $%.4f | 盈亏: $%.2f (%.2f%%) | 持仓时间: %s\n",
				order.Symbol, order.Side, order.EntryPrice, order.ExitPrice,
				order.RealizedPnL, order.PnLPct, order.HoldDuration))
		}
		sb.WriteString("\n")
	}

	// Candidate Coins
	if len(ctx.CandidateCoins) > 0 {
		sb.WriteString("## 待分析币种\n\n")
		for _, coin := range ctx.CandidateCoins {
			sb.WriteString(fmt.Sprintf("- %s (来源: %s)\n", coin.Symbol, strings.Join(coin.Sources, ", ")))
		}
		sb.WriteString("\n")
	}

	// Market Data
	if len(ctx.MarketDataMap) > 0 {
		sb.WriteString("## 市场数据\n\n")
		for symbol, data := range ctx.MarketDataMap {
			sb.WriteString(fmt.Sprintf("### %s\n", symbol))
			sb.WriteString(fmt.Sprintf("- 价格: $%.4f | 24h涨跌: %.2f%%\n", data.Price, data.Change24h))
			sb.WriteString(fmt.Sprintf("- 24h高点: $%.4f | 低点: $%.4f\n", data.HighPrice24h, data.LowPrice24h))
			sb.WriteString(fmt.Sprintf("- 24h成交量: $%.2f\n", data.Volume24h))
			sb.WriteString(fmt.Sprintf("- 持仓量: $%.2f | OI变化: %.2f%%\n", data.OpenInterest, data.OIChange24h))
			sb.WriteString(fmt.Sprintf("- 资金费率: %.4f%%\n\n", data.FundingRate*100))
		}
	}

	// Position Limits
	sb.WriteString("## 仓位限制\n\n")
	sb.WriteString(fmt.Sprintf("- BTC/ETH: 最大%dx杠杆，单仓最大%.0f%%权益\n",
		ctx.BTCETHLeverage, ctx.BTCETHPosRatio*100))
	sb.WriteString(fmt.Sprintf("- 山寨币: 最大%dx杠杆，单仓最大%.0f%%权益\n\n",
		ctx.AltcoinLeverage, ctx.AltcoinPosRatio*100))

	return sb.String()
}
