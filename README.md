# Passive Income Ahh

[![GitHub stars](https://img.shields.io/github/stars/LynchzDEV/ai-auto-trader-ahh?style=social)](https://github.com/LynchzDEV/ai-auto-trader-ahh/stargazers)
[![GitHub forks](https://img.shields.io/github/forks/LynchzDEV/ai-auto-trader-ahh?style=social)](https://github.com/LynchzDEV/ai-auto-trader-ahh/network/members)
[![GitHub issues](https://img.shields.io/github/issues/LynchzDEV/ai-auto-trader-ahh)](https://github.com/LynchzDEV/ai-auto-trader-ahh/issues)
[![GitHub contributors](https://img.shields.io/github/contributors/LynchzDEV/ai-auto-trader-ahh)](https://github.com/LynchzDEV/ai-auto-trader-ahh/graphs/contributors)
[![GitHub license](https://img.shields.io/github/license/LynchzDEV/ai-auto-trader-ahh)](https://github.com/LynchzDEV/ai-auto-trader-ahh/blob/main/LICENSE)

An advanced AI-powered cryptocurrency futures trading platform that leverages multi-agent debate consensus, comprehensive backtesting, and real-time portfolio management to automate trading strategies on Binance Futures.

## 🚀 Key Features

*   **🤖 Multi-AI Debate System**: Utilizes multiple AI personas (e.g., Risk Manager, Technical Analyst, Fundamentalist) to debate and reach a consensus on trading decisions.
*   **🧠 Advanced Decision Engine**: Integrates OpenRouter to access top-tier LLMs (DeepSeek, Claude, GPT-4) for market analysis.
*   **📊 Comprehensive Backtesting**: Robust engine to test strategies against historical Binance data with detailed performance metrics (Sharpe ratio, max drawdown, win rate).
*   **⚡ Real-time Trading**: Automated, low-latency execution on Binance Futures with support for both Testnet and Mainnet.
*   **🛡️ Risk Management**: Built-in position sizing, stop-loss/take-profit automation, and leverage controls.
*   **🖥️ Modern Dashboard**: a sleek, glassmorphism-inspired UI built with React & TailwindCSS for real-time monitoring of positions, equity, and logs.
*   **📝 Live System Logs**: Real-time streaming of server logs directly to the frontend for easy debugging and monitoring.
*   **🏆 Strategy Ranking**: Visual comparison of different strategy performances.

## 🧠 How It Works

The system operates on an automated loop that combines technical analysis with AI reasoning:

1.  **Market Analysis**: Before consulting AI, the system calculates hard mathematical indicators (EMA trends, RSI levels, ATR volatility).
2.  **AI Decision**: A structured prompt including account state, market data, and active positions is sent to the LLM (e.g., DeepSeek, Claude). The AI "thinks" (Chain of Thought) and outputs a JSON decision with confidence levels, stop-losses, and take-profits.
3.  **Risk Validation**: A rigid validation layer strictly enforces a minimum 3:1 Reward-to-Risk ratio and position sizing limits. Even if the AI says "BUY", the math can veto it.
4.  **Execution**: Validated trades are executed as "Bracket Orders" on Binance—simultaneously placing the Entry, Stop Loss, and Take Profit orders to ensure safety.

For a deep dive into the math and logic, check out [TRADING_ALGO.md](TRADING_ALGO.md).


## 📂 Project Structure

A high-level overview of the codebase structure:

```
auto-trader-ahh/
├── client/                 # Frontend Application (React + Vite)
│   ├── src/
│   │   ├── components/     # Reusable UI components (Charts, Layouts, etc.)
│   │   ├── lib/            # API clients and utilities
│   │   ├── pages/          # Application views (Dashboard, Backtest, Logs, etc.)
│   │   ├── types/          # TypeScript interfaces
│   │   └── App.tsx         # Main entry point with routing
│   ├── Dockerfile          # Frontend container definition
│   └── package.json        # Frontend dependencies
│
├── server/                 # Backend Application (Go)
│   ├── api/                # HTTP API endpoints and server handlers
│   ├── backtest/           # Backtesting engine and simulation logic
│   ├── config/             # Configuration loading and validation
│   ├── data/               # SQLite database storage
│   ├── debate/             # Multi-agent debate and consensus logic
│   ├── decision/           # AI decision-making prompt engineering and parsing
│   ├── exchange/           # Binance Futures API integration
│   ├── logger/             # Custom logging and broadcasting system
│   ├── mcp/                # Model Context Protocol (AI client integration)
│   ├── store/              # Database repositories (Equity, Trades, Settings)
│   ├── strategy/           # Strategy interfaces and definitions
│   ├── trader/             # Core trading engine and execution loop
│   ├── main.go             # Application entry point
│   ├── Dockerfile          # Backend container definition
│   └── go.mod              # Go module definitions
│
├── docker-compose.yml      # Container orchestration
└── README.md               # Project documentation
```

## 🛠️ Tech Stack

**Backend**
*   **Language**: Go 1.23
*   **Database**: SQLite
*   **AI Integration**: OpenRouter API (DeepSeek, Anthropic, OpenAI)
*   **Exchange**: Binance Futures API
*   **Libraries**: generic-go-binance, go-sqlite3

**Frontend**
*   **Framework**: React 18, Vite
*   **Language**: TypeScript
*   **Styling**: TailwindCSS, Framer Motion
*   **Components**: Shadcn/UI, Lucide Icons
*   **Visualization**: Recharts

## 🏁 Getting Started

### Prerequisites

*   **Go** 1.23+
*   **Node.js** 20+
*   **Docker** & **Docker Compose** (recommended)
*   **Binance Futures Account** (Testnet recommended for development)
*   **OpenRouter API Key**

### 🐳 Docker Quick Start (Recommended)

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
    API_PORT=your_port_here
    ACCESS_PASSKEY=your_key_here (recommend for security)
    ```

3.  **Run with Docker Compose**:
    ```bash
    cd ..
    docker compose up -d --build
    ```

4.  **Access the App**:
    *   **Dashboard**: [http://localhost:5173](http://localhost:5173)
    *   **API**: [http://localhost:8080](http://localhost:8080)

### 🔧 Manual Installation

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

## ⚙️ Configuration

The system is highly configurable via the Dashboard "Settings" page or `server/.env`.

| Environment Variable | Description | Default |
|----------------------|-------------|---------|
| `API_PORT` | Port for the Go server | `8080` |
| `ACCESS_PASSKEY` | Application password for login | Optional |

## ⚠️ Disclaimer

This monitoring and trading software is for **educational and experimental purposes only**. Cryptocurrency trading involves significant financial risk. The authors and contributors are not responsible for any financial losses incurred while using this software. **Use at your own risk.**

## 📄 License

Distributed under the MIT License. See `LICENSE` for more information.
