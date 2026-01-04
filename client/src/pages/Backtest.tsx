import { useEffect, useState } from 'react';
import { motion } from 'framer-motion';
import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from 'recharts';
import {
  FlaskConical,
  Play,
  Trash2,
  RefreshCw,
  TrendingUp,
  TrendingDown,
  Target,
  Percent,
  Activity,
} from 'lucide-react';
import {
  listBacktests,
  startBacktest,
  getBacktestMetrics,
  getBacktestEquity,
  getBacktestTrades,
  deleteBacktest,
  getStrategies,
} from '../lib/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { GlassCard } from '@/components/ui/glass-card';
import { GlowBadge } from '@/components/ui/glow-badge';
import { StatCard, ProgressStat } from '@/components/ui/stat-card';
import { SpotlightCard } from '@/components/ui/spotlight-card';
import { useConfirm, useAlert } from '@/components/ui/confirm-modal';
import { MobileCardTable } from '@/components/ui/mobile-card-table';

interface BacktestConfig {
  symbols: string[];
  initial_balance: number;
  start_ts: number;
  end_ts: number;
}

interface BacktestRun {
  run_id: string;
  status: string;
  config: BacktestConfig;
  started_at: string;
  completed_at: string;
  current_equity: number;
  progress: number;
  error?: string;
}

interface BacktestMetrics {
  total_return: number;
  total_return_pct: number;
  win_rate: number;
  total_trades: number;
  winning_trades: number;
  losing_trades: number;
  sharpe_ratio: number;
  max_drawdown: number;
  max_drawdown_pct: number;
  avg_win: number;
  avg_loss: number;
  profit_factor: number;
  final_equity: number;
}

interface EquityPoint {
  timestamp: string;
  equity: number;
}

interface Trade {
  timestamp: string;
  symbol: string;
  side: string;
  entry_price: number;
  exit_price: number;
  pnl: number;
  pnl_percent: number;
}

