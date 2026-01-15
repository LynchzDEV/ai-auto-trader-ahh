# Changelog

All notable changes to this project will be documented in this file.

## [v3.49.0] - 2026-01-15

### Added
- **Auto-Avoid Worst Symbols**: New feature that automatically excludes symbols with consistent losses in the last 24 hours from trading. Configurable with `EnableAutoAvoidWorstSymbols`, `AutoAvoidMinLoss24h` (default: 5 USDT), and `AutoAvoidMinTrades24h` (default: 2). Works for both standard trading and **Smart Find** discovery to prevent "finding" known losers. Symbols with open positions are still analyzed.
- **24h Trading History AI Context**: AI now receives the past 24 hours of trading history for the specific symbol it is analyzing, including **entry reasons** (why it opened), P&L, and close reasons. This helps the AI learn from recent trades and avoid repeating failed patterns on that specific asset.
- **Account Worst Performers Context**: The AI is now provided with a ranking of the top 5 worst-performing symbols across your entire account in the last 24h. If the current symbol is on this "worst list," the AI receives a critical warning to be extremely cautious. **System Prompt** updated to explicitly enforce checking this list and historical performance.

## [v3.48.3] - 2026-01-15

### Fixed
- **Daily Loss Persistence**: Fixed critical bug where daily loss limit state was reset on bot restart. Trading pause now survives restarts by persisting `stopUntil`, `initialBalance`, and `lastResetTime` to database.

## [v3.48.2] - 2026-01-14

