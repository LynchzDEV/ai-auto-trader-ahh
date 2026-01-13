import { useEffect, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
  getTraders,
  getStatus,
  getPositions,
  getAccount,
  startTrader,
  stopTrader,
} from "../lib/api";
import type { Trader, Position } from "../types";
import {
  Play,
  Square,
  RefreshCw,
  TrendingUp,
  TrendingDown,
  Activity,
  Wallet,
  DollarSign,
  Target,
  Zap,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { GlassCard } from "@/components/ui/glass-card";
import { GlowBadge } from "@/components/ui/glow-badge";
import { StatCard, MiniStat } from "@/components/ui/stat-card";
import { SpotlightCard } from "@/components/ui/spotlight-card";
import { useAlert } from "@/components/ui/confirm-modal";
import { MobileCardTable } from "@/components/ui/mobile-card-table";

interface AccountInfo {
  wallet_balance: number;
  total_equity: number;
  available: number;
  unrealized_pnl: number;
}

export default function Dashboard() {
  const [traders, setTraders] = useState<Trader[]>([]);
  const [selectedTrader, setSelectedTrader] = useState<string | null>(null);
  const [status, setStatus] = useState<any>(null);
  const [positions, setPositions] = useState<Position[]>([]);
  const [account, setAccount] = useState<AccountInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const { alert, AlertDialog } = useAlert();

  useEffect(() => {
    loadTraders();
  }, []);

  useEffect(() => {
    if (selectedTrader) {
      loadTraderData();
      const interval = setInterval(loadTraderData, 10000);
      return () => clearInterval(interval);
    }
  }, [selectedTrader]);

  const loadTraders = async () => {
    try {
      const res = await getTraders();
      const savedOrder = JSON.parse(
        localStorage.getItem("trader_order") || "[]"
      );
      let loadedTraders = res.data.traders || [];

      if (savedOrder.length > 0) {
        loadedTraders = loadedTraders.sort((a: Trader, b: Trader) => {
          const indexA = savedOrder.indexOf(a.id);
          const indexB = savedOrder.indexOf(b.id);
          if (indexA === -1 && indexB === -1) return 0;
          if (indexA === -1) return 1;
          if (indexB === -1) return -1;
          return indexA - indexB;
        });
      }
      setTraders(loadedTraders);
      if (res.data.traders?.length > 0 && !selectedTrader) {
        setSelectedTrader(res.data.traders[0].id);
      }
    } catch (err) {
      console.error("Failed to load traders:", err);
    } finally {
      setLoading(false);
    }
  };

  const loadTraderData = async () => {
    if (!selectedTrader) return;
    setRefreshing(true);
    try {
      const [statusRes, positionsRes, accountRes] = await Promise.all([
        getStatus(selectedTrader),
        getPositions(selectedTrader),
        getAccount(selectedTrader).catch(() => ({ data: null })),
      ]);
      setStatus(statusRes.data);
      setPositions(positionsRes.data.positions || []);
      setAccount(accountRes.data);
    } catch (err) {
      console.error("Failed to load trader data:", err);
    } finally {
      setRefreshing(false);
    }
  };

  const handleStart = async (id: string) => {
    try {
      await startTrader(id);
      loadTraders();
      loadTraderData();
    } catch (err: any) {
      alert({
        title: "Error",
        description: err.response?.data?.error || "Failed to start trader",
        variant: "danger",
      });
    }
  };

  const handleStop = async (id: string) => {
    try {
      await stopTrader(id);
      loadTraders();
      loadTraderData();
    } catch (err) {
      console.error("Failed to stop trader:", err);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center h-screen">
        <div className="flex flex-col items-center gap-4">
          <div className="relative w-16 h-16 flex items-center justify-center">
            <motion.div
              className="absolute inset-0 border-4 border-primary/20 rounded-full"
              animate={{ opacity: [0.3, 0.8, 0.3] }}
              transition={{
                duration: 1.5,
                repeat: Infinity,
                ease: "easeInOut",
              }}
            />
            <motion.div
              className="w-8 h-8 bg-primary/20 rounded-lg flex items-center justify-center"
              animate={{ opacity: [0.5, 1, 0.5] }}
              transition={{
                duration: 1.5,
                repeat: Infinity,
                ease: "easeInOut",
              }}
            >
              <div className="w-4 h-4 bg-primary rounded" />
            </motion.div>
          </div>
          <span className="text-muted-foreground">Loading dashboard...</span>
        </div>
      </div>
    );
  }

  const totalPnL = positions.reduce((sum, p) => sum + p.pnl, 0);
  // Calculate weighted average PnL% based on position notional values
  const totalNotional = positions.reduce(
    (sum, p) => sum + Math.abs(p.amount * p.entry_price),
    0
  );
  const totalPnLPercent =
    totalNotional > 0
      ? positions.reduce((sum, p) => {
          const notional = Math.abs(p.amount * p.entry_price);
          return sum + p.pnl_percent * notional;
        }, 0) / totalNotional
      : 0;

  return (
    <div className="p-4 lg:p-6 space-y-4 lg:space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-3">
        <motion.div
          initial={{ opacity: 0, x: -20 }}
          animate={{ opacity: 1, x: 0 }}
        >
          <h1 className="text-2xl lg:text-3xl font-bold text-gradient">
            Dashboard
          </h1>
          <p className="text-sm lg:text-base text-muted-foreground">
            Monitor your AI trading bots in real-time
          </p>
        </motion.div>
        <motion.div
          initial={{ opacity: 0, x: 20 }}
          animate={{ opacity: 1, x: 0 }}
          className="flex items-center gap-3"
        >
          {selectedTrader && status?.running && (
            <GlowBadge variant="success" glow pulse dot>
              Live Trading
            </GlowBadge>
          )}
          <Button
            variant="outline"
            size="icon"
            onClick={loadTraderData}
            disabled={refreshing}
            className="glass"
          >
            <RefreshCw
              className={`h-4 w-4 transition-opacity ${
                refreshing ? "opacity-50" : ""
              }`}
            />
          </Button>
        </motion.div>
      </div>

      {/* Stats Cards */}
      {selectedTrader && (
        <div className="grid gap-3 lg:gap-4 grid-cols-2 lg:grid-cols-4">
          <StatCard
            title="Account Balance"
            value={account?.wallet_balance || 0}
            icon={DollarSign}
            prefix="$"
            decimals={2}
            delay={0}
          />
          <StatCard
            title="Equity"
            value={account?.total_equity || 0}
            icon={Wallet}
            prefix="$"
            decimals={2}
            delay={1}
          />
          <StatCard
            title="Unrealized PnL"
            value={totalPnL}
            icon={totalPnL >= 0 ? TrendingUp : TrendingDown}
            prefix="$"
            decimals={2}
            colorize
            change={totalPnLPercent}
            changeLabel="avg"
            delay={2}
          />
          <StatCard
            title="Active Positions"
            value={positions.length}
            icon={Target}
            decimals={0}
            delay={3}
          />
        </div>
      )}

      <div className="grid gap-4 lg:gap-6 lg:grid-cols-3">
        {/* Traders List */}
        <GlassCard className="lg:col-span-1 p-0 overflow-hidden">
          <div className="p-4 border-b border-white/5">
            <h2 className="font-semibold flex items-center gap-2">
              <Zap className="w-4 h-4 text-primary" />
              Trading Bots
            </h2>
            <p className="text-sm text-muted-foreground">
              Select a bot to monitor
            </p>
          </div>

          {traders.length === 0 ? (
            <div className="p-6 text-center">
              <p className="text-muted-foreground text-sm">
                No traders configured. Go to Config to create one.
              </p>
            </div>
          ) : (
            <ScrollArea className="h-[280px] lg:h-[400px]">
              <div className="p-4 space-y-2">
                <AnimatePresence>
                  {traders.map((trader, index) => (
                    <motion.div
                      key={trader.id}
                      initial={{ opacity: 0, x: -20 }}
                      animate={{ opacity: 1, x: 0 }}
                      transition={{ delay: index * 0.05 }}
                      onClick={() => setSelectedTrader(trader.id)}
                      className={`group cursor-pointer p-4 rounded-xl transition-all duration-200 ${
                        selectedTrader === trader.id
                          ? "bg-primary/20 border border-primary/30"
                          : "bg-white/5 hover:bg-white/10 border border-transparent"
                      }`}
                    >
                      <div className="flex items-center justify-between mb-2">
                        <span className="font-medium">{trader.name}</span>
                        <GlowBadge
                          variant={
                            trader.status === "running"
                              ? "success"
                              : "secondary"
                          }
                          dot={trader.status === "running"}
                          pulse={trader.status === "running"}
                        >
                          {trader.status === "running" ? "Running" : "Stopped"}
                        </GlowBadge>
                      </div>

                      <div className="flex items-center justify-between">
                        <span className="text-xs text-muted-foreground">
                          {trader.exchange || "binance"}
                        </span>
                        <div className="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                          {trader.status === "running" ? (
                            <Button
                              size="icon"
                              variant="destructive"
                              className="h-7 w-7"
                              onClick={(e) => {
                                e.stopPropagation();
                                handleStop(trader.id);
                              }}
                            >
                              <Square className="h-3 w-3" />
                            </Button>
                          ) : (
                            <Button
                              size="icon"
                              className="h-7 w-7 bg-green-600 hover:bg-green-500"
                              onClick={(e) => {
                                e.stopPropagation();
                                handleStart(trader.id);
                              }}
                            >
                              <Play className="h-3 w-3" />
                            </Button>
                          )}
                        </div>
                      </div>
                    </motion.div>
                  ))}
                </AnimatePresence>
              </div>
            </ScrollArea>
          )}
        </GlassCard>

        {/* Main Content */}
        <div className="lg:col-span-2 space-y-6">
          {/* Positions Table */}
          <GlassCard className="p-0 overflow-hidden">
            <div className="p-4 border-b border-white/5 flex items-center justify-between">
              <div>
                <h2 className="font-semibold flex items-center gap-2">
                  <Target className="w-4 h-4 text-primary" />
                  Open Positions
                </h2>
                <p className="text-sm text-muted-foreground">
                  {positions.length} active position
                  {positions.length !== 1 ? "s" : ""}
                </p>
              </div>
              {positions.length > 0 && (
                <div className="flex items-center gap-4">
                  <MiniStat
                    label="Total PnL"
                    value={`$${totalPnL.toFixed(2)}`}
                    colorize
                  />
                </div>
              )}
            </div>

            <div className="p-4 lg:p-0">
              <MobileCardTable<Position>
                data={positions}
                keyExtractor={(pos, i) => `${pos.symbol}-${i}`}
                columns={[
                  {
                    key: "symbol",
                    label: "Symbol",
                    primary: true,
                    render: (v) => <span className="font-medium">{v}</span>,
                  },
                  {
                    key: "side",
                    label: "Side",
                    primary: true,
                    render: (v) => (
                      <GlowBadge variant={v === "LONG" ? "success" : "danger"}>
                        {v}
                      </GlowBadge>
                    ),
                  },
                  {
                    key: "pnl",
                    label: "PnL",
                    primary: true,
                    align: "right",
                    render: (v, pos) => (
                      <span
                        className={`font-mono font-medium ${
                          v >= 0 ? "text-green-400" : "text-red-400"
                        }`}
                      >
                        ${v.toFixed(2)} ({pos.pnl_percent >= 0 ? "+" : ""}
                        {pos.pnl_percent.toFixed(2)}%)
                      </span>
                    ),
                  },
                  {
                    key: "amount",
                    label: "Size",
                    align: "right",
                    render: (v) => (
                      <span className="font-mono">
                        {Math.abs(v).toFixed(4)}
                      </span>
                    ),
                  },
                  {
                    key: "entry_price",
                    label: "Entry",
                    align: "right",
                    render: (v) => (
                      <span className="font-mono">${v.toFixed(2)}</span>
                    ),
                  },
                  {
                    key: "mark_price",
                    label: "Mark",
                    align: "right",
                    render: (v) => (
                      <span className="font-mono">${v.toFixed(2)}</span>
                    ),
                  },
                ]}
                emptyState={
                  <div className="p-12 text-center">
                    <Activity className="w-12 h-12 text-muted-foreground/30 mx-auto mb-4" />
                    <p className="text-muted-foreground">No open positions</p>
                    <p className="text-sm text-muted-foreground/60">
                      Positions will appear here when the AI opens trades
                    </p>
                  </div>
                }
              />
            </div>
          </GlassCard>

          {/* AI Decisions */}
          <GlassCard className="p-0 overflow-hidden">
            <div className="p-4 border-b border-white/5 flex flex-col sm:flex-row sm:items-center justify-between gap-4">
              <div>
                <h2 className="font-semibold flex items-center gap-2">
                  <Activity className="w-4 h-4 text-primary" />
                  AI Decisions
                </h2>
                <p className="text-sm text-muted-foreground">
                  Recent trading signals from AI
                </p>
              </div>
              <div className="flex items-center gap-3">
                {status?.decisions &&
                  Object.values(status.decisions).filter(
                    (d: any) => d.action === "BUY"
                  ).length > 0 && (
                    <span className="text-xs font-medium text-green-400 bg-green-400/10 px-2 py-0.5 rounded-full border border-green-400/20">
                      {
                        Object.values(status.decisions).filter(
                          (d: any) => d.action === "BUY"
                        ).length
                      }{" "}
                      BUY
                    </span>
                  )}
                {status?.decisions &&
                  Object.values(status.decisions).filter(
                    (d: any) => d.action === "SELL"
                  ).length > 0 && (
                    <span className="text-xs font-medium text-red-400 bg-red-400/10 px-2 py-0.5 rounded-full border border-red-400/20">
                      {
                        Object.values(status.decisions).filter(
                          (d: any) => d.action === "SELL"
                        ).length
                      }{" "}
                      SELL
                    </span>
                  )}
              </div>
            </div>
            <div className="px-4 py-2 bg-white/5 border-b border-white/5 flex justify-between items-center text-xs text-muted-foreground">
              <span>Recent signal analysis</span>
              <span>
                {status?.decisions ? Object.keys(status.decisions).length : 0}{" "}
                active signals
              </span>
            </div>

            {status?.decisions && Object.keys(status.decisions).length > 0 ? (
              <div className="p-4 grid grid-cols-1 md:grid-cols-2 gap-3">
                <AnimatePresence mode="popLayout">
                  {Object.entries(status.decisions).map(
                    ([symbol, dec]: [string, any], index) => (
                      <motion.div
                        key={symbol}
                        layout
                        initial={{ opacity: 0, scale: 0.95 }}
                        animate={{ opacity: 1, scale: 1 }}
                        exit={{ opacity: 0, scale: 0.95 }}
                        transition={{ duration: 0.2, delay: index * 0.05 }}
                        className="h-full"
                      >
                        <SpotlightCard
                          className="p-4 h-full flex flex-col justify-between hover:border-primary/50 transition-colors"
                          spotlightColor={
                            dec.action === "BUY"
                              ? "rgba(34, 197, 94, 0.1)"
                              : dec.action === "SELL"
                              ? "rgba(239, 68, 68, 0.1)"
                              : "rgba(59, 130, 246, 0.1)"
                          }
                        >
                          <div>
                            <div className="flex justify-between items-center mb-3">
                              <div className="flex items-center gap-2">
                                <span className="font-semibold text-lg">
                                  {symbol}
                                </span>
                              </div>
                              <GlowBadge
                                variant={
                                  dec.action === "BUY"
                                    ? "success"
                                    : dec.action === "SELL"
                                    ? "danger"
                                    : dec.action === "CLOSE"
                                    ? "warning"
                                    : "secondary"
                                }
                                glow
                                className="font-bold"
                              >
                                {dec.action}
                              </GlowBadge>
                            </div>

                            <div className="grid grid-cols-2 gap-2 mb-3">
                              <div className="bg-black/20 rounded p-1.5 text-center border border-white/5">
                                <div className="text-[10px] text-muted-foreground uppercase tracking-wider mb-0.5">
                                  Confidence
                                </div>
                                <div
                                  className={`font-mono font-medium ${
                                    dec.confidence >= 70
                                      ? "text-green-400"
                                      : dec.confidence >= 40
                                      ? "text-yellow-400"
                                      : "text-red-400"
                                  }`}
                                >
                                  {dec.confidence}%
                                </div>
                              </div>
                              <div className="bg-black/20 rounded p-1.5 text-center border border-white/5">
                                <div className="text-[10px] text-muted-foreground uppercase tracking-wider mb-0.5">
                                  Price
                                </div>
                                <div className="font-mono text-muted-foreground">
                                  ${dec.current_price?.toFixed(2) || "---"}
                                </div>
                              </div>
                            </div>
                          </div>

                          <div className="mt-2 text-sm text-muted-foreground/80 bg-white/5 p-2 rounded border border-white/5 flex-grow">
                            <p className="line-clamp-3 italic">
                              "{dec.reasoning}"
                            </p>
                          </div>
                        </SpotlightCard>
                      </motion.div>
                    )
                  )}
                </AnimatePresence>
              </div>
            ) : (
              <div className="p-12 text-center">
                <Activity className="w-12 h-12 text-muted-foreground/30 mx-auto mb-4" />
                <p className="text-muted-foreground">No recent AI decisions</p>
                <p className="text-sm text-muted-foreground/60">
                  Decisions will appear here when the AI analyzes markets
                </p>
              </div>
            )}
          </GlassCard>
        </div>
      </div>

      {/* Alert Dialog */}
      {AlertDialog}
    </div>
  );
}