export default function Backtest() {
  const [runs, setRuns] = useState<BacktestRun[]>([]);
  const [selectedRun, setSelectedRun] = useState<string | null>(null);
  const [metrics, setMetrics] = useState<BacktestMetrics | null>(null);
  const [equityCurve, setEquityCurve] = useState<EquityPoint[]>([]);
  const [trades, setTrades] = useState<Trade[]>([]);
  const [strategies, setStrategies] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const { confirm, ConfirmDialog } = useConfirm();
  const { alert, AlertDialog } = useAlert();

  // Form state
  const [formData, setFormData] = useState({
    symbols: 'BTCUSDT,ETHUSDT',
    start_date: '',
    end_date: '',
    initial_capital: 10000,
    strategy_id: '',
    ai_model: 'deepseek/deepseek-v3.2',
  });

  useEffect(() => {
    loadData();
  }, []);

  useEffect(() => {
    if (selectedRun) {
      loadRunData(selectedRun);
    }
  }, [selectedRun]);

  const loadData = async () => {
    try {
      const [runsRes, strategiesRes] = await Promise.all([
        listBacktests().catch(() => ({ data: { backtests: [] } })),
        getStrategies().catch(() => ({ data: { strategies: [] } })),
      ]);
      const backtests = runsRes.data.backtests || [];
      setRuns(backtests);
      setStrategies(strategiesRes.data.strategies || []);
      if (backtests.length > 0 && !selectedRun) {
        setSelectedRun(backtests[0].run_id);
      }
    } catch (err) {
      console.error('Failed to load data:', err);
    } finally {
      setLoading(false);
    }
  };

  const loadRunData = async (runId: string) => {
    try {
      const [metricsRes, equityRes, tradesRes] = await Promise.all([
        getBacktestMetrics(runId).catch(() => ({ data: null })),
        getBacktestEquity(runId).catch(() => ({ data: { equity: [] } })),
        getBacktestTrades(runId).catch(() => ({ data: { trades: [] } })),
      ]);
      setMetrics(metricsRes.data);
      setEquityCurve(equityRes.data.equity || []);
      setTrades(tradesRes.data.trades || []);
    } catch (err) {
      console.error('Failed to load run data:', err);
    }
  };

  const handleStartBacktest = async () => {
    setCreating(true);
    try {
      const data = {
        symbols: formData.symbols.split(',').map((s) => s.trim()),
        start_date: formData.start_date,
        end_date: formData.end_date,
        initial_capital: formData.initial_capital,
        strategy_id: formData.strategy_id,
        ai_model: formData.ai_model,
      };
      const res = await startBacktest(data);
      setSelectedRun(res.data.run_id);
      await loadData();
    } catch (err: any) {
      alert({
        title: 'Error',
        description: err.response?.data?.error || 'Failed to start backtest',
        variant: 'danger',
      });
    } finally {
      setCreating(false);
    }
  };

  const handleDeleteBacktest = async (runId: string) => {
    const confirmed = await confirm({
      title: 'Delete Backtest',
      description: 'Are you sure you want to delete this backtest? This action cannot be undone.',
      confirmText: 'Delete',
      variant: 'danger',
    });
    if (!confirmed) return;
    try {
      await deleteBacktest(runId);
      if (selectedRun === runId) {
        setSelectedRun(null);
        setMetrics(null);
        setEquityCurve([]);
        setTrades([]);
      }
      await loadData();
    } catch (err) {
      console.error('Failed to delete backtest:', err);
    }
  };

  const currentRun = runs.find((r) => r.run_id === selectedRun);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-screen">
        <div className="flex flex-col items-center gap-4">
          <div className="relative w-16 h-16 flex items-center justify-center">
            <motion.div
              className="absolute inset-0 border-4 border-primary/20 rounded-full"
              animate={{ opacity: [0.3, 0.8, 0.3] }}
              transition={{ duration: 1.5, repeat: Infinity, ease: 'easeInOut' }}
            />
            <motion.div
              className="w-8 h-8 bg-primary/20 rounded-lg flex items-center justify-center"
              animate={{ opacity: [0.5, 1, 0.5] }}
              transition={{ duration: 1.5, repeat: Infinity, ease: 'easeInOut' }}
            >
              <div className="w-4 h-4 bg-primary rounded" />
            </motion.div>
          </div>
          <span className="text-muted-foreground">Loading backtests...</span>
        </div>
      </div>
    );
  }

  return (
    <div className="p-4 lg:p-6 space-y-4 lg:space-y-6">
      {/* Header */}
      <div className="flex justify-between items-start gap-4">
        <motion.div
          initial={{ opacity: 0, x: -20 }}
          animate={{ opacity: 1, x: 0 }}
        >
          <h1 className="text-2xl lg:text-3xl font-bold text-gradient flex items-center gap-3">
            <FlaskConical className="w-6 h-6 lg:w-8 lg:h-8" />
            Backtesting
          </h1>
          <p className="text-sm lg:text-base text-muted-foreground">
            Test your strategies on historical data
          </p>
        </motion.div>
        <Button variant="outline" size="icon" onClick={loadData} className="glass">
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        {/* Configuration Panel */}
        <GlassCard className="lg:col-span-1 p-0 overflow-hidden">
          <div className="p-4 border-b border-white/5">
            <h2 className="font-semibold">New Backtest</h2>
            <p className="text-sm text-muted-foreground">Configure and run</p>
          </div>

          <div className="p-4 space-y-4">
            <div className="space-y-2">
              <Label>Symbols (comma-separated)</Label>
              <Input
                value={formData.symbols}
                onChange={(e) => setFormData({ ...formData, symbols: e.target.value })}
                placeholder="BTCUSDT,ETHUSDT"
                className="glass"
              />
            </div>

            <div className="grid grid-cols-2 gap-2">
              <div className="space-y-2">
                <Label>Start Date</Label>
                <Input
                  type="date"
                  value={formData.start_date}
                  onChange={(e) => setFormData({ ...formData, start_date: e.target.value })}
                  className="glass"
                />
              </div>
              <div className="space-y-2">
                <Label>End Date</Label>
                <Input
                  type="date"
                  value={formData.end_date}
                  onChange={(e) => setFormData({ ...formData, end_date: e.target.value })}
                  className="glass"
                />
              </div>
            </div>

            <div className="space-y-2">
              <Label>Initial Capital ($)</Label>
              <Input
                type="number"
                value={formData.initial_capital}
                onChange={(e) =>
                  setFormData({ ...formData, initial_capital: Number(e.target.value) })
                }
                className="glass"
              />
            </div>

            <div className="space-y-2">
              <Label>Strategy</Label>
              <Select
                value={formData.strategy_id}
                onValueChange={(v) => setFormData({ ...formData, strategy_id: v })}
              >
                <SelectTrigger className="glass">
                  <SelectValue placeholder="Select strategy" />
                </SelectTrigger>
                <SelectContent>
                  {strategies.map((s) => (
                    <SelectItem key={s.id} value={s.id}>
                      {s.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label>AI Model</Label>
              <Select
                value={formData.ai_model}
                onValueChange={(v) => setFormData({ ...formData, ai_model: v })}
              >
                <SelectTrigger className="glass">
                  <SelectValue placeholder="Select model" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="google/gemini-2.5-flash">Gemini 2.5 Flash</SelectItem>
                  <SelectItem value="openai/gpt-oss-120b">GPT-OSS-120B</SelectItem>
                  <SelectItem value="x-ai/grok-4.1-fast">Grok 4.1 Fast</SelectItem>
                  <SelectItem value="deepseek/deepseek-v3.2">DeepSeek V3.2</SelectItem>
                  <SelectItem value="openai/gpt-5-mini">GPT-5 Mini</SelectItem>
                  <SelectItem value="openai/gpt-4.1-nano">GPT-4.1 Nano</SelectItem>
                  <SelectItem value="openai/gpt-4o-mini">GPT-4o Mini</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <Button
              className="w-full"
              onClick={handleStartBacktest}
              disabled={creating || !formData.start_date || !formData.end_date}
            >
              {creating ? (
                <>
                  <motion.div
                    className="w-4 h-4 mr-2 bg-current rounded-sm"
                    animate={{ opacity: [0.4, 1, 0.4] }}
                    transition={{ duration: 1, repeat: Infinity }}
                  />
                  Starting...
                </>
              ) : (
                <>
                  <Play className="w-4 h-4 mr-2" />
                  Start Backtest
                </>
              )}
            </Button>
          </div>

          {/* Previous Runs */}
          <div className="p-4 border-t border-white/5">
            <h3 className="font-medium mb-3">Previous Runs</h3>
            <ScrollArea className="h-[200px]">
              <div className="space-y-2">
                {runs.length === 0 ? (
                  <p className="text-sm text-muted-foreground text-center py-4">
                    No backtests yet
                  </p>
                ) : (
                  runs.map((run) => (
                    <motion.div
                      key={run.run_id}
                      initial={{ opacity: 0, x: -10 }}
                      animate={{ opacity: 1, x: 0 }}
                      className={`p-3 rounded-lg cursor-pointer transition-all ${
                        selectedRun === run.run_id
                          ? 'bg-primary/20 border border-primary/30'
                          : 'bg-white/5 hover:bg-white/10'
                      }`}
                      onClick={() => setSelectedRun(run.run_id)}
                    >
                      <div className="flex items-center justify-between mb-1">
                        <span className="text-sm font-medium truncate">
                          {run.config?.symbols?.join(', ') || 'No symbols'}
                        </span>
                        <GlowBadge
                          variant={
                            run.status === 'running'
                              ? 'info'
                              : run.status === 'completed'
                              ? 'success'
                              : run.status === 'failed'
                              ? 'danger'
                              : 'secondary'
                          }
                          pulse={run.status === 'running'}
                        >
                          {run.status}
                        </GlowBadge>
                      </div>
                      <div className="flex items-center justify-between text-xs text-muted-foreground">
                        <span>${(run.config?.initial_balance || 0).toLocaleString()}</span>
                        <div className="flex gap-1">
                          <Button
                            size="icon"
                            variant="ghost"
                            className="h-6 w-6"
                            onClick={(e) => {
                              e.stopPropagation();
                              handleDeleteBacktest(run.run_id);
                            }}
                          >
                            <Trash2 className="h-3 w-3" />
                          </Button>
                        </div>
                      </div>
                      {run.error && (
                        <div className="mt-2 text-xs text-red-400 truncate">
                          {run.error}
                        </div>
                      )}
                      {run.status === 'running' && (
                        <div className="mt-2">
                          <ProgressStat
                            label="Progress"
                            value={run.progress}
                            color="primary"
                          />
                        </div>
                      )}
                    </motion.div>
                  ))
                )}
              </div>
            </ScrollArea>
          </div>
        </GlassCard>

        {/* Results Panel */}
        <div className="lg:col-span-2 space-y-6">
          {selectedRun && currentRun ? (
            <>
              {/* Metrics Cards */}
              {metrics && (
                <div className="grid gap-3 lg:gap-4 grid-cols-2 lg:grid-cols-4">
                  <StatCard
                    title="Total PnL"
                    value={metrics.total_return}
                    icon={metrics.total_return >= 0 ? TrendingUp : TrendingDown}
                    prefix="$"
                    decimals={2}
                    colorize
                    change={metrics.total_return_pct}
                    delay={0}
                  />
                  <StatCard
                    title="Win Rate"
                    value={metrics.win_rate}
                    icon={Target}
                    suffix="%"
                    decimals={1}
                    delay={1}
                  />
                  <StatCard
                    title="Total Trades"
                    value={metrics.total_trades}
                    icon={Activity}
                    decimals={0}
                    delay={2}
                  />
                  <StatCard
                    title="Sharpe Ratio"
                    value={metrics.sharpe_ratio}
                    icon={Percent}
                    decimals={2}
                    delay={3}
                  />
                </div>
              )}

              {/* Charts */}
              <GlassCard>
                <Tabs defaultValue="equity">
                  <TabsList className="grid grid-cols-3 w-full max-w-md">
                    <TabsTrigger value="equity">Equity Curve</TabsTrigger>
                    <TabsTrigger value="trades">Trades</TabsTrigger>
                    <TabsTrigger value="metrics">Metrics</TabsTrigger>
                  </TabsList>

                  <TabsContent value="equity" className="mt-4">
                    {equityCurve.length > 0 ? (
                      <div className="h-[250px] lg:h-[350px]">
                        <ResponsiveContainer width="100%" height="100%">
                          <AreaChart data={equityCurve}>
                            <defs>
                              <linearGradient id="colorEquity" x1="0" y1="0" x2="0" y2="1">
                                <stop offset="5%" stopColor="#3b82f6" stopOpacity={0.3} />
                                <stop offset="95%" stopColor="#3b82f6" stopOpacity={0} />
                              </linearGradient>
                            </defs>
                            <CartesianGrid strokeDasharray="3 3" stroke="#ffffff10" />
                            <XAxis
                              dataKey="timestamp"
                              stroke="#71717a"
                              fontSize={12}
                              tickFormatter={(v) => new Date(v).toLocaleDateString()}
                            />
                            <YAxis
                              stroke="#71717a"
                              fontSize={12}
                              tickFormatter={(v) => `$${v.toLocaleString()}`}
                            />
                            <Tooltip
                              contentStyle={{
                                backgroundColor: '#12121a',
                                border: '1px solid rgba(255,255,255,0.1)',
                                borderRadius: '8px',
                              }}
                              labelFormatter={(v) => new Date(v).toLocaleString()}
                              formatter={(v) => [`$${Number(v).toFixed(2)}`, 'Equity']}
                            />
                            <Area
                              type="monotone"
                              dataKey="equity"
                              stroke="#3b82f6"
                              strokeWidth={2}
                              fill="url(#colorEquity)"
                            />
                          </AreaChart>
                        </ResponsiveContainer>
                      </div>
                    ) : (
                      <div className="h-[200px] lg:h-[350px] flex items-center justify-center">
                        <p className="text-muted-foreground">No equity data available</p>
                      </div>
                    )}
                  </TabsContent>

                  <TabsContent value="trades" className="mt-4">
                    <ScrollArea className="h-[250px] lg:h-[350px]">
                      <div className="p-4 lg:p-0">
                        <MobileCardTable<Trade>
                          data={trades}
                          keyExtractor={(_, i) => i}
                          columns={[
                            {
                              key: 'symbol',
                              label: 'Symbol',
                              primary: true,
                              render: (v) => <span className="font-medium">{v}</span>,
                            },
                            {
                              key: 'side',
                              label: 'Side',
                              primary: true,
                              render: (v) => (
                                <GlowBadge variant={v === 'LONG' ? 'success' : 'danger'}>
                                  {v}
                                </GlowBadge>
                              ),
                            },
                            {
                              key: 'pnl',
                              label: 'PnL',
                              primary: true,
                              align: 'right',
                              render: (v, trade) => (
                                <span className={`font-mono font-medium ${v >= 0 ? 'text-green-400' : 'text-red-400'}`}>
                                  ${v.toFixed(2)} ({trade.pnl_percent >= 0 ? '+' : ''}{trade.pnl_percent.toFixed(1)}%)
                                </span>
                              ),
                            },
                            {
                              key: 'timestamp',
                              label: 'Time',
                              render: (v) => <span className="text-sm">{new Date(v).toLocaleDateString()}</span>,
                            },
                            {
                              key: 'entry_price',
                              label: 'Entry',
                              align: 'right',
                              render: (v) => <span className="font-mono">${v.toFixed(2)}</span>,
                            },
                            {
                              key: 'exit_price',
                              label: 'Exit',
                              align: 'right',
                              render: (v) => <span className="font-mono">${v.toFixed(2)}</span>,
                            },
                          ]}
                          emptyState={
                            <div className="h-[200px] flex items-center justify-center">
                              <p className="text-muted-foreground">No trades yet</p>
                            </div>
                          }
                        />
                      </div>
                    </ScrollArea>
                  </TabsContent>

                  <TabsContent value="metrics" className="mt-4">
                    {metrics ? (
                      <div className="grid gap-4 md:grid-cols-2">
                        <SpotlightCard className="p-4">
                          <h4 className="text-sm text-muted-foreground mb-3">
                            Trade Statistics
                          </h4>
                          <div className="space-y-3">
                            <div className="flex justify-between">
                              <span className="text-muted-foreground">Winning Trades</span>
                              <span className="font-medium text-green-400">
                                {metrics.winning_trades}
                              </span>
                            </div>
                            <div className="flex justify-between">
                              <span className="text-muted-foreground">Losing Trades</span>
                              <span className="font-medium text-red-400">
                                {metrics.losing_trades}
                              </span>
                            </div>
                            <div className="flex justify-between">
                              <span className="text-muted-foreground">Avg Win</span>
                              <span className="font-medium text-green-400">
                                ${metrics.avg_win.toFixed(2)}
                              </span>
                            </div>
                            <div className="flex justify-between">
                              <span className="text-muted-foreground">Avg Loss</span>
                              <span className="font-medium text-red-400">
                                ${Math.abs(metrics.avg_loss).toFixed(2)}
                              </span>
                            </div>
                          </div>
                        </SpotlightCard>

                        <SpotlightCard className="p-4">
                          <h4 className="text-sm text-muted-foreground mb-3">
                            Risk Metrics
                          </h4>
                          <div className="space-y-3">
                            <div className="flex justify-between">
                              <span className="text-muted-foreground">Max Drawdown</span>
                              <span className="font-medium text-red-400">
                                {metrics.max_drawdown_pct.toFixed(2)}%
                              </span>
                            </div>
                            <div className="flex justify-between">
                              <span className="text-muted-foreground">Profit Factor</span>
                              <span className="font-medium">
                                {metrics.profit_factor.toFixed(2)}
                              </span>
                            </div>
                            <div className="flex justify-between">
                              <span className="text-muted-foreground">Sharpe Ratio</span>
                              <span className="font-medium">
                                {metrics.sharpe_ratio.toFixed(2)}
                              </span>
                            </div>
                            <div className="flex justify-between">
                              <span className="text-muted-foreground">Win Rate</span>
                              <span className="font-medium">
                                {metrics.win_rate.toFixed(1)}%
                              </span>
                            </div>
                          </div>
                        </SpotlightCard>
                      </div>
                    ) : (
                      <div className="h-[350px] flex items-center justify-center">
                        <p className="text-muted-foreground">No metrics available</p>
                      </div>
                    )}
                  </TabsContent>
                </Tabs>
              </GlassCard>
            </>
          ) : (
            <GlassCard className="p-12 text-center">
              <FlaskConical className="w-16 h-16 text-muted-foreground/30 mx-auto mb-4" />
              <h3 className="text-xl font-medium mb-2">No Backtest Selected</h3>
              <p className="text-muted-foreground">
                Configure and start a new backtest, or select a previous run from the list.
              </p>
            </GlassCard>
          )}
        </div>
      </div>

      {/* Confirmation and Alert Dialogs */}
      {ConfirmDialog}
      {AlertDialog}
    </div>
  );
}