### Fixed
- **Guaranteed Profit Logging**: Add logging for guaranteed profit settings to aid debugging. (#2329f4c)

## [v3.48.1] - 2026-01-14

### Fixed
- **Signal Confirmation**: Normalize AI action comparison in signal confirmation to treat equivalent bullish/bearish signals (BUY/LONG, SELL/SHORT) as the same. (#7565b2a)

## [v3.48.0] - 2026-01-14

### Added
- **Guaranteed Minimum Profit**: New feature to prevent profitable trades from turning into losses by automatically protecting gains once a threshold is reached. (#ed3aea5)

## [v3.47.1] - 2026-01-14

### Fixed
- **Trading Pairs Logging**: Add defensive logging when no trading pairs are found to help diagnose configuration issues. (#986d1d7)

## [v3.47.0] - 2026-01-14

### Added
- **Signal Confirmation UI**: Added UI controls for Signal Confirmation settings (delay, confidence thresholds, price stability) to the Strategies page. (#4cbd76d)

## [v3.46.0] - 2026-01-14

### Added
- **Funding Rate Context**: Add funding rate information to AI context for more accurate profit calculations in perpetual futures trading. (#a211e41)

## [v3.45.0] - 2026-01-14

### Added
- **AI TP/SL Hybrid Mode**: New mode that trusts AI-suggested take profit and stop loss levels with minimal constraints, giving the AI more control over risk management. (#587d511)

## [v3.44.0] - 2026-01-14

### Added
- **Signal Confirmation System**: New confirmation system for medium-confidence trades that waits for price stability and AI re-confirmation before executing. (#ce4b0dd)

## [v3.43.1] - 2026-01-14

### Fixed
- **AI Response Parsing**: Strip markdown formatting from AI responses to prevent parsing errors. (#854fe8d)

## [v3.43.0] - 2026-01-14

### Added
- **Market Intelligence Toggle**: Made the Market Intelligence feature optional with a UI toggle, allowing users to enable/disable external data enrichment. (#616a129)

## [v3.42.5] - 2026-01-14

### Fixed
- **Intel Data Quality**: Provide clean market data without prescriptive warnings, letting AI make its own interpretations. (#f6691c8)

## [v3.42.4] - 2026-01-14

### Fixed
- **Risk Management**: Raise minimum stop loss to 3% and adjust take profit for 3:1 risk-reward ratio. (#a667711)

## [v3.42.3] - 2026-01-14

### Fixed
- **CoinGecko Rate Limiting**: Add rate limiting for CoinGecko API calls and widen SL/TP parameters. (#52f218c)

## [v3.42.2] - 2026-01-14

### Fixed
- **Trend Strength**: Tighten EMA trend strength gates to filter out weak signals. (#d093931)

## [v3.42.1] - 2026-01-14

### Fixed
- **CoinGecko Data Formatting**: Ensure dynamic CoinGecko data is correctly formatted for AI by passing symbol-ID mapping. (#cf1968b)

## [v3.42.0] - 2026-01-14

### Added
- **LunarCrush & TradingView Integration**: Added LunarCrush social sentiment and TradingView technical ratings to the intel module. (#22d4cf6)

## [v3.41.0] - 2026-01-14

### Added
- **CoinGecko Dynamic Search**: Added CoinGecko search with dynamic ID caching to automatically find coin data for any symbol. (#9e85d97)

## [v3.40.2] - 2026-01-14

### Fixed
- **Intel Caching**: Use per-symbol caching for intel data to prevent data mixing between different coins. (#15f97a6)

## [v3.40.1] - 2026-01-14

### Fixed
- **Intel Logging**: Improved wording for Market Intelligence logs. (#86e3827)

## [v3.40.0] - 2026-01-14

### Changed
- **News Source**: Switch to Google News RSS for live market intelligence, providing more reliable and up-to-date news data. (#7213a73)

## [v3.39.1] - 2026-01-14

### Fixed
- **Intel Debug Logs**: Added [Intel] prefix to debug logs for easier filtering. (#5706ee1)

## [v3.39.0] - 2026-01-14

### Added
- **Intel Module Tests**: Added comprehensive test suite for the market intelligence module. (#7997435)

## [v3.38.2] - 2026-01-14

### Fixed
- **Intel Integration**: Inject intel data into analyzeAndTrade function for AI decision making. (#2e2dc41)

## [v3.38.1] - 2026-01-13

### Fixed
- **SQLite Timestamps**: Handle SQLite timestamp format with UTC suffix for proper date parsing. (#976a02e)

## [v3.38.0] - 2026-01-13

### Added
- **Free Market Intelligence**: Added a free market intelligence module to replace OpenRouter web access, providing news and market data without additional API costs. (#81c9ac3)

## [v3.37.9] - 2026-01-13

### Fixed
- **CI Build**: Disable GitHub Actions cache export and switch to pure Go SQLite driver for more reliable builds. (#81b1a73)

## [v3.37.8] - 2026-01-13

### Fixed
- **Position Close UI Sync**: Update UI immediately when AI closes position on Binance, ensuring real-time dashboard accuracy. (#847dd63)

## [v3.37.7] - 2026-01-13

### Fixed
- **Position Close UI Sync**: Fixed bug where closed positions remained visible in the UI after AI closed them on Binance. Positions now update immediately instead of waiting for background sync. (#ace36ce)

## [v3.37.6] - 2026-01-13

### Fixed
- **Dust Position Filter**: Filter out positions with notional value < $1 from display. Fixes issues where closed positions or tiny dust amounts were still showing in the dashboard. (#df071a9)

## [v3.37.5] - 2026-01-13

### Changed
- **Auto Smart Find Volatility**: Auto Smart Find now always uses volatility-based coin selection instead of volume-based, ensuring it finds coins with actual momentum. (#571d357)

## [v3.37.4] - 2026-01-13

### Fixed
- **Auto Smart Find**: Fixed Auto Smart Find to always use volatility-based coin selection. (#b092e8a)

## [v3.37.3] - 2026-01-13

### Fixed
- **Dynamic Coin Source**: Add top_volume to source type check for dynamic coin source. (#4e127ae)

## [v3.37.2] - 2026-01-13

### Fixed
- **PnL Display**: Improve PnL display and minimum position validation. (#a8745c6)

## [v3.37.1] - 2026-01-13

### Fixed
- **Auto Smart Find UI**: Always show Auto Smart Find section in Turbo Mode. (#560e758)

## [v3.37.0] - 2026-01-13

### Added
- **Smart Find Auto-Refresh**: New toggle (Turbo Mode only) that automatically cycles Smart Find at configurable intervals (30min, 1hr, 2hr, 4hr) to discover new risky symbols. Analyzes open positions first, then finds 2x max_positions symbols. (#9349c5f)

## [v3.36.10] - 2026-01-12

### Fixed
- **Trailing Stop & Drawdown**: Use raw price % instead of ROE for trailing stop and drawdown calculations for more accurate profit tracking. (#093cdef)

## [v3.36.9] - 2026-01-11

### Fixed
- **Dynamic Noise Zone Config**: Use dynamic noise zone config values in AI prompts instead of hardcoded values. (#3dcaac8)

## [v3.36.8] - 2026-01-11

### Fixed
- **Critical PnL Bug**: Fixed critical PnL calculation bug that was causing massive churn loss. (#28b0f58)

## [v3.36.7] - 2026-01-10

### Fixed
- **Critical Engine Bugs**: Fixed critical engine bugs causing potential losses. (#12218f1)

## [v3.36.6] - 2026-01-10

### Fixed
- **Noise Zone UI**: Refined Noise Zone Protection UI and layout. (#918b2a4)

## [v3.36.5] - 2026-01-10

### Fixed
- **Algo Order Handling**: Handle algo order cancellations and emergency SLs properly. (#5c0719d)

## [v3.36.4] - 2026-01-10

### Fixed
- **Trend Strength Gate**: Add trend strength gate to prevent entries in weak/sideways markets. (#d35fc8f)

## [v3.36.3] - 2026-01-09

### Fixed
- **High Priority Bugs**: Fixed high priority bugs with comprehensive test suite. (#da48585)

## [v3.36.2] - 2026-01-09

### Fixed
- **Critical Trading Bugs**: Fixed critical bugs causing trading losses. (#5148d7a)

## [v3.36.1] - 2026-01-09

### Fixed
- **P&L Calculation**: Remove leverage multiplier from risk monitoring P&L calculation. (#747fe8c)

## [v3.36.0] - 2026-01-09

### Added
- **Skip Exchange TP**: Skip exchange TP order when trailing stop is enabled - let TSL handle profits instead. (#cbece43)

## [v3.35.1] - 2026-01-09

### Fixed
- **Input Validation**: Allow negative numbers in Smart Loss Cut and Noise Zone inputs. (#a8c8385)

## [v3.35.0] - 2026-01-08

### Added
- **Noise Zone Protection UI**: Add configurable Noise Zone Protection settings in the UI. (#b1764a5)

## [v3.34.1] - 2026-01-08

### Fixed
- **Simple Mode + Trailing Stop**: Simple Mode now works correctly with Trailing Stop and other features. Redesigned to only disable automatic drawdown protection. (#97bfb36)

## [v3.34.0] - 2026-01-08

### Added
- **Live Strategy Reload**: Strategy configuration changes now apply immediately to running traders without requiring a restart. (#0c402e2)

## [v3.33.2] - 2026-01-07

### Fixed
- **Copy Trading Testnet**: Mock Copy Trading status on Testnet. (#5f66f9c)

## [v3.33.1] - 2026-01-07

### Changed
- **Copy Trading UI**: Hidden irrelevant strategy settings when Copy Trading mode is active. (#40175a7)

## [v3.33.0] - 2026-01-07

### Added
- **Copy Trading Support**: Added "Binance Copy Trading" mode to strategies. In this mode, the bot acts as a monitor for a copy trading portfolio without executing independent AI trades. (#25e8b00)

## [v3.22.0] - 2026-01-07

### Added
- **Dashboard Enhancements**:
  - Added signal summary indicators (e.g., "2 BUY, 1 SELL") to the AI Decisions card header. (#c707a24)
  - Implemented detailed "Spotlight Cards" for decision logs with color-coded confidence scores and reasoning snippets. (#c707a24)

## [v3.21.0] - 2026-01-07

### Added
- **Loss Limits UI**: Added input fields for "Max Daily Loss %" and "Max Drawdown %" in Strategy Configuration. (#d9ebae0)
- **Documentation**: Added recommended settings documentation suggesting a 15% daily loss limit for high leverage trading. (#0d582dc)

## [v3.20.0] - 2026-01-06

### Added
- **Emergency Shutdown**:
  - Added UI toggle and threshold input for the "Emergency Shutdown" system. (#185bc17)
  - Implemented backend logic to actively monitor account equity at the start of each cycle and halt trading if it falls below the safety floor (default $60). (#a6fde77)

## [v3.19.0] - 2026-01-06

### Changed
- **Smart Loss V2**: Upgraded the smart loss logic to be more tolerant of volatility when using high leverage (20x+), preventing "shake-out" exits on noise. (#eaad3b4)

## [v3.18.0] - 2026-01-06

### Added
- **Three-Zone Management**: Implemented "Profit", "Noise", and "Danger" zones in both the engine logic and AI prompts to nuanced position management. (#38fc064)

### Fixed
- **Leverage Calculation**: Fixed a bug where leverage multipliers were not correctly applying to position size calculations in some edge cases. (#c415855)

## [v3.17.0] - 2026-01-06

### Added
- **Anti-Hedging Logic**: Added safety checks to prevent opening a position if an opposite position already exists (e.g., won't open LONG if SHORT exists) to avoid "Hedge Mode" API errors. (#9b58c26)

## [v3.15.0] - 2026-01-06

### Added
- **Turbo Mode**:
  - Added "Turbo Mode" toggle to Strategy settings for high-frequency scalping. (#5fea4a6)
  - Updated "Smart Find" to recommend volatile pairs suitable for Turbo strategies. (#0f14525)
- **UI**: Added badges to the static coin input field for better visibility. (#0f14525)

## [v3.13.0] - 2026-01-06

### Added
- **Global Context**: Added logic to fetch 24h ticker stats for `BTCUSDT` and inject it into the AI prompt for every trade, providing global market sentiment context. (#d30b523)

### Fixed
- **Validation**: Fixed bug where positions were sometimes closed prematurely due to incorrect profit threshold calculations in negative PnL scenarios. (#ff0adf0)

## [v3.10.0] - 2026-01-06

### Added
- **Auto-Reversal**: Implemented logic to automatically close an existing position if the AI signals a reversal (e.g., Close SHORT and Open LONG). (#125252e)

## [v3.9.0] - 2026-01-06

### Added
- **Dynamic Sourcing**: Added "Top by Volume" option to Coin Source configuration, allowing the bot to automatically trade the top 20 volume coins on Binance. (#dfdc86d)

## [v3.7.0] - 2026-01-05

### Added
- **Live Logs**: Implemented Server-Sent Events (SSE) to stream server logs directly to the frontend UI in real-time. (#64e84ca)

### Fixed
- **Networking**: Configured custom HTTP transport for OpenRouter client to force IPv4 usage, resolving persistent "Context Deadline Exceeded" timeout errors. (#dafd99b)

## [v3.5.0] - 2026-01-05

### Added
- **Bubble Chart**: Integrated d3-force to create an interactive, physics-based bubble chart on the Rankings page to visualize symbol performance. (#aeef6c3)

## [v3.0.0] - 2026-01-05

### Added
- **Global Settings**: Added a new "Configuration" section to the UI for managing global API keys (OpenRouter, Binance), simplifying setup for multi-bot environments. (#faa9c78)

## [v2.0.0] - 2026-01-05

### Changed
- **Mobile UI**: Major overhaul of the mobile interface, introducing a bottom navigation dock and converting data tables to card views for better mobile usability. (#baf98a6)

## [v1.6.0] - 2026-01-04

### Added
- **PnL Tracking**: Implemented polling of Binance Trade History to accurately track and display "Realized PnL" separate from Unrealized PnL. (#44ce590)

## [v1.4.10] - 2026-01-03

### Fixed
- **Order Types**: Switched to using Binance `STOP_MARKET` and `TAKE_PROFIT_MARKET` Algo Orders for SL/TP to resolve "Order Type Not Supported" errors in One-Way Mode. (#720688d)

## [v1.4.7] - 2026-01-01

### Added
- **Initial Release**: Baseline version with core AI decision loop and basic execution logic.
