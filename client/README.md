# Passive Income Ahh - Client

React frontend for the AI-powered trading platform.

## Tech Stack

- **React 18** - UI framework
- **TypeScript** - Type safety
- **Vite** - Build tool
- **TailwindCSS** - Styling
- **shadcn/ui** - UI components
- **Framer Motion** - Animations
- **Recharts** - Charts
- **React Router** - Navigation
- **Lucide Icons** - Icons

## Quick Start

```bash
# Install dependencies
npm install

# Start development server
npm run dev

# Build for production
npm run build

# Preview production build
npm run preview
```

## Project Structure

```
client/
├── public/
│   └── icon.svg           # App icon
├── src/
│   ├── components/
│   │   ├── ui/           # shadcn/ui components
│   │   └── Layout.tsx    # Main layout with sidebar
│   ├── lib/
│   │   ├── api.ts        # API client
│   │   └── utils.ts      # Utilities
│   ├── pages/
│   │   ├── Dashboard.tsx # Main dashboard
│   │   ├── Backtest.tsx  # Backtesting UI
│   │   ├── Debate.tsx    # AI debate arena
│   │   ├── Equity.tsx    # Equity charts
│   │   ├── History.tsx   # Trade history
│   │   ├── Strategies.tsx # Strategy management
│   │   ├── Config.tsx    # API configuration
│   │   └── Logs.tsx      # Decision logs
│   ├── App.tsx           # Routes
│   ├── main.tsx          # Entry point
│   └── index.css         # Global styles
├── index.html            # HTML template with SEO
└── vite.config.ts        # Vite configuration
```

## Pages

### Dashboard
- Real-time trader status
- Position monitoring
- Quick actions (start/stop traders)
- AI decision feed

### Backtest
- Configure backtest parameters
- Run historical simulations
- View equity curves
- Performance metrics

### Debate Arena
- Create multi-AI debate sessions
- Watch AI personalities discuss markets
- View consensus decisions
- Real-time message streaming (SSE)

### Equity
- Portfolio equity charts
- Time range selection
- Daily returns visualization

### History
- Complete trade log
- Filter by symbol, side, date
- View AI reasoning for each trade

### Strategies
- Create/edit trading strategies
- Risk parameter configuration
- AI model selection

### Config
- API key management
- Exchange configuration
- System settings

## Design System

### Theme
- Dark-only glassmorphism design
- Primary: Blue/Purple gradient
- Background: `#0a0a0f`
- Glass effects with blur/opacity

### CSS Classes
```css
.glass-card       /* Glassmorphism card */
.glass-sidebar    /* Sidebar with glass effect */
.glow-border      /* Animated glow border */
.glow-primary     /* Primary color glow */
.text-gradient    /* Gradient text effect */
.pulse-live       /* Live indicator pulse */
```

### Animation
- Framer Motion for page transitions
- Fade animations for loading states
- Number tickers for live data

## API Integration

API client in `src/lib/api.ts`:

```typescript
import * as api from '@/lib/api';

// Health check
await api.checkHealth();

// Traders
const traders = await api.getTraders();
await api.createTrader({ ... });
await api.startTrader(id);
await api.stopTrader(id);

// Backtests
await api.startBacktest({ ... });
const backtests = await api.getBacktests();

// Debates
await api.createDebateSession({ ... });
const sessions = await api.getDebateSessions();
```

## Environment

The client connects to the backend at `http://localhost:8080` by default.

To change the API URL, update `src/lib/api.ts`:

```typescript
const API_BASE = 'http://localhost:8080/api';
```

## Development

```bash
# Install new component (shadcn/ui)
npx shadcn@latest add <component-name>

# Format code
npm run format

# Lint
npm run lint
```

## Building for Production

```bash
# Build
npm run build

# Output in dist/ folder
# Serve with any static file server
```

## SEO & Multi-Platform Deployment

The app includes full SEO optimization for social sharing (Twitter/X, Facebook, LinkedIn) and mobile platforms (iOS, Android PWA).

### Environment Variables

Copy `.env.example` to `.env` and configure for your deployment:

```env
# Required: Your production URL (no trailing slash)
VITE_SITE_URL=https://trade.lynchz.dev

# Optional: Customize branding
VITE_SITE_NAME=Passive Income Ahh
VITE_SITE_DESCRIPTION=AI-powered cryptocurrency trading platform...

# Optional: Social media attribution
VITE_TWITTER_HANDLE=@YourHandle
VITE_FB_APP_ID=your-fb-app-id
```

### What's Included

| Feature | Files |
|---------|-------|
| **Open Graph** (Facebook, LinkedIn) | `index.html` |
| **Twitter/X Cards** | `index.html` |
| **iOS Web App** | `index.html`, touch icons |
| **Android PWA** | `manifest.json` |
| **Search Engines** | `robots.txt`, `sitemap.xml` |
| **Structured Data** | JSON-LD in `index.html` |

### Generated Assets (in `/public`)

- `og-image.png` - Social sharing preview (1200×630)
- `apple-touch-icon*.png` - iOS home screen icons
- `icon-*.png` - PWA icons (192, 512)
- `favicon-*.png` - Browser favicons (16, 32)

### Deployment Checklist

1. **Set your domain** in `client/.env`:
   ```env
   VITE_SITE_URL=https://your-domain.com
   ```

2. **Build the app**:
   ```bash
   npm run build
   ```

3. **Verify SEO files** in `dist/`:
   - Check `robots.txt` has correct sitemap URL
   - Check `sitemap.xml` has correct domain
   - Check `index.html` meta tags

4. **Deploy** the `dist/` folder to your server

### Testing Social Sharing

After deploying, test your meta tags:
- [Facebook Sharing Debugger](https://developers.facebook.com/tools/debug/)
- [Twitter Card Validator](https://cards-dev.twitter.com/validator)
- [LinkedIn Post Inspector](https://www.linkedin.com/post-inspector/)
