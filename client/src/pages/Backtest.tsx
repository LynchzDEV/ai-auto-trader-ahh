/* ===========================================================================
   QUORUM — Backtest
   "Prove it on history before it touches capital."

   Faithful port of redesign/secondary.jsx → Backtest(), wired to the REAL
   backend (/api/backtest/*). Preserves every feature of the previous page:
   run config (symbols / dates / capital / fees / leverage / strategy / AI
   model), live concierge model-sync, previous-runs list with select + delete,
   rich metric tiles, equity + drawdown charts, monthly returns, and a
   trade-return distribution. No mock data — empty states stand in until a run
   produces results.
   =========================================================================== */
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import {
  listBacktests,
  stopBacktest,
  startBacktest,
  getBacktestMetrics,
  getBacktestEquity,
  getBacktestTrades,
  deleteBacktest,
  getStrategies,
} from "../lib/api";
import { useConciergeAccounts } from "../hooks/useConciergeAccounts";
import { chartLink } from "../lib/deepLinks";
import { displaySymbol } from "../lib/symbol";
import { Ic } from "../quorum/icons";
import {
  AreaPlus,
  Underwater,
  MonthlyBars,
  ReturnHistogram,
  type MonthlyBar,
} from "../quorum/analytics-charts";
import { useConfirm, useAlert } from "@/components/ui/confirm-modal";

/* ----------------------------------------------------------------------------
   Backend contract (server/backtest/types.go + api/server.go)
   ---------------------------------------------------------------------------- */

interface BacktestConfig {
  run_id?: string;
  name?: string;
  symbols?: string[];
  timeframes?: string[];
  decision_timeframe?: string;
  initial_balance?: number;
  start_ts?: number; // unix milliseconds
  end_ts?: number; // unix milliseconds
  fee_bps?: number;
  slippage_bps?: number;
  btc_eth_leverage?: number;
  altcoin_leverage?: number;
}

interface BacktestRun {
  run_id: string;
  name?: string;
  status: string;
  config?: BacktestConfig;
  started_at?: string;
  completed_at?: string;
  current_equity?: number;
  progress?: number;
  error?: string;
}

interface BacktestMetrics {
  total_return: number;
  total_return_pct: number;
  max_drawdown: number;
  max_drawdown_pct: number;
  sharpe_ratio: number;
  sortino_ratio: number;
  win_rate: number;
  profit_factor: number;
  total_trades: number;
  winning_trades: number;
  losing_trades: number;
  avg_win: number;
  avg_loss: number;
  largest_win: number;
  largest_loss: number;
  avg_hold_time_hours: number;
  final_equity: number;
}

// GET /api/backtest/{id}/equity → { equity_curve: EquityPoint[] }
interface EquityPoint {
  timestamp: number; // unix milliseconds
  equity: number;
  available: number;
  pnl: number;
  pnl_pct: number;
  drawdown_pct: number; // ≤ 0
  cycle: number;
}

// GET /api/backtest/{id}/trades → { trades: TradeEvent[] }
interface TradeEvent {
  timestamp: number; // unix milliseconds
  symbol: string;
  action: string;
  side: string;
  quantity: number;
  price: number;
  fee: number;
  realized_pnl: number;
  leverage: number;
  cycle: number;
  note?: string;
}

interface StrategyLite {
  id: string;
  name: string;
}

// An AI model choice derived from a Quota Concierge account.
interface ModelOption {
  id: string; // account id — sent as ai_model
  label: string; // e.g. "Personal MAX (Claude)"
  provider: string; // account kind, e.g. "claude"
}

const CHART_SETUP_DRAFT_STORAGE_KEY = "ahh.chart.setupDraft";
const BACKTEST_TIMEFRAMES = ["1m", "5m", "15m", "30m", "1h", "4h", "1d"] as const;

interface ChartSetupDraft {
  symbol?: string;
  interval?: string;
  call?: string;
  headline?: string;
}

