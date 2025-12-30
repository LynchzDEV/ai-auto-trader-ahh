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
  custom_prompt: string;
  trading_interval: number;
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
  min_confidence: number;
  min_risk_reward_ratio: number;
}

export interface Trader {
  id: string;
  name: string;
  strategy_id: string;
  exchange: string;
  status: string;
  initial_balance: number;
  config: TraderConfig;
  created_at: string;
  is_running?: boolean;
}

export interface TraderConfig {
  ai_provider: string;
  ai_model: string;
  api_key: string;
  secret_key: string;
  testnet: boolean;
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
