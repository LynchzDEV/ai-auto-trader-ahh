import { useEffect, useState } from 'react';
import { motion } from 'framer-motion';
import {
  History as HistoryIcon,
  RefreshCw,
  Search,
  Download,
  TrendingUp,
  TrendingDown,
  Filter,

} from 'lucide-react';
import { getTraders, getDecisions, getTrades } from '../lib/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { ScrollArea } from '@/components/ui/scroll-area';

import { GlassCard } from '@/components/ui/glass-card';
import { GlowBadge } from '@/components/ui/glow-badge';
import { StatCard } from '@/components/ui/stat-card';
import { MobileCardTable } from '@/components/ui/mobile-card-table';

interface RawDecision {
  id: number;
  trader_id: string;
  timestamp: string;
  decisions: string; // JSON string of decision array
  executed: boolean;
}

interface Decision {
  id: string;
  trader_id: string;
  symbol: string;
  action: string;
  confidence: number;
  reasoning: string;
  executed: boolean;
  pnl?: number;
  created_at: string;
}

interface Trade {
  id: number;
  trader_id: string;
  symbol: string;
  side: string;
  price: number;
  quantity: number;
  quote_qty: number;
  realized_pnl: number;
  commission: number;
  timestamp: string;
  order_id: number;
}

export default function History() {
  const [traders, setTraders] = useState<any[]>([]);
  const [selectedTrader, setSelectedTrader] = useState<string>('');
  const [decisions, setDecisions] = useState<Decision[]>([]);
  const [filteredDecisions, setFilteredDecisions] = useState<Decision[]>([]);
  const [trades, setTrades] = useState<Trade[]>([]);
  const [filteredTrades, setFilteredTrades] = useState<Trade[]>([]);
  const [loading, setLoading] = useState(true);
  const [viewMode, setViewMode] = useState<'trades' | 'decisions'>('trades');

  // Filters
  const [searchQuery, setSearchQuery] = useState('');
  const [actionFilter, setActionFilter] = useState<string>('all');
  const [sortField] = useState<'created_at' | 'symbol' | 'pnl'>('created_at');
  const [sortDir] = useState<'asc' | 'desc'>('desc');

  useEffect(() => {
    loadTraders();
  }, []);

  useEffect(() => {
    if (selectedTrader) {
      loadDecisions();
      loadTrades();
    }
  }, [selectedTrader]);

  useEffect(() => {
    filterAndSort();
  }, [decisions, searchQuery, actionFilter, sortField, sortDir]);

  useEffect(() => {
    filterAndSortTrades();
  }, [trades, searchQuery, actionFilter, sortField, sortDir]);

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

  // Check if a decision is an error/failed entry
  const isErrorDecision = (reasoning: string, action: string, confidence: number): boolean => {
    if (!reasoning) return false;
    const lowerReasoning = reasoning.toLowerCase();
    const errorPatterns = [
      'failed',
      'error',
      'timeout',
      'context deadline exceeded',
      'unable to',
      'could not',
    ];
    return (
      errorPatterns.some(pattern => lowerReasoning.includes(pattern)) ||
      (confidence === 0 && action === 'NONE')
    );
  };

  const loadDecisions = async () => {
    try {
      const res = await getDecisions(selectedTrader);
      const rawDecisions: RawDecision[] = res.data.decisions || [];

      // Flatten the nested decisions and filter out errors
      const flatDecisions: Decision[] = [];
      for (const raw of rawDecisions) {
        try {
          const innerDecisions = JSON.parse(raw.decisions || '[]');
          for (const dec of innerDecisions) {
            const reasoning = dec.reasoning || dec.error || 'No reasoning provided';
            const action = dec.action || 'NONE';
            const confidence = dec.confidence || 0;

            // Skip error entries - they shouldn't be shown in trade history
            if (isErrorDecision(reasoning, action, confidence)) {
              continue;
            }

            flatDecisions.push({
              id: `${raw.id}-${dec.symbol}`,
              trader_id: raw.trader_id,
              symbol: dec.symbol || 'UNKNOWN',
              action: action,
              confidence: confidence,
              reasoning: reasoning,
              executed: raw.executed,
              pnl: dec.pnl,
              created_at: raw.timestamp,
            });
          }
        } catch {
          // Skip malformed decisions
        }
      }
      setDecisions(flatDecisions);
    } catch (err) {
      console.error('Failed to load decisions:', err);
    }
  };

  const loadTrades = async () => {
    try {
      const res = await getTrades(selectedTrader);
      setTrades(res.data.trades || []);
    } catch (err) {
      console.error('Failed to load trades:', err);
    }
  };

  const filterAndSortTrades = () => {
    let filtered = [...trades];

    // Search filter
    if (searchQuery) {
      const query = searchQuery.toLowerCase();
      filtered = filtered.filter((t) => t.symbol.toLowerCase().includes(query));
    }

    // Action filter
    if (actionFilter !== 'all') {
      filtered = filtered.filter((t) => {
        const side = t.side.toLowerCase();
        switch (actionFilter) {
          case 'buy':
            return side === 'buy';
          case 'sell':
            return side === 'sell';
          default:
            return true;
        }
      });
    }

    // Sort
    filtered.sort((a, b) => {
      let aVal: any, bVal: any;
      switch (sortField) {
        case 'created_at':
          aVal = new Date(a.timestamp).getTime();
          bVal = new Date(b.timestamp).getTime();
          break;
        case 'symbol':
          aVal = a.symbol;
          bVal = b.symbol;
          break;
        case 'pnl':
          aVal = a.realized_pnl || 0;
          bVal = b.realized_pnl || 0;
          break;
        default:
          aVal = new Date(a.timestamp).getTime();
          bVal = new Date(b.timestamp).getTime();
      }
      if (sortDir === 'asc') {
        return aVal > bVal ? 1 : -1;
      }
      return aVal < bVal ? 1 : -1;
    });

    setFilteredTrades(filtered);
  };

  const filterAndSort = () => {
    let filtered = [...decisions];

    // Search filter
    if (searchQuery) {
      const query = searchQuery.toLowerCase();
      filtered = filtered.filter(
        (d) =>
          d.symbol.toLowerCase().includes(query) ||
          d.reasoning.toLowerCase().includes(query)
      );
    }

    // Action filter - handle various action name formats
    if (actionFilter !== 'all') {
      filtered = filtered.filter((d) => {
        const action = d.action.toLowerCase();
        switch (actionFilter) {
          case 'buy':
            return action === 'buy' || action === 'open_long';
          case 'sell':
            return action === 'sell' || action === 'open_short';
          case 'close':
            return action === 'close' || action === 'close_long' || action === 'close_short';
          case 'hold':
            return action === 'hold' || action === 'wait';
          default:
            return action === actionFilter;
        }
      });
    }

    // Sort
    filtered.sort((a, b) => {
      let aVal: any, bVal: any;
      switch (sortField) {
        case 'created_at':
          aVal = new Date(a.created_at).getTime();
          bVal = new Date(b.created_at).getTime();
          break;
        case 'symbol':
          aVal = a.symbol;
          bVal = b.symbol;
          break;
        case 'pnl':
          aVal = a.pnl || 0;
          bVal = b.pnl || 0;
          break;
        default:
          aVal = a.created_at;
          bVal = b.created_at;
      }
      if (sortDir === 'asc') {
        return aVal > bVal ? 1 : -1;
      }
      return aVal < bVal ? 1 : -1;
    });

    setFilteredDecisions(filtered);
  };



  const exportToCsv = () => {
    const headers = ['Date', 'Symbol', 'Action', 'Confidence', 'PnL', 'Reasoning'];
    const rows = filteredDecisions.map((d) => [
      new Date(d.created_at).toISOString(),
      d.symbol,
      d.action,
      d.confidence,
      d.pnl || 'N/A',
      `"${d.reasoning.replace(/"/g, '""')}"`,
    ]);

    const csv = [headers.join(','), ...rows.map((r) => r.join(','))].join('\n');
    const blob = new Blob([csv], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `trade-history-${new Date().toISOString().split('T')[0]}.csv`;
    a.click();
  };

  // Calculate stats based on view mode
  const tradeStats = {
    total: filteredTrades.length,
    buys: filteredTrades.filter((t) => t.side === 'BUY').length,
    sells: filteredTrades.filter((t) => t.side === 'SELL').length,
    totalPnl: filteredTrades.reduce((sum, t) => sum + (t.realized_pnl || 0), 0),
    totalCommission: filteredTrades.reduce((sum, t) => sum + (t.commission || 0), 0),
  };

  const decisionStats = {
    total: filteredDecisions.length,
    executed: filteredDecisions.filter((d) => d.executed).length,
    totalPnl: filteredDecisions.reduce((sum, d) => sum + (d.pnl || 0), 0),
    avgConfidence:
      filteredDecisions.length > 0
        ? filteredDecisions.reduce((sum, d) => sum + d.confidence, 0) / filteredDecisions.length
        : 0,
  };

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
          <span className="text-muted-foreground">Loading trade history...</span>
        </div>
      </div>
    );
  }



  return (
    <div className="p-4 lg:p-6 space-y-4 lg:space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-4">
        <motion.div
          initial={{ opacity: 0, x: -20 }}
          animate={{ opacity: 1, x: 0 }}
          className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4"
        >
          <div>
            <h1 className="text-2xl lg:text-3xl font-bold text-gradient flex items-center gap-3">
              <HistoryIcon className="w-6 h-6 lg:w-8 lg:h-8" />
              Trade History
            </h1>
            <p className="text-sm lg:text-base text-muted-foreground">Complete log of all AI trading decisions</p>
          </div>

          <div className="flex gap-2 w-full sm:w-auto">
            <Select value={selectedTrader} onValueChange={setSelectedTrader}>
              <SelectTrigger className="flex-1 sm:w-[180px] glass">
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
            <Button variant="outline" onClick={exportToCsv} className="glass hidden sm:flex">
              <Download className="h-4 w-4 mr-2" />
              Export
            </Button>
            <Button variant="outline" size="icon" onClick={exportToCsv} className="glass sm:hidden">
              <Download className="h-4 w-4" />
            </Button>
            <Button variant="outline" size="icon" onClick={() => { loadDecisions(); loadTrades(); }} className="glass">
              <RefreshCw className="h-4 w-4" />
            </Button>
          </div>
        </motion.div>
      </div>

      {/* View Mode Tabs */}
      <div className="flex gap-2">
        <Button
          variant={viewMode === 'trades' ? 'default' : 'outline'}
          onClick={() => setViewMode('trades')}
          className={viewMode === 'trades' ? '' : 'glass'}
        >
          <TrendingUp className="h-4 w-4 mr-2" />
          Executed Trades ({trades.length})
        </Button>
        <Button
          variant={viewMode === 'decisions' ? 'default' : 'outline'}
          onClick={() => setViewMode('decisions')}
          className={viewMode === 'decisions' ? '' : 'glass'}
        >
          <HistoryIcon className="h-4 w-4 mr-2" />
          AI Decisions ({decisions.length})
        </Button>
      </div>

      {/* Stats */}
      {viewMode === 'trades' ? (
        <div className="grid gap-4 md:grid-cols-4">
          <StatCard
            title="Total Trades"
            value={tradeStats.total}
            icon={HistoryIcon}
            decimals={0}
            delay={0}
          />
          <StatCard
            title="Buys / Sells"
            value={tradeStats.buys}
            suffix={` / ${tradeStats.sells}`}
            icon={TrendingUp}
            decimals={0}
            delay={1}
          />
          <StatCard
            title="Realized PnL"
            value={tradeStats.totalPnl}
            icon={tradeStats.totalPnl >= 0 ? TrendingUp : TrendingDown}
            prefix="$"
            decimals={2}
            colorize
            delay={2}
          />
          <StatCard
            title="Total Fees"
            value={tradeStats.totalCommission}
            icon={Filter}
            prefix="$"
            decimals={4}
            delay={3}
          />
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-4">
          <StatCard
            title="Total Decisions"
            value={decisionStats.total}
            icon={HistoryIcon}
            decimals={0}
            delay={0}
          />
          <StatCard
            title="Executed"
            value={decisionStats.executed}
            icon={TrendingUp}
            decimals={0}
            delay={1}
          />
          <StatCard
            title="Total PnL"
            value={decisionStats.totalPnl}
            icon={decisionStats.totalPnl >= 0 ? TrendingUp : TrendingDown}
            prefix="$"
            decimals={2}
            colorize
            delay={2}
          />
          <StatCard
            title="Avg Confidence"
            value={decisionStats.avgConfidence}
            icon={Filter}
            suffix="%"
            decimals={1}
            delay={3}
          />
        </div>
      )}

      {/* Filters */}
      <div className="flex flex-col sm:flex-row gap-3">
        <div className="relative flex-1">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Search symbol..."
            className="pl-10 glass"
          />
        </div>
        <Select value={actionFilter} onValueChange={setActionFilter}>
          <SelectTrigger className="w-full sm:w-[140px] glass">
            <SelectValue placeholder="Action" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Actions</SelectItem>
            <SelectItem value="buy">Buy</SelectItem>
            <SelectItem value="sell">Sell</SelectItem>
            <SelectItem value="hold">Hold</SelectItem>
            <SelectItem value="close">Close</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {/* Table */}
      <GlassCard className="p-0 overflow-hidden">
        <ScrollArea className="h-[400px] lg:h-[500px]">
          <div className="p-4 lg:p-0">
            {viewMode === 'trades' ? (
              <MobileCardTable<Trade>
                data={filteredTrades}
                keyExtractor={(trade) => trade.id}
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
                      <GlowBadge variant={v === 'BUY' ? 'success' : 'danger'}>
                        {v}
                      </GlowBadge>
                    ),
                  },
                  {
                    key: 'realized_pnl',
                    label: 'PnL',
                    primary: true,
                    align: 'right',
                    render: (v) => (
                      <span className={`font-mono font-medium ${v >= 0 ? 'text-green-400' : 'text-red-400'}`}>
                        {v !== 0 ? `$${v.toFixed(4)}` : '-'}
                      </span>
                    ),
                  },
                  {
                    key: 'timestamp',
                    label: 'Date',
                    primary: true,
                    render: (v) => <span className="text-xs text-muted-foreground">{new Date(v).toLocaleDateString()}</span>,
                  },
                  {
                    key: 'price',
                    label: 'Price',
                    align: 'right',
                    render: (v) => <span className="font-mono">${v.toFixed(2)}</span>,
                  },
                  {
                    key: 'quantity',
                    label: 'Quantity',
                    align: 'right',
                    render: (v) => <span className="font-mono">{v.toFixed(4)}</span>,
                  },
                  {
                    key: 'quote_qty',
                    label: 'Value',
                    align: 'right',
                    render: (v) => <span className="font-mono">${v.toFixed(2)}</span>,
                  },
                ]}
                emptyState={
                  <div className="p-12 text-center">
                    <HistoryIcon className="w-12 h-12 text-muted-foreground/30 mx-auto mb-4" />
                    <p className="text-muted-foreground">No trades found</p>
                  </div>
                }
              />
            ) : (
              <MobileCardTable<Decision>
                data={filteredDecisions}
                keyExtractor={(d) => d.id}
                columns={[
                  {
                    key: 'symbol',
                    label: 'Symbol',
                    primary: true,
                    render: (v) => <span className="font-medium">{v}</span>,
                  },
                  {
                    key: 'action',
                    label: 'Action',
                    primary: true,
                    render: (v) => (
                      <GlowBadge
                        variant={
                          ['buy', 'open_long'].includes(v?.toLowerCase())
                            ? 'success'
                            : ['sell', 'open_short'].includes(v?.toLowerCase())
                              ? 'danger'
                              : ['close', 'close_long', 'close_short'].includes(v?.toLowerCase())
                                ? 'warning'
                                : 'secondary'
                        }
                      >
                        {v?.toUpperCase() || 'N/A'}
                      </GlowBadge>
                    ),
                  },
                  {
                    key: 'pnl',
                    label: 'PnL',
                    primary: true,
                    align: 'right',
                    render: (v) => (
                      <span className={`font-mono font-medium ${(v || 0) >= 0 ? 'text-green-400' : 'text-red-400'}`}>
                        {v !== undefined ? `$${v.toFixed(2)}` : '-'}
                      </span>
                    ),
                  },
                  {
                    key: 'created_at',
                    label: 'Date',
                    primary: true,
                    render: (v) => <span className="text-xs text-muted-foreground">{new Date(v).toLocaleDateString()}</span>,
                  },
                  {
                    key: 'confidence',
                    label: 'Confidence',
                    align: 'right',
                    render: (v) => <span className="font-mono">{v}%</span>,
                  },
                  {
                    key: 'executed',
                    label: 'Status',
                    render: (v) => (
                      <GlowBadge variant={v ? 'success' : 'secondary'} dot={v}>
                        {v ? 'Executed' : 'Pending'}
                      </GlowBadge>
                    ),
                  },
                  {
                    key: 'reasoning',
                    label: 'Reasoning',
                    render: (v) => (
                      <p className="text-sm text-muted-foreground line-clamp-2">{v}</p>
                    ),
                  },
                ]}
                emptyState={
                  <div className="p-12 text-center">
                    <HistoryIcon className="w-12 h-12 text-muted-foreground/30 mx-auto mb-4" />
                    <p className="text-muted-foreground">No decisions found</p>
                  </div>
                }
              />
            )}
          </div>
        </ScrollArea>
      </GlassCard>
    </div>
  );
}
