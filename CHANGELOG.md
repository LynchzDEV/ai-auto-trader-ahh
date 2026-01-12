# Changelog

All notable changes to this project will be documented in this file.

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
