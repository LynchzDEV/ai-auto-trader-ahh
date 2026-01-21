# Passive Income Ahh

[![GitHub stars](https://img.shields.io/github/stars/LynchzDEV/ai-auto-trader-ahh?style=social)](https://github.com/LynchzDEV/ai-auto-trader-ahh/stargazers)
[![GitHub forks](https://img.shields.io/github/forks/LynchzDEV/ai-auto-trader-ahh?style=social)](https://github.com/LynchzDEV/ai-auto-trader-ahh/network/members)
[![GitHub issues](https://img.shields.io/github/issues/LynchzDEV/ai-auto-trader-ahh)](https://github.com/LynchzDEV/ai-auto-trader-ahh/issues)
[![GitHub contributors](https://img.shields.io/github/contributors/LynchzDEV/ai-auto-trader-ahh)](https://github.com/LynchzDEV/ai-auto-trader-ahh/graphs/contributors)
[![GitHub license](https://img.shields.io/github/license/LynchzDEV/ai-auto-trader-ahh)](https://github.com/LynchzDEV/ai-auto-trader-ahh/blob/main/LICENSE)

An advanced AI-powered cryptocurrency futures trading platform that leverages multi-agent debate consensus, comprehensive backtesting, and real-time portfolio management to automate trading strategies on Binance Futures.

## Key Features

### Core Trading
*   **Multi-AI Debate System**: Multiple AI personas (Bull, Bear, Analyst, Contrarian, Risk Manager) debate and reach consensus on trading decisions
*   **Advanced Decision Engine**: Integrates OpenRouter to access top-tier LLMs (DeepSeek, Claude, GPT-4) with Chain-of-Thought reasoning
*   **Comprehensive Backtesting**: Test strategies against historical Binance data with detailed metrics (Sharpe ratio, max drawdown, win rate, profit factor)
*   **Real-time Trading**: Automated, low-latency execution on Binance Futures with Testnet and Mainnet support
*   **Bracket Orders**: Atomic execution of Entry + Stop Loss + Take Profit orders
*   **WebSocket Real-time Updates**: Sub-second position monitoring via Binance User Data Stream (1.13ms average latency vs 30-60s REST polling)

### Risk Management (15+ Layers)

#### Entry Safety (7 Layers)
*   **EMA Spread Gate**: Requires ≥0.6% EMA spread for entries - blocks weak/choppy trends
*   **Momentum Exhaustion Detection**: Blocks extended price + opposite MACD histogram entries
*   **Wick Rejection Pattern**: Blocks when 3+ of 5 candles show rejection wicks
*   **Volume Decline Detection**: Blocks when volume < 60% of 5-candle average
*   **Resistance/Support Buffer**: Blocks entries within 0.5% of 40-candle high/low
*   **RSI Extreme Blocking**: Blocks LONG if RSI >75, SHORT if RSI <25
*   **Counter-Trend Prevention**: LONG requires Price > EMA9, SHORT requires Price < EMA9

#### Multi-Timeframe Confirmation (4 Checks)
*   **Trend Direction**: Higher TF EMA9 vs EMA21 must align
*   **Price Action**: Price must respect higher TF EMA21
*   **MACD Momentum**: Higher TF histogram sign must support trade
*   **Trend Strength**: Higher TF EMA spread must be ≥0.4%

#### Open Interest Analysis (v3.52.0+)
*   **OI + Price Interpretation**: Real money flow analysis revealing if trends are backed by new capital
*   **Top Trader Long/Short Ratio**: Crowding detection warns when >70% of traders are on one side
*   **Falling Knife Protection**: Blocks LONG entries during detected Long Liquidation cascades
*   **Rocket Short Protection**: Blocks SHORT entries during detected Short Squeezes

#### Position Management
*   **Hard Validation**: Enforced 3:1 minimum risk/reward ratio
*   **Noise Zone Protection**: Block closing positions between -1.5% and +1.5% PnL to prevent panic selling
*   **Trailing Stop Loss**: Automatically lock profits at +1% with 0.5% trailing distance
*   **Smart Loss Cut**: Force close losers after extended hold time (30+ minutes with >1% loss)
*   **Max Hold Duration**: Automatically close positions held longer than 4 hours
*   **Drawdown Protection**: Close positions if drawdown from peak exceeds 40%
*   **Emergency Shutdown**: Halt trading if balance drops below configured threshold ($60 default)
*   **Guaranteed Profit Lock**: Automatically protect gains once threshold is reached (v3.48.0+)
*   **Daily Loss Limits**: Stop trading after reaching daily loss threshold (15% default)

### Dynamic Features
*   **Smart Find**: AI-recommended volatile trading pairs with auto-refresh and OI-based discovery
*   **Turbo Mode**: High-frequency scalping with dynamic coin discovery
*   **Copy Trading Mode**: Monitor positions without executing trades
*   **Live Strategy Reload**: Apply configuration changes without restarting
*   **Signal Confirmation**: Wait for price stability and AI re-confirmation before executing medium-confidence trades
*   **Bilingual Support**: English and Chinese AI prompts

### Modern Dashboard
*   **Glassmorphism UI**: Sleek React + TailwindCSS interface
*   **Real-time Logs**: Live streaming of server logs via SSE
*   **Equity Curve**: Visual portfolio growth tracking
*   **Strategy Ranking**: Compare strategy performance with interactive charts

## How It Works

The system operates on an automated loop (default: 5 minutes) combining technical analysis with AI reasoning:

1.  **Market Analysis**: Calculate hard mathematical indicators (EMA9/21 trends, RSI levels, MACD, ATR volatility, Bollinger Bands)
2.  **Trend Validation**: Check trend strength gate - only proceed if EMA spread > 0.2%
3.  **AI Decision**: Send structured prompt with account state, market data, and positions to LLM. The AI outputs a JSON decision with confidence, stop-loss, and take-profit levels
4.  **Risk Validation**: Enforce minimum 3:1 reward-to-risk ratio, leverage limits, and position sizing caps
5.  **Noise Zone Check**: Block closures in the -1.5% to +1.5% PnL zone unless confidence > 95%
6.  **Execution**: Execute validated trades as bracket orders (Entry + SL + TP simultaneously)
7.  **Position Management**: Track peak PnL, apply trailing stops, enforce max hold duration

For a deep dive into the math and logic, check out [TRADING_ALGO.md](TRADING_ALGO.md).

## Project Structure

```
auto-trader-ahh/
├── client/                 # Frontend Application (React + Vite)
│   ├── src/
│   │   ├── components/     # Reusable UI components (Charts, Layouts, etc.)
│   │   ├── contexts/       # React contexts (Auth, etc.)
│   │   ├── lib/            # API clients and utilities
│   │   ├── pages/          # Application views
│   │   │   ├── Dashboard   # Real-time trader status & positions
│   │   │   ├── Strategies  # Strategy configuration & management
│   │   │   ├── Backtest    # Historical backtesting
│   │   │   ├── Debate      # Multi-AI consensus arena
│   │   │   ├── History     # Trade history & PnL
│   │   │   ├── Equity      # Portfolio growth visualization
│   │   │   ├── Ranking     # Strategy performance comparison
│   │   │   ├── Config      # Global settings
│   │   │   └── Logs        # Real-time server logs
│   │   ├── types/          # TypeScript interfaces
│   │   └── App.tsx         # Main entry point with routing
│   ├── Dockerfile          # Frontend container definition
│   └── package.json        # Frontend dependencies
│
├── server/                 # Backend Application (Go)
│   ├── api/                # HTTP API endpoints (net/http)
│   ├── backtest/           # Backtesting engine and simulation
│   ├── config/             # Configuration loading and validation
│   ├── data/               # SQLite database storage (trading.db)
│   ├── debate/             # Multi-agent debate and consensus
│   ├── decision/           # AI decision engine (NOFX-style XML parsing)
│   │   ├── prompt_builder  # System/user prompt construction
│   │   ├── parser          # XML to JSON parsing
│   │   └── validator       # Risk/reward validation
│   ├── events/             # Event hub for real-time communication
│   ├── exchange/           # Binance Futures API integration
│   ├── logger/             # Log broadcasting system (SSE)
│   ├── market/             # Technical indicators (EMA, RSI, MACD, ATR, etc.)
│   ├── mcp/                # Multi-provider AI client (OpenRouter)
│   ├── store/              # Database models (Strategies, Traders, Trades, etc.)
│   ├── trader/             # Core trading engine and execution loop
│   ├── main.go             # Application entry point
│   ├── Dockerfile          # Backend container definition
│   └── go.mod              # Go module definitions
│
├── docker-compose.yml      # Container orchestration
├── TRADING_ALGO.md         # Algorithm documentation
├── RECOMMENDED_SETTINGS.md # Configuration guide
├── CHANGELOG.md            # Version history
└── README.md               # Project documentation
```

## Tech Stack

**Backend**
*   **Language**: Go 1.23
*   **Database**: SQLite
*   **AI Integration**: OpenRouter API (DeepSeek, Anthropic Claude, OpenAI GPT-4, Llama, etc.)
*   **Exchange**: Binance Futures API
*   **Libraries**: generic-go-binance, go-sqlite3

**Frontend**
*   **Framework**: React 18, Vite
*   **Language**: TypeScript
*   **Styling**: TailwindCSS, Framer Motion
*   **Components**: Shadcn/UI, Lucide Icons
*   **Visualization**: Recharts, D3

## Getting Started

### Prerequisites

*   **Go** 1.23+
*   **Node.js** 20+
*   **Docker** & **Docker Compose** (recommended)
*   **Binance Futures Account** (Testnet recommended for development)
*   **OpenRouter API Key**

### Docker Quick Start (Recommended)

1.  **Clone the repository**:
    ```bash
    git clone https://github.com/LynchzDEV/ai-auto-trader-ahh.git
    cd ai-auto-trader-ahh
    ```

2.  **Configure Environment**:
    Create a `.env` file in the `server/` directory:
    ```bash
    cd server
    cp .env.example .env
    ```
    Edit `.env` and add your keys:
    ```env
    API_PORT=8080
    ACCESS_PASSKEY=your_optional_passkey
    ```

3.  **Run with Docker Compose**:
    ```bash
    cd ..
    docker compose up -d --build
    ```

4.  **Access the App**:
    *   **Dashboard**: [http://localhost:5173](http://localhost:5173)
    *   **API**: [http://localhost:8080](http://localhost:8080)

### Manual Installation

#### Backend
```bash
cd server
go mod download
go run main.go
```

#### Frontend
```bash
cd client
npm install
npm run dev
```

## Configuration

The system is highly configurable via the Dashboard "Settings" page or `server/.env`.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `OPENROUTER_API_KEY` | OpenRouter API key for AI | Required |
| `OPENROUTER_MODEL` | AI model to use | `deepseek/deepseek-v3.2` |
| `BINANCE_API_KEY` | Binance Futures API key | Required |
| `BINANCE_SECRET_KEY` | Binance Futures secret | Required |
| `BINANCE_TESTNET` | Use testnet (true/false) | `true` |
| `API_PORT` | Port for the Go server | `8080` |
| `ACCESS_PASSKEY` | Optional app password | None |

### Strategy Configuration

| Setting | Description | Default |
|---------|-------------|---------|
| **Max Positions** | Maximum concurrent positions | 2 |
| **BTC/ETH Leverage** | Max leverage for BTC/ETH | 10x |
| **Altcoin Leverage** | Max leverage for altcoins | 20x |
| **Min Confidence** | Minimum AI confidence to trade | 85% |
| **Min R/R Ratio** | Minimum reward-to-risk ratio | 3.0 |
| **Trading Interval** | Minutes between trading cycles | 5 |
| **Daily Loss Limit** | Stop trading after this loss % | 15% |
| **Noise Zone** | PnL range to block closures | -1.5% to +1.5% |

See [RECOMMENDED_SETTINGS.md](RECOMMENDED_SETTINGS.md) for detailed configuration guides.

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/health` | Health check |
| GET | `/api/traders` | List traders |
| POST | `/api/traders` | Create trader |
| POST | `/api/traders/{id}/start` | Start trader |
| POST | `/api/traders/{id}/stop` | Stop trader |
| GET | `/api/strategies` | List strategies |
| POST | `/api/strategies` | Create strategy |
| PUT | `/api/strategies/{id}` | Update strategy |
| GET | `/api/status` | Get trader status |
| GET | `/api/positions` | Get open positions |
| GET | `/api/decisions` | Get recent AI decisions |
| GET | `/api/trades` | Get trade history |
| GET | `/api/backtest` | List backtests |
| POST | `/api/backtest/start` | Start backtest |
| GET | `/api/debate/sessions` | List debate sessions |
| POST | `/api/debate/sessions` | Create debate session |
| GET | `/api/settings` | Get global settings |
| PUT | `/api/settings` | Update global settings |
| GET | `/api/logs/stream` | SSE log stream |
| GET | `/api/events` | SSE event stream |

## Disclaimer

This trading software is for **educational and experimental purposes only**. Cryptocurrency futures trading involves significant financial risk including the possibility of losing more than your initial investment. The authors and contributors are not responsible for any financial losses incurred while using this software. **Use at your own risk.**

## License

Distributed under the MIT License. See `LICENSE` for more information.
