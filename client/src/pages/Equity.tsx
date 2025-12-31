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
  BarChart,
  Bar,
  Cell,
} from 'recharts';
import {
  TrendingUp,
  TrendingDown,
  RefreshCw,
  DollarSign,
  Activity,
} from 'lucide-react';
import { getTraders, getEquityHistory, getAccount } from '../lib/api';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { GlassCard } from '@/components/ui/glass-card';
import { StatCard } from '@/components/ui/stat-card';

interface EquityPoint {
  timestamp: string;
  equity: number;
}

interface DailyReturn {
  date: string;
  return: number;
}

export default function Equity() {
  const [traders, setTraders] = useState<any[]>([]);
  const [selectedTrader, setSelectedTrader] = useState<string>('');
  const [equityData, setEquityData] = useState<EquityPoint[]>([]);
  const [account, setAccount] = useState<any>(null);
  const [timeRange, setTimeRange] = useState('1M');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    loadTraders();
  }, []);

  useEffect(() => {
    if (selectedTrader) {
      loadEquityData();
    }
  }, [selectedTrader, timeRange]);

  const loadTraders = async () => {
    try {
      const res = await getTraders();
      setTraders(res.data.traders || []);
      if (res.data.traders?.length > 0) {
        setSelectedTrader(res.data.traders[0].id);
      }
    } catch (err) {
      console.error('Failed to load traders:', err);
    } finally {
      setLoading(false);
    }
  };

  const loadEquityData = async () => {
    try {
      const [equityRes, accountRes] = await Promise.all([
        getEquityHistory(selectedTrader).catch(() => ({ data: { history: [] } })),
        getAccount(selectedTrader).catch(() => ({ data: null })),
      ]);
      setEquityData(equityRes.data.history || []);
      setAccount(accountRes.data);
    } catch (err) {
      console.error('Failed to load equity data:', err);
    }
  };

  // Calculate metrics
  const calculateMetrics = () => {
    if (equityData.length < 2) return { pnl: 0, pnlPercent: 0, maxDrawdown: 0, dailyReturns: [] };

    const firstEquity = equityData[0]?.equity || 0;
    const lastEquity = equityData[equityData.length - 1]?.equity || 0;
    const pnl = lastEquity - firstEquity;
    const pnlPercent = firstEquity > 0 ? ((lastEquity - firstEquity) / firstEquity) * 100 : 0;

    // Calculate max drawdown
    let peak = equityData[0]?.equity || 0;
    let maxDrawdown = 0;
    for (const point of equityData) {
      if (point.equity > peak) peak = point.equity;
      const drawdown = ((peak - point.equity) / peak) * 100;
      if (drawdown > maxDrawdown) maxDrawdown = drawdown;
    }

    // Calculate daily returns
    const dailyReturns: DailyReturn[] = [];
    for (let i = 1; i < equityData.length; i++) {
      const prevEquity = equityData[i - 1].equity;
      const currEquity = equityData[i].equity;
      if (prevEquity > 0) {
        dailyReturns.push({
          date: equityData[i].timestamp,
          return: ((currEquity - prevEquity) / prevEquity) * 100,
        });
      }
    }

    return { pnl, pnlPercent, maxDrawdown, dailyReturns };
  };

  const metrics = calculateMetrics();

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
          <span className="text-muted-foreground">Loading equity data...</span>
        </div>
      </div>
    );
  }

  return (
    <div className="p-4 lg:p-6 space-y-4 lg:space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
        <motion.div
          initial={{ opacity: 0, x: -20 }}
          animate={{ opacity: 1, x: 0 }}
        >
          <h1 className="text-3xl font-bold text-gradient flex items-center gap-3">
            <TrendingUp className="w-8 h-8" />
            Equity Charts
          </h1>
          <p className="text-muted-foreground">Track portfolio performance over time</p>
        </motion.div>

        <div className="flex gap-2">
          <Select value={selectedTrader} onValueChange={setSelectedTrader}>
            <SelectTrigger className="w-[200px] glass">
              <SelectValue placeholder="Select trader" />
            </SelectTrigger>
            <SelectContent>
              {traders.map((t) => (
                <SelectItem key={t.id} value={t.id}>
                  {t.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button variant="outline" size="icon" onClick={loadEquityData} className="glass">
            <RefreshCw className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {/* Time Range Selector */}
      <div className="flex gap-2">
        {['1D', '1W', '1M', '3M', 'ALL'].map((range) => (
          <Button
            key={range}
            variant={timeRange === range ? 'default' : 'outline'}
            size="sm"
            onClick={() => setTimeRange(range)}
            className={timeRange !== range ? 'glass' : ''}
          >
            {range}
          </Button>
        ))}
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <StatCard
          title="Current Equity"
          value={account?.equity || 0}
          icon={DollarSign}
          prefix="$"
          decimals={2}
          delay={0}
        />
        <StatCard
          title="Total PnL"
          value={metrics.pnl}
          icon={metrics.pnl >= 0 ? TrendingUp : TrendingDown}
          prefix="$"
          decimals={2}
          colorize
          change={metrics.pnlPercent}
          delay={1}
        />
        <StatCard
          title="Max Drawdown"
          value={metrics.maxDrawdown}
          icon={TrendingDown}
          suffix="%"
          decimals={2}
          delay={2}
        />
        <StatCard
          title="Data Points"
          value={equityData.length}
          icon={Activity}
          decimals={0}
          delay={3}
        />
      </div>

      {/* Charts */}
      <GlassCard>
        <Tabs defaultValue="equity">
          <TabsList className="grid grid-cols-2 w-full max-w-md">
            <TabsTrigger value="equity">Equity Curve</TabsTrigger>
            <TabsTrigger value="returns">Daily Returns</TabsTrigger>
          </TabsList>

          <TabsContent value="equity" className="mt-4">
            {equityData.length > 0 ? (
              <div className="h-[450px]">
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={equityData}>
                    <defs>
                      <linearGradient id="colorEquityMain" x1="0" y1="0" x2="0" y2="1">
                        <stop
                          offset="5%"
                          stopColor={metrics.pnl >= 0 ? '#22c55e' : '#ef4444'}
                          stopOpacity={0.3}
                        />
                        <stop
                          offset="95%"
                          stopColor={metrics.pnl >= 0 ? '#22c55e' : '#ef4444'}
                          stopOpacity={0}
                        />
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
                      domain={['dataMin - 100', 'dataMax + 100']}
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
                      stroke={metrics.pnl >= 0 ? '#22c55e' : '#ef4444'}
                      strokeWidth={2}
                      fill="url(#colorEquityMain)"
                    />
                  </AreaChart>
                </ResponsiveContainer>
              </div>
            ) : (
              <div className="h-[450px] flex items-center justify-center">
                <div className="text-center">
                  <TrendingUp className="w-16 h-16 text-muted-foreground/30 mx-auto mb-4" />
                  <p className="text-muted-foreground">No equity data available</p>
                  <p className="text-sm text-muted-foreground/60">
                    Start trading to see your equity curve
                  </p>
                </div>
              </div>
            )}
          </TabsContent>

          <TabsContent value="returns" className="mt-4">
            {metrics.dailyReturns.length > 0 ? (
              <div className="h-[450px]">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={metrics.dailyReturns}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#ffffff10" />
                    <XAxis
                      dataKey="date"
                      stroke="#71717a"
                      fontSize={12}
                      tickFormatter={(v) => new Date(v).toLocaleDateString()}
                    />
                    <YAxis
                      stroke="#71717a"
                      fontSize={12}
                      tickFormatter={(v) => `${v.toFixed(1)}%`}
                    />
                    <Tooltip
                      contentStyle={{
                        backgroundColor: '#12121a',
                        border: '1px solid rgba(255,255,255,0.1)',
                        borderRadius: '8px',
                      }}
                      labelFormatter={(v) => new Date(v).toLocaleDateString()}
                      formatter={(v) => [`${Number(v).toFixed(2)}%`, 'Return']}
                    />
                    <Bar dataKey="return" radius={[4, 4, 0, 0]}>
                      {metrics.dailyReturns.map((entry, index) => (
                        <Cell
                          key={`cell-${index}`}
                          fill={entry.return >= 0 ? '#22c55e' : '#ef4444'}
                        />
                      ))}
                    </Bar>
                  </BarChart>
                </ResponsiveContainer>
              </div>
            ) : (
              <div className="h-[450px] flex items-center justify-center">
                <div className="text-center">
                  <Activity className="w-16 h-16 text-muted-foreground/30 mx-auto mb-4" />
                  <p className="text-muted-foreground">No daily returns data</p>
                </div>
              </div>
            )}
          </TabsContent>
        </Tabs>
      </GlassCard>
    </div>
  );
}
