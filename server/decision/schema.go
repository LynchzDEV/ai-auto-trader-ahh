package decision

import (
	"fmt"
	"strings"
)

// SchemaEntry represents a single metric definition
type SchemaEntry struct {
	Name           string
	DescriptionEN  string
	DescriptionZH  string
	Formula        string
	CommonMistakes string
}

// DataDictionary defines the glossary of all metrics used in the system
var DataDictionary = []SchemaEntry{
	// --- Account Metrics ---
	{
		Name:           "Unrealized PnL",
		DescriptionEN:  "Current profit or loss of open positions based on mark price.",
		DescriptionZH:  "基于标记价格的当前持仓浮动盈亏。",
		Formula:        "(Mark Price - Entry Price) * Quantity * Direction (Long=1, Short=-1)",
		CommonMistakes: "Do not confuse with Realized PnL (closed trades). This value changes ticks by tick.",
	},
	{
		Name:           "Margin Ratio",
		DescriptionEN:  "Percentage of account equity currently used as margin.",
		DescriptionZH:  "当前账户权益中用作保证金的百分比。",
		Formula:        "(Total Margin Used / Total Equity) * 100",
		CommonMistakes: "High margin ratio (>50%) means high risk of liquidation if market moves against you.",
	},
	{
		Name:           "Drawdown",
		DescriptionEN:  "Decline from the peak equity or peak PnL.",
		DescriptionZH:  "从峰值权益或峰值盈亏的回撤幅度。",
		Formula:        "(Peak Value - Current Value) / Peak Value * 100",
		CommonMistakes: "AI often ignores drawdown. If a position was +20% and is now +5%, that's a 75% drawdown of profit!",
	},

	// --- Market Data Metrics ---
	{
		Name:           "Open Interest (OI)",
		DescriptionEN:  "Total value of all open futures contracts. Proxies for 'Money Flow'.",
		DescriptionZH:  "所有未平仓合约的总价值。代表'资金流向'。",
		Formula:        "Sum(Open Contracts * Price)",
		CommonMistakes: "Rising Price + Rising OI = Bullish (New Money). Rising Price + Falling OI = Bearish (Short Covering).",
	},
	{
		Name:           "OI Change 1H",
		DescriptionEN:  "Percentage change in Open Interest over the last hour.",
		DescriptionZH:  "过去1小时内持仓量的百分比变化。",
		Formula:        "(Current OI - OI 1h ago) / OI 1h ago * 100",
		CommonMistakes: "Negative OI Change often precedes a reversal (positions closing).",
	},
	{
		Name:           "Funding Rate",
		DescriptionEN:  "Periodic fee exchanged between longs and shorts. Positive = Longs pay Shorts.",
		DescriptionZH:  "多空双方定期交换的费用。正值=多头付给空头。",
		Formula:        "Based on premium between Futures Price and Spot Price",
		CommonMistakes: "Extremely high positive funding (>0.05%) means market is overheated/crowded long.",
	},
	{
		Name:           "Long/Short Ratio",
		DescriptionEN:  "Ratio of net long accounts vs net short accounts (Top Traders).",
		DescriptionZH:  "净多头账户与净空头账户的比例（顶尖交易员）。",
		Formula:        "Long Accounts / Short Accounts",
		CommonMistakes: "High L/S Ratio (>2.5) is often a BEARISH signal (Contra-indicator: Retail is crowded long).",
	},

	// --- Sentiment Analysis ---
	{
		Name:           "Sentiment Trend",
		DescriptionEN:  "Direction of retail sentiment shift over 1 hour (FOMO vs Fear).",
		DescriptionZH:  "过去1小时散户情绪变化的方向（FOMO vs 恐慌）。",
		Formula:        "Derived from L/S Ratio Change > 5% threshold",
		CommonMistakes: "If sentiment is 'BECOMING_BULLISH' (+5% L/S ratio), price often dumps (Liquidation hunting).",
	},
	{
		Name:           "Liquidation Pressure",
		DescriptionEN:  "Inferred status of forced liquidations based on OI drop and price velocity.",
		DescriptionZH:  "基于持仓量下降和价格速度推断的强制平仓状态。",
		Formula:        "If OI drops > 2% AND Price moves > 1.5% in opposite direction",
		CommonMistakes: "If 'LONG_LIQUIDATION' is active (Falling Knife), DO NOT BUY until it stops.",
	},

	// --- Technical Indicators ---
	{
		Name:           "RSI (Relative Strength Index)",
		DescriptionEN:  "Momentum indicator measuring speed/change of price movements.",
		DescriptionZH:  "衡量价格变动速度/变化的动能指标。",
		Formula:        "100 - (100 / (1 + RS))",
		CommonMistakes: "RSI > 70 is Overbought (Sell pressure), RSI < 30 is Oversold (Buy pressure). Don't buy at 80!",
	},
	{
		Name:           "EMA Spread",
		DescriptionEN:  "Percentage difference between EMA9 and EMA21.",
		DescriptionZH:  "EMA9和EMA21之间的百分比差异。",
		Formula:        "(EMA9 - EMA21) / EMA21 * 100",
		CommonMistakes: "If Spread < 0.6%, trend is weak/choppy. AI should avoid trading.",
	},
	{
		Name:           "MACD Histogram",
		DescriptionEN:  "Difference between MACD line and Signal line. Shows momentum strength.",
		DescriptionZH:  "MACD线与信号线的差值。显示动能强度。",
		Formula:        "MACD Line - Signal Line",
		CommonMistakes: "If Price is rising but MACD Hist is falling (Divergence), trend is weakening.",
	},
}

// GetSchemaPrompt generates the Data Dictionary section for the AI system prompt
func GetSchemaPrompt(lang Language) string {
	var sb strings.Builder

	if lang == LangChinese {
		sb.WriteString("\n\n## 数据词典与指标说明 (Data Dictionary)\n")
		sb.WriteString("为了确保决策准确，请严格遵循以下指标定义：\n\n")

		for _, entry := range DataDictionary {
			sb.WriteString(fmt.Sprintf("### %s\n", entry.Name))
			sb.WriteString(fmt.Sprintf("- **定义**: %s\n", entry.DescriptionZH))
			if entry.Formula != "" {
				sb.WriteString(fmt.Sprintf("- **公式/来源**: %s\n", entry.Formula))
			}
			if entry.CommonMistakes != "" {
				sb.WriteString(fmt.Sprintf("- **⚠️ 常见错误**: %s\n", entry.CommonMistakes))
			}
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("\n\n## Data Dictionary & Metric Definitions\n")
		sb.WriteString("To ensure accurate decisions, strictly adhere to these definitions:\n\n")

		for _, entry := range DataDictionary {
			sb.WriteString(fmt.Sprintf("### %s\n", entry.Name))
			sb.WriteString(fmt.Sprintf("- **Definition**: %s\n", entry.DescriptionEN))
			if entry.Formula != "" {
				sb.WriteString(fmt.Sprintf("- **Formula/Source**: %s\n", entry.Formula))
			}
			if entry.CommonMistakes != "" {
				sb.WriteString(fmt.Sprintf("- **⚠️ Common Mistakes**: %s\n", entry.CommonMistakes))
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