function readChartSetupDraft(): ChartSetupDraft | null {
  try {
    const raw = localStorage.getItem(CHART_SETUP_DRAFT_STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" ? (parsed as ChartSetupDraft) : null;
  } catch {
    return null;
  }
}

function isBacktestTimeframe(value: string): value is (typeof BACKTEST_TIMEFRAMES)[number] {
  return (BACKTEST_TIMEFRAMES as readonly string[]).includes(value);
}

/* ----------------------------------------------------------------------------
   Formatting helpers (local — keep the editorial mono look of the mockup)
   ---------------------------------------------------------------------------- */
const m$ = (n: number, d = 2) =>
  (n < 0 ? "-$" : "$") +
  Math.abs(n).toLocaleString("en-US", {
    minimumFractionDigits: d,
    maximumFractionDigits: d,
  });
const sg = (n: number, d = 2) =>
  (n >= 0 ? "+" : "−") + Math.abs(n).toFixed(d);

function fmtTs(ts?: number): string {
  if (!ts) return "—";
  const ms = ts > 1_000_000_000_000 ? ts : ts * 1000;
  return new Date(ms).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

function fmtRange(cfg?: BacktestConfig): string {
  if (!cfg?.start_ts || !cfg?.end_ts) return "—";
  return `${fmtTs(cfg.start_ts)} — ${fmtTs(cfg.end_ts)}`;
}

// Convert a yyyy-mm-dd date input to unix milliseconds (UTC midnight).
function dateToTs(d: string): number {
  if (!d) return 0;
  const ms = Date.parse(d + "T00:00:00Z");
  return Number.isFinite(ms) ? ms : 0;
}

// Bucket an equity curve into calendar-month % returns for the monthly bars.
function monthlyReturns(curve: EquityPoint[]): MonthlyBar[] {
  if (curve.length < 2) return [];
  const buckets = new Map<string, { first: number; last: number; label: string }>();
  for (const p of curve) {
    const d = new Date(p.timestamp); // backend timestamps are unix milliseconds
    const key = `${d.getUTCFullYear()}-${String(d.getUTCMonth()).padStart(2, "0")}`;
    const label = d.toLocaleDateString(undefined, { month: "short" });
    const b = buckets.get(key);
    if (b) b.last = p.equity;
    else buckets.set(key, { first: p.equity, last: p.equity, label });
  }
  return Array.from(buckets.values()).map((b) => ({
    m: b.label,
    pct: b.first ? Math.round((b.last / b.first - 1) * 1000) / 10 : 0,
  }));
}

/* ----------------------------------------------------------------------------
   Component
   ---------------------------------------------------------------------------- */
export default function Backtest() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const setupAppliedRef = useRef(false);
  const [runs, setRuns] = useState<BacktestRun[]>([]);
  const [selectedRun, setSelectedRun] = useState<string | null>(null);
  const [metrics, setMetrics] = useState<BacktestMetrics | null>(null);
  const [equityCurve, setEquityCurve] = useState<EquityPoint[]>([]);
  const [trades, setTrades] = useState<TradeEvent[]>([]);
  const [strategies, setStrategies] = useState<StrategyLite[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [setupSource, setSetupSource] = useState<string | null>(null);
  const { confirm, ConfirmDialog } = useConfirm();
  const { alert, AlertDialog } = useAlert();

  // AI model options sourced live from the Quota Concierge (Claude MAX
  // accounts). The hook polls + refreshes on focus/visibility/`concierge:changed`
  // so newly-added accounts appear without an app restart.
  const { accounts: conciergeAccounts, refresh: refreshModels } =
    useConciergeAccounts();
  const modelOptions = useMemo<ModelOption[]>(() => {
    const fallback: ModelOption[] = [
      { id: "claude", label: "Claude MAX", provider: "claude" },
    ];
    const options: ModelOption[] = conciergeAccounts
      .filter((a) => a && a.id)
      .map((a) => {
        const kind: string = a.kind || "claude";
        const kindLabel = kind.charAt(0).toUpperCase() + kind.slice(1);
        return {
          id: String(a.id),
          label: `${a.label || kindLabel} (${kindLabel})`,
          provider: kind,
        };
      });
    return options.length > 0 ? options : fallback;
  }, [conciergeAccounts]);

  // Run-config form state.
  const [form, setForm] = useState({
    name: "Conservative Trend",
    symbols: "BTCUSDT,ETHUSDT",
    timeframe: "15m",
    start_date: "",
    end_date: "",
    initial_capital: 3000,
    btc_eth_leverage: 10,
    altcoin_leverage: 20,
    fee_bps: 5,
    slippage_bps: 2,
    strategy_id: "",
    ai_model: "",
  });

  useEffect(() => {
    void loadData();
  }, []);

  // SPEC-26: Chart setup handoff. Review-only: prefill the form, but never run
  // a backtest by navigation. The user still chooses a date range and clicks Run.
  useEffect(() => {
    if (setupAppliedRef.current) return;
    const rawSymbol = searchParams.get("setup_symbol");
    if (!rawSymbol) return;
    const symbol = rawSymbol.trim().toUpperCase();
    if (!symbol) return;

    setupAppliedRef.current = true;
    const draft = readChartSetupDraft();
    const interval = (searchParams.get("setup_interval") || draft?.interval || "").trim();
    const label = `${displaySymbol(symbol)}${interval ? ` · ${interval}` : ""}`;
    const call = draft?.call ? ` · ${String(draft.call).replace(/_/g, " ")}` : "";

    setSetupSource(`Loaded from Chart setup: ${label}${call}`);
    setForm((f) => ({
      ...f,
      name: draft?.headline ? `Chart setup: ${draft.headline.slice(0, 80)}` : `Chart setup: ${label}`,
      symbols: symbol,
      timeframe: isBacktestTimeframe(interval) ? interval : f.timeframe,
    }));

    const next = new URLSearchParams(searchParams);
    next.delete("setup_symbol");
    next.delete("setup_interval");
    setSearchParams(next, { replace: true });
  }, [searchParams, setSearchParams]);

  // Default to the first available model, and keep the selection valid if the
  // currently-picked account disappears from the live list.
  useEffect(() => {
    setForm((f) =>
      modelOptions.some((m) => m.id === f.ai_model)
        ? f
        : { ...f, ai_model: modelOptions[0].id }
    );
  }, [modelOptions]);

  useEffect(() => {
    if (selectedRun) void loadRunData(selectedRun);
  }, [selectedRun]);

  const formSymbols = useMemo(
    () =>
      form.symbols
        .split(",")
        .map((s) => s.trim().toUpperCase())
        .filter(Boolean),
    [form.symbols]
  );
  const formStartTs = useMemo(() => dateToTs(form.start_date), [form.start_date]);
  const formEndTs = useMemo(() => dateToTs(form.end_date), [form.end_date]);
  const hasRunnableConfig =
    formSymbols.length > 0 &&
    formStartTs > 0 &&
    formEndTs > formStartTs &&
    Number(form.initial_capital) > 0 &&
    Number(form.fee_bps) >= 0 &&
    Number(form.fee_bps) <= 1000 &&
    Number(form.slippage_bps) >= 0 &&
    Number(form.slippage_bps) <= 1000 &&
    Number(form.btc_eth_leverage) >= 1 &&
    Number(form.btc_eth_leverage) <= 125 &&
    Number(form.altcoin_leverage) >= 1 &&
    Number(form.altcoin_leverage) <= 125;
  const canRunBacktest = hasRunnableConfig && !creating;
  const runDisabledTitle = hasRunnableConfig
    ? "Run backtest"
    : "Need ≥1 symbol, valid dates, capital > 0, fees 0–1000 bps, slippage 0–1000 bps, leverage 1–125×";

  const loadData = async () => {
    try {
      const [runsRes, strategiesRes] = await Promise.all([
        listBacktests().catch(() => ({ data: { backtests: [] } })),
        getStrategies().catch(() => ({ data: { strategies: [] } })),
      ]);
      const backtests: BacktestRun[] = runsRes.data.backtests || [];
      setRuns(backtests);
      setStrategies(strategiesRes.data.strategies || []);
      if (backtests.length > 0 && !selectedRun) {
        setSelectedRun(backtests[0].run_id);
      }
    } catch (err) {
      console.error("Failed to load data:", err);
    } finally {
      setLoading(false);
    }
  };

  const loadRunData = async (runId: string) => {
    try {
      const [metricsRes, equityRes, tradesRes] = await Promise.all([
        getBacktestMetrics(runId).catch(() => ({ data: null })),
        getBacktestEquity(runId).catch(() => ({ data: {} })),
        getBacktestTrades(runId).catch(() => ({ data: { trades: [] } })),
      ]);
      setMetrics(metricsRes.data as BacktestMetrics | null);
      // Backend returns { equity_curve: [...] }; tolerate a legacy { equity: [...] }.
      const eq =
        (equityRes.data as any)?.equity_curve ||
        (equityRes.data as any)?.equity ||
        [];
      setEquityCurve(eq as EquityPoint[]);
      setTrades(((tradesRes.data as any)?.trades || []) as TradeEvent[]);
    } catch (err) {
      console.error("Failed to load run data:", err);
    }
  };

  const handleStartBacktest = async () => {
    setError(null);
    if (formSymbols.length === 0) {
      setError("Enter at least one symbol before running a backtest.");
      return;
    }
    if (!formStartTs || !formEndTs || formEndTs <= formStartTs) {
      setError("Choose a valid start date and an end date after it.");
      return;
    }
    setCreating(true);
    try {
      // Map the form onto the real backtest.Config (snake_case JSON tags).
      const payload = {
        name: form.name,
        symbols: formSymbols,
        decision_timeframe: form.timeframe,
        timeframes: [form.timeframe],
        start_ts: formStartTs,
        end_ts: formEndTs,
        initial_balance: Number(form.initial_capital),
        fee_bps: Number(form.fee_bps),
        slippage_bps: Number(form.slippage_bps),
        btc_eth_leverage: Number(form.btc_eth_leverage),
        altcoin_leverage: Number(form.altcoin_leverage),
        // Preserved passthrough — the engine ignores unknown keys, but these
        // keep the strategy + model selection in the request for any consumer.
        strategy_id: form.strategy_id,
        ai_model: form.ai_model,
      };
      const res = await startBacktest(payload);
      const newId = res.data?.run_id;
      if (newId) setSelectedRun(newId);
      await loadData();
    } catch (err: any) {
      setError(err?.response?.data?.error || "Failed to start backtest");
    } finally {
      setCreating(false);
    }
  };

  const handleDeleteBacktest = async (runId: string) => {
    const ok = await confirm({
      title: "Delete backtest run",
      description: "Delete this backtest run and its results? This cannot be undone.",
      confirmText: "Delete",
      variant: "danger",
    });
    if (!ok) return;
    try {
      await deleteBacktest(runId);
      if (selectedRun === runId) {
        setSelectedRun(null);
        setMetrics(null);
        setEquityCurve([]);
        setTrades([]);
      }
      await loadData();
    } catch (err: any) {
      alert({
        title: "Error",
        description: err.response?.data?.error || "Failed to delete backtest",
        variant: "danger",
      });
    }
  };

  // Stop a running backtest (wires the existing /backtest/{id}/stop route).
  const handleStopBacktest = async (runId: string) => {
    const ok = await confirm({
      title: "Stop backtest run",
      description: "Stop this running backtest? Any partial results may be incomplete.",
      confirmText: "Stop",
      variant: "warning",
    });
    if (!ok) return;
    try {
      await stopBacktest(runId);
      await loadData();
    } catch (err: any) {
      alert({
        title: "Error",
        description: err.response?.data?.error || "Failed to stop backtest",
        variant: "danger",
      });
    }
  };

  // While any run is in progress, poll so its status + progress meter advance without a
  // manual refresh. Stops polling once nothing is running.
  const anyRunning = runs.some((r) => r.status === "running");
  useEffect(() => {
    if (!anyRunning) return;
    const t = setInterval(() => {
      void loadData();
    }, 4000);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [anyRunning]);

  const currentRun = runs.find((r) => r.run_id === selectedRun) || null;

  /* ---- Derived view-model ---- */
  const equitySeries = useMemo(
    () => equityCurve.map((p) => p.equity),
    [equityCurve]
  );
  const drawdownSeries = useMemo(
    () => equityCurve.map((p) => p.drawdown_pct),
    [equityCurve]
  );
  const monthly = useMemo(() => monthlyReturns(equityCurve), [equityCurve]);
  // Trade returns as % of starting capital — the honest proxy available from
  // execution events (the backend exposes realized_pnl, not per-trade entry/exit).
  const tradeReturns = useMemo(() => {
    const base = currentRun?.config?.initial_balance || form.initial_capital || 1;
    return trades
      .filter((t) => t.realized_pnl !== 0)
      .map((t) => Math.round((t.realized_pnl / base) * 1000) / 10);
  }, [trades, currentRun, form.initial_capital]);
  const closedCount = tradeReturns.length;

  const minDrawdown = drawdownSeries.length ? Math.min(...drawdownSeries) : 0;

  // avgRR and expectancy are derived from the metric primitives the backend
  // provides (it doesn't ship avg_rr / expectancy / exposure directly).
  const avgRR = useMemo(() => {
    if (!metrics) return 0;
    const loss = Math.abs(metrics.avg_loss);
    return loss > 0 ? metrics.avg_win / loss : 0;
  }, [metrics]);
  const expectancyR = useMemo(() => {
    if (!metrics) return 0;
    const loss = Math.abs(metrics.avg_loss);
    if (loss <= 0) return 0;
    const wr = metrics.win_rate / 100;
    const expDollars = wr * metrics.avg_win - (1 - wr) * loss;
    return expDollars / loss; // expressed in R
  }, [metrics]);

  const hasResults = !!metrics && equityCurve.length > 0;

  // Metric tiles — every metric the task calls for: return, sharpe, sortino,
  // maxDD, winRate, profitFactor, trades, avgRR, expectancy (+ avg hold to
  // round out the grid-4 to a clean 4+4+2). avgRR / expectancy are derived;
  // the rest come straight off the backend Metrics struct.
  const tiles: Array<[string, string, string]> = metrics
    ? [
        ["Net return", sg(metrics.total_return_pct) + "%", metrics.total_return_pct >= 0 ? "var(--up)" : "var(--down)"],
        ["Sharpe", metrics.sharpe_ratio.toFixed(2), "var(--text)"],
        ["Sortino", metrics.sortino_ratio.toFixed(2), "var(--text)"],
        ["Max drawdown", "−" + Math.abs(metrics.max_drawdown_pct).toFixed(1) + "%", "var(--down)"],
        ["Win rate", metrics.win_rate.toFixed(0) + "%", "var(--text)"],
        ["Profit factor", metrics.profit_factor.toFixed(2), "var(--text)"],
        ["Trades", String(metrics.total_trades), "var(--text)"],
        ["Avg R:R", avgRR.toFixed(2), "var(--accent-2)"],
        ["Expectancy", sg(expectancyR, 2) + "R", expectancyR >= 0 ? "var(--up)" : "var(--down)"],
        ["Avg hold", metrics.avg_hold_time_hours > 0 ? metrics.avg_hold_time_hours.toFixed(1) + "h" : "—", "var(--text)"],
      ]
    : [];

  // Run-configuration sidebar rows (echo the selected run, else the live form).
  const cfg = currentRun?.config;
  const configRows: Array<[string, string]> = [
    ["Strategy", currentRun?.name || cfg?.name || form.name],
    ["Symbols", (cfg?.symbols || form.symbols.split(",").map((s) => s.trim())).map(displaySymbol).join(" · ")],
    ["Date range", cfg ? fmtRange(cfg) : form.start_date && form.end_date ? `${form.start_date} — ${form.end_date}` : "—"],
    ["Timeframe", cfg?.decision_timeframe || form.timeframe],
    ["Starting capital", m$(cfg?.initial_balance ?? form.initial_capital, 0)],
    ["Leverage", `${cfg?.btc_eth_leverage ?? form.btc_eth_leverage}× BTC/ETH · ${cfg?.altcoin_leverage ?? form.altcoin_leverage}× alt`],
    ["Fees", `${((cfg?.fee_bps ?? form.fee_bps) / 100).toFixed(2)}% taker · ${((cfg?.slippage_bps ?? form.slippage_bps) / 100).toFixed(2)}% slip`],
  ];

  const set = <K extends keyof typeof form>(k: K, v: (typeof form)[K]) =>
    setForm((f) => ({ ...f, [k]: v }));

  const inputStyle: React.CSSProperties = {
    width: "100%",
    fontFamily: "var(--font-mono)",
    fontSize: 12.5,
    padding: "8px 11px",
    border: "1px solid var(--line)",
    borderRadius: 7,
    background: "var(--surface-2)",
    color: "var(--text)",
    outline: "none",
  };

  /* ---- Loading skeleton ---- */
  if (loading) {
    return (
      <div className="page">
        <div className="page-head">
          <div>
            <h1 className="page-title">Backtest</h1>
            <p className="page-desc">Prove it on history before it touches capital</p>
          </div>
        </div>
        <div className="panel panel-pad" style={{ textAlign: "center", color: "var(--text-3)" }}>
          <span className="row" style={{ justifyContent: "center", gap: 10 }}>
            <span className="dot live" />
            Loading backtests…
          </span>
        </div>
      </div>
    );
  }

  return (
    <div className="page fade-in">
      <div className="page-head">
        <div>
          <h1 className="page-title">Backtest</h1>
          <p className="page-desc">Prove it on history before it touches capital</p>
        </div>
        <div className="row" style={{ gap: 8 }}>
          <button className="btn icon" onClick={() => void loadData()} title="Refresh">
            <Ic name="refresh" size={15} />
          </button>
          <button
            className="btn primary"
            onClick={() => void handleStartBacktest()}
            disabled={!canRunBacktest}
            title={runDisabledTitle}
          >
            <Ic name="play" size={14} />
            {creating ? "Running…" : "Run backtest"}
          </button>
        </div>
      </div>

      {error && (
        <div
          className="panel panel-pad"
          style={{
            marginBottom: 16,
            borderColor: "rgba(224,98,94,.4)",
            color: "var(--down)",
            fontSize: 13,
          }}
        >
          {error}
        </div>
      )}

      <div style={{ display: "grid", gridTemplateColumns: "1fr 286px", gap: 16, alignItems: "start" }}>
        {/* ============ LEFT: results ============ */}
        <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
          {/* Equity vs buy & hold + drawdown */}
          <div className="panel panel-pad">
            <div
              className="row"
              style={{ justifyContent: "space-between", alignItems: "flex-start", marginBottom: 16, flexWrap: "wrap", gap: 12 }}
            >
              <div>
                <div className="serif" style={{ fontSize: 22, fontWeight: 600 }}>
                  {currentRun?.name || cfg?.name || form.name || "Backtest"}
                </div>
                <div className="faint" style={{ fontSize: 12.5 }}>
                  {cfg
                    ? `${fmtRange(cfg)} · ${(cfg.symbols || []).map(displaySymbol).join(" · ")} · ${cfg.decision_timeframe || ""}`
                    : "Configure a run, then prove it on history"}
                </div>
              </div>
              {metrics && (
                <div className="row" style={{ gap: 22 }}>
                  <div style={{ textAlign: "right" }}>
                    <div
                      className="mono"
                      style={{
                        fontSize: 27,
                        fontWeight: 700,
                        color: metrics.total_return_pct >= 0 ? "var(--up)" : "var(--down)",
                      }}
                    >
                      {sg(metrics.total_return_pct) + "%"}
                    </div>
                    <div className="eyebrow">strategy</div>
                  </div>
                  <div style={{ textAlign: "right" }}>
                    <div className="mono" style={{ fontSize: 27, color: "var(--text)" }}>
                      {m$(metrics.final_equity, 0)}
                    </div>
                    <div className="eyebrow">final equity</div>
                  </div>
                </div>
              )}
            </div>

            {/* legend */}
            <div className="row" style={{ gap: 18, marginBottom: 12 }}>
              <span className="row" style={{ gap: 6, fontSize: 12 }}>
                <span style={{ width: 16, height: 2, background: "var(--accent)", display: "inline-block" }} />
                Equity
              </span>
              <span className="row" style={{ gap: 6, fontSize: 12, color: "var(--text-3)" }}>
                <span style={{ width: 16, borderTop: "1px dashed var(--line-strong)", display: "inline-block" }} />
                {minDrawdown < 0 ? `peak-to-trough ${minDrawdown.toFixed(1)}%` : "drawdown below"}
              </span>
            </div>

            {hasResults ? (
              <>
                <AreaPlus
                  data={equitySeries}
                  height={228}
                  color="var(--accent)"
                  fmt={(v) => m$(v)}
                  labelFor={(i) =>
                    equityCurve[i] ? new Date(equityCurve[i].timestamp).toLocaleDateString() : ""
                  }
                />
                <div className="eyebrow" style={{ margin: "16px 0 6px" }}>
                  Drawdown
                </div>
                <Underwater data={drawdownSeries} height={62} />
              </>
            ) : (
              <div
                style={{
                  height: 300,
                  display: "flex",
                  flexDirection: "column",
                  alignItems: "center",
                  justifyContent: "center",
                  gap: 10,
                  color: "var(--text-3)",
                  border: "1px dashed var(--line)",
                  borderRadius: "var(--r)",
                }}
              >
                <Ic name="backtest" size={26} />
                <div style={{ fontSize: 13 }}>
                  {currentRun?.status === "running"
                    ? "Backtest running… results will appear as it completes"
                    : "No results yet — run a backtest or pick a previous run"}
                </div>
              </div>
            )}
          </div>

          {/* metric tiles */}
          {metrics ? (
            <div className="grid-4">
              {tiles.map((c, i) => (
                <div key={i} className="stat">
                  <span className="label">{c[0]}</span>
                  <div className="value" style={{ fontSize: 20, color: c[2] }}>
                    {c[1]}
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="grid-4">
              {["Net return", "Sharpe", "Sortino", "Max drawdown", "Win rate", "Profit factor", "Trades", "Avg R:R", "Expectancy", "Avg hold"].map(
                (label, i) => (
                  <div key={i} className="stat">
                    <span className="label">{label}</span>
                    <div className="value" style={{ fontSize: 20, color: "var(--text-3)" }}>
                      &mdash;
                    </div>
                  </div>
                )
              )}
            </div>
          )}

          {/* monthly + distribution */}
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }}>
            <div className="panel" style={{ overflow: "hidden" }}>
              <div className="panel-head">
                <h3>
                  <Ic name="ranking" />
                  Monthly returns
                </h3>
              </div>
              <div className="panel-pad">
                {monthly.length ? (
                  <MonthlyBars data={monthly} />
                ) : (
                  <div className="faint" style={{ fontSize: 12.5, padding: "18px 0", textAlign: "center" }}>
                    No monthly data yet
                  </div>
                )}
              </div>
            </div>
            <div className="panel" style={{ overflow: "hidden" }}>
              <div className="panel-head">
                <h3>
                  <Ic name="backtest" />
                  Trade distribution
                </h3>
                <span className="sub">{closedCount} trades</span>
              </div>
              <div className="panel-pad">
                {tradeReturns.length ? (
                  <ReturnHistogram data={tradeReturns} />
                ) : (
                  <div className="faint" style={{ fontSize: 12.5, padding: "18px 0", textAlign: "center" }}>
                    No closed trades yet
                  </div>
                )}
              </div>
            </div>
          </div>

          {/* previous runs */}
          <div className="panel" style={{ overflow: "hidden" }}>
            <div className="panel-head">
              <h3>
                <Ic name="history" />
                Previous runs
              </h3>
              <span className="sub">{runs.length} total</span>
            </div>
            {runs.length === 0 ? (
              <div className="panel-pad faint" style={{ fontSize: 13, textAlign: "center" }}>
                No backtests yet
              </div>
            ) : (
              <div>
                {runs.map((run, i) => {
                  const active = selectedRun === run.run_id;
                  const runSymbols = run.config?.symbols || [];
                  const runInterval = run.config?.decision_timeframe || "1h";
                  const statusColor =
                    run.status === "completed"
                      ? "var(--up)"
                      : run.status === "running"
                      ? "var(--warn)"
                      : run.status === "failed"
                      ? "var(--down)"
                      : "var(--text-3)";
                  return (
                    <div
                      key={run.run_id}
                      onClick={() => setSelectedRun(run.run_id)}
                      style={{
                        display: "flex",
                        alignItems: "center",
                        gap: 14,
                        padding: "12px 18px",
                        borderBottom: i < runs.length - 1 ? "1px solid var(--line)" : "none",
                        cursor: "pointer",
                        background: active ? "var(--surface-2)" : "transparent",
                        borderLeft: active ? "2px solid var(--accent)" : "2px solid transparent",
                      }}
                    >
                      <span
                        className="badge"
                        style={{ color: statusColor, borderColor: "var(--line-2)", flex: "none" }}
                      >
                        {run.status === "running" && (
                          <span className="dot live" style={{ width: 5, height: 5 }} />
                        )}
                        {run.status}
                      </span>
                      <div style={{ flex: 1, minWidth: 0 }}>
                        <div className="sym mono row" style={{ gap: 4, fontWeight: 600, fontSize: 13, flexWrap: "wrap" }}>
                          {runSymbols.length > 0
                            ? runSymbols.map((sym, idx) => (
                                <span key={sym} className="row" style={{ gap: 4 }}>
                                  {idx > 0 && <span>,</span>}
                                  <button
                                    type="button"
                                    className="sym mono"
                                    title={`Open ${displaySymbol(sym)} chart`}
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      navigate(chartLink(sym, runInterval));
                                    }}
                                    style={{
                                      background: "transparent",
                                      border: 0,
                                      padding: 0,
                                      color: "inherit",
                                      cursor: "pointer",
                                      font: "inherit",
                                      fontWeight: 600,
                                    }}
                                  >
                                    {displaySymbol(sym)}
                                  </button>
                                </span>
                              ))
                            : run.name || "Backtest"}
                        </div>
                        <div className="faint" style={{ fontSize: 11 }}>
                          {fmtRange(run.config)}
                          {run.error ? ` · ${run.error}` : ""}
                        </div>
                      </div>
                      <span className="mono faint" style={{ fontSize: 12 }}>
                        {m$(run.config?.initial_balance ?? 0, 0)}
                      </span>
                      {typeof run.progress === "number" && run.status === "running" && (
                        <div className="meter" style={{ width: 70 }}>
                          <span style={{ width: `${Math.round((run.progress || 0) * 100)}%` }} />
                        </div>
                      )}
                      {run.status === "running" && (
                        <button
                          className="btn ghost icon"
                          title="Stop run"
                          onClick={(e) => {
                            e.stopPropagation();
                            void handleStopBacktest(run.run_id);
                          }}
                        >
                          <Ic name="stop" size={14} />
                        </button>
                      )}
                      <button
                        className="btn ghost icon"
                        title="Delete run"
                        onClick={(e) => {
                          e.stopPropagation();
                          void handleDeleteBacktest(run.run_id);
                        }}
                      >
                        <Ic name="x" size={14} />
                      </button>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </div>

        {/* ============ RIGHT: run configuration ============ */}
        <div className="panel" style={{ overflow: "hidden", position: "sticky", top: 76 }}>
          <div className="panel-head">
            <h3>
                <Ic name="config" />
                Run configuration
              </h3>
            </div>
          <div className="panel-pad" style={{ display: "flex", flexDirection: "column", gap: 11 }}>
            {setupSource && (
              <div
                className="row"
                style={{
                  alignItems: "flex-start",
                  gap: 9,
                  padding: "10px 12px",
                  borderRadius: "var(--r)",
                  border: "1px solid var(--accent-line)",
                  background: "var(--accent-dim)",
                  color: "var(--text-2)",
                  fontSize: 12,
                  lineHeight: 1.45,
                }}
              >
                <Ic name="chart" size={15} style={{ color: "var(--accent)", flex: "none", marginTop: 1 }} />
                <span>
                  <strong style={{ color: "var(--text)" }}>{setupSource}</strong>
                  {" "}This only pre-fills the backtest form; it does not start a run.
                </span>
              </div>
            )}

            {/* Strategy name */}
            <div>
              <div className="eyebrow" style={{ marginBottom: 5 }}>
                Strategy name
              </div>
              <input
                style={inputStyle}
                value={form.name}
                onChange={(e) => set("name", e.target.value)}
                placeholder="Conservative Trend"
              />
            </div>

            {/* Symbols */}
            <div>
              <div className="eyebrow" style={{ marginBottom: 5 }}>
                Symbols
              </div>
              <input
                style={inputStyle}
                value={form.symbols}
                onChange={(e) => set("symbols", e.target.value)}
                placeholder="BTCUSDT, ETHUSDT, SOLUSDT"
              />
            </div>

            {/* Date range */}
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
              <div>
                <div className="eyebrow" style={{ marginBottom: 5 }}>
                  Start
                </div>
                <input
                  type="date"
                  style={inputStyle}
                  value={form.start_date}
                  onChange={(e) => set("start_date", e.target.value)}
                />
              </div>
              <div>
                <div className="eyebrow" style={{ marginBottom: 5 }}>
                  End
                </div>
                <input
                  type="date"
                  style={inputStyle}
                  value={form.end_date}
                  onChange={(e) => set("end_date", e.target.value)}
                />
              </div>
            </div>

            {/* Timeframe + capital */}
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
              <div>
                <div className="eyebrow" style={{ marginBottom: 5 }}>
                  Timeframe
                </div>
                <select
                  style={inputStyle}
                  value={form.timeframe}
                  onChange={(e) => set("timeframe", e.target.value)}
                >
                  {BACKTEST_TIMEFRAMES.map((tf) => (
                    <option key={tf} value={tf}>
                      {tf}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <div className="eyebrow" style={{ marginBottom: 5 }}>
                  Capital ($)
                </div>
                <input
                  type="number"
                  style={inputStyle}
                  value={form.initial_capital}
                  onChange={(e) => set("initial_capital", Number(e.target.value))}
                />
              </div>
            </div>

            {/* Leverage */}
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
              <div>
                <div className="eyebrow" style={{ marginBottom: 5 }}>
                  BTC/ETH lev
                </div>
                <input
                  type="number"
                  style={inputStyle}
                  value={form.btc_eth_leverage}
                  onChange={(e) => set("btc_eth_leverage", Number(e.target.value))}
                />
              </div>
              <div>
                <div className="eyebrow" style={{ marginBottom: 5 }}>
                  Altcoin lev
                </div>
                <input
                  type="number"
                  style={inputStyle}
                  value={form.altcoin_leverage}
                  onChange={(e) => set("altcoin_leverage", Number(e.target.value))}
                />
              </div>
            </div>

            {/* Fees + slippage (basis points) */}
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
              <div>
                <div className="eyebrow" style={{ marginBottom: 5 }}>
                  Taker fee (bps)
                </div>
                <input
                  type="number"
                  style={inputStyle}
                  value={form.fee_bps}
                  onChange={(e) => set("fee_bps", Number(e.target.value))}
                />
              </div>
              <div>
                <div className="eyebrow" style={{ marginBottom: 5 }}>
                  Slippage (bps)
                </div>
                <input
                  type="number"
                  style={inputStyle}
                  value={form.slippage_bps}
                  onChange={(e) => set("slippage_bps", Number(e.target.value))}
                />
              </div>
            </div>

            {/* Strategy preset */}
            {strategies.length > 0 && (
              <div>
                <div className="eyebrow" style={{ marginBottom: 5 }}>
                  Strategy preset{" "}
                  <span className="faint" style={{ textTransform: "none" }}>· not applied yet</span>
                </div>
                <select
                  style={inputStyle}
                  value={form.strategy_id}
                  onChange={(e) => set("strategy_id", e.target.value)}
                >
                  <option value="">None</option>
                  {strategies.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </select>
              </div>
            )}

            {/* AI model — live concierge sync */}
            <div>
              <div className="eyebrow" style={{ marginBottom: 5 }}>
                AI model{" "}
                <span className="faint" style={{ textTransform: "none" }}>· not applied yet</span>
              </div>
              <select
                style={inputStyle}
                value={form.ai_model}
                onChange={(e) => set("ai_model", e.target.value)}
                onFocus={() => refreshModels()}
                disabled={modelOptions.length === 0}
              >
                {modelOptions.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.label}
                  </option>
                ))}
              </select>
            </div>

            <button
              className="btn primary"
              style={{ justifyContent: "center", marginTop: 4 }}
              onClick={() => void handleStartBacktest()}
              disabled={!canRunBacktest}
              title={runDisabledTitle}
            >
              <Ic name="play" size={14} />
              {creating ? "Running…" : "Run backtest"}
            </button>

            {/* Echo of the active run's resolved configuration. */}
            <div style={{ borderTop: "1px solid var(--line)", margin: "4px 0 0", paddingTop: 12, display: "flex", flexDirection: "column", gap: 9 }}>
              <div className="eyebrow">Active run</div>
              {configRows.map((c, i) => (
                <div key={i} className="row" style={{ justifyContent: "space-between", gap: 12 }}>
                  <span className="faint" style={{ fontSize: 11.5 }}>
                    {c[0]}
                  </span>
                  <span className="mono" style={{ fontSize: 11.5, textAlign: "right", color: "var(--text-2)" }}>
                    {c[1]}
                  </span>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
      {ConfirmDialog}
      {AlertDialog}
    </div>
  );
}
