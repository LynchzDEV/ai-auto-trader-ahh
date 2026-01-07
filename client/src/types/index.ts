export interface Strategy {
  id: string;
  name: string;
  description: string;
  is_active: boolean;
  config: StrategyConfig;
  created_at: string;
  updated_at: string;
}

export interface StrategyConfig {
  coin_source: CoinSourceConfig;
  indicators: IndicatorConfig;
  risk_control: RiskControlConfig;
  ai: AIConfig;
  custom_prompt: string;
  trading_interval: number;
  turbo_mode: boolean;
  simple_mode?: boolean;
}

export interface AIConfig {
  enable_reasoning: boolean;
  reasoning_model: string;
}

export interface CoinSourceConfig {
  source_type: string;
  static_coins: string[];
}

export interface IndicatorConfig {
  primary_timeframe: string;
  kline_count: number;
  enable_ema: boolean;
  enable_macd: boolean;
  enable_rsi: boolean;
  enable_atr: boolean;
  enable_boll: boolean;
  enable_volume: boolean;
  ema_periods: number[];
  rsi_period: number;
  atr_period: number;
  boll_period: number;
  macd_fast: number;
  macd_slow: number;
  macd_signal: number;
}

export interface RiskControlConfig {
  max_positions: number;
  max_leverage: number;
  max_position_percent: number;
  max_margin_usage: number;
  min_position_usd: number;
  min_position_size_btc_eth?: number;
  min_confidence: number;
  min_risk_reward_ratio: number;
  high_confidence_close_threshold?: number;
  max_daily_loss_pct?: number;
  max_drawdown_pct?: number;
  stop_trading_mins?: number;
  close_positions_on_daily_loss?: boolean;
  enable_emergency_shutdown?: boolean;
  emergency_min_balance?: number;
  // Trailing Stop Loss
  enable_trailing_stop?: boolean;
  trailing_stop_activate_pct?: number;
  trailing_stop_distance_pct?: number;
  // Max Hold Duration
  enable_max_hold_duration?: boolean;
  max_hold_duration_mins?: number;
  // Smart Loss Cut
  enable_smart_loss_cut?: boolean;
  smart_loss_cut_mins?: number;
  smart_loss_cut_pct?: number;
}

export interface Trader {
  id: string;
  name: string;
  strategy_id: string;
  exchange: string;
  status: 'running' | 'stopped';
  initial_balance: number;
  config: TraderConfig;
  created_at: string;
}

export interface TraderConfig {
  ai_provider: string;
  ai_model: string;
  api_key: string;
  secret_key: string;
  testnet: boolean;
  use_custom_model?: boolean;
  enable_reasoning?: boolean;
  reasoning_model?: string;
  // Per-trader OpenRouter config (falls back to global if empty)
  openrouter_api_key?: string;
  openrouter_model?: string;
}

export interface Position {
  symbol: string;
  side: string;
  amount: number;
  entry_price: number;
  mark_price: number;
  pnl: number;
  pnl_percent: number;
  leverage: number;
}

export interface Decision {
  id: number;
  trader_id: string;
  timestamp: string;
  decisions: string;
  executed: boolean;
}
