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
  ChevronDown,
  ChevronUp,
} from 'lucide-react';
import { getTraders, getDecisions } from '../lib/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { GlassCard } from '@/components/ui/glass-card';
import { GlowBadge } from '@/components/ui/glow-badge';
import { StatCard } from '@/components/ui/stat-card';

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

export default function History() {
  const [traders, setTraders] = useState<any[]>([]);
  const [selectedTrader, setSelectedTrader] = useState<string>('');
  const [decisions, setDecisions] = useState<Decision[]>([]);
  const [filteredDecisions, setFilteredDecisions] = useState<Decision[]>([]);
  const [loading, setLoading] = useState(true);

  // Filters
  const [searchQuery, setSearchQuery] = useState('');
  const [actionFilter, setActionFilter] = useState<string>('all');
  const [sortField, setSortField] = useState<'created_at' | 'symbol' | 'pnl'>('created_at');
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc');

  useEffect(() => {
    loadTraders();
  }, []);

  useEffect(() => {
    if (selectedTrader) {
      loadDecisions();
    }
  }, [selectedTrader]);

  useEffect(() => {
    filterAndSort();
  }, [decisions, searchQuery, actionFilter, sortField, sortDir]);

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

  const handleSort = (field: 'created_at' | 'symbol' | 'pnl') => {
    if (sortField === field) {
      setSortDir(sortDir === 'asc' ? 'desc' : 'asc');
    } else {
      setSortField(field);
      setSortDir('desc');
    }
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

  // Calculate stats
  const stats = {
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

  const SortIcon = ({ field }: { field: string }) => {
    if (sortField !== field) return null;
    return sortDir === 'asc' ? (
      <ChevronUp className="w-4 h-4" />
    ) : (
      <ChevronDown className="w-4 h-4" />
    );
  };

  return (
    <div className="p-4 lg:p-6 space-y-4 lg:space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
        <motion.div
          initial={{ opacity: 0, x: -20 }}
          animate={{ opacity: 1, x: 0 }}
        >
          <h1 className="text-3xl font-bold text-gradient flex items-center gap-3">
            <HistoryIcon className="w-8 h-8" />
            Trade History
          </h1>
          <p className="text-muted-foreground">Complete log of all AI trading decisions</p>
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
          <Button variant="outline" onClick={exportToCsv} className="glass">
            <Download className="h-4 w-4 mr-2" />
            Export CSV
          </Button>
          <Button variant="outline" size="icon" onClick={loadDecisions} className="glass">
            <RefreshCw className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <StatCard
          title="Total Decisions"
          value={stats.total}
          icon={HistoryIcon}
          decimals={0}
          delay={0}
        />
        <StatCard
          title="Executed"
          value={stats.executed}
          icon={TrendingUp}
          decimals={0}
          delay={1}
        />
        <StatCard
          title="Total PnL"
          value={stats.totalPnl}
          icon={stats.totalPnl >= 0 ? TrendingUp : TrendingDown}
          prefix="$"
          decimals={2}
          colorize
          delay={2}
        />
        <StatCard
          title="Avg Confidence"
          value={stats.avgConfidence}
          icon={Filter}
          suffix="%"
          decimals={1}
          delay={3}
        />
      </div>

      {/* Filters */}
      <div className="flex gap-4 flex-wrap">
        <div className="relative flex-1 min-w-[200px]">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Search by symbol or reasoning..."
            className="pl-10 glass"
          />
        </div>
        <Select value={actionFilter} onValueChange={setActionFilter}>
          <SelectTrigger className="w-[150px] glass">
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
        <ScrollArea className="h-[500px]">
          <table className="w-full trading-table">
            <thead className="sticky top-0 bg-[#12121a] z-10">
              <tr className="border-b border-white/5">
                <th
                  className="p-4 text-left font-medium text-muted-foreground cursor-pointer hover:text-white"
                  onClick={() => handleSort('created_at')}
                >
                  <div className="flex items-center gap-1">
                    Date <SortIcon field="created_at" />
                  </div>
                </th>
                <th
                  className="p-4 text-left font-medium text-muted-foreground cursor-pointer hover:text-white"
                  onClick={() => handleSort('symbol')}
                >
                  <div className="flex items-center gap-1">
                    Symbol <SortIcon field="symbol" />
                  </div>
                </th>
                <th className="p-4 text-left font-medium text-muted-foreground">Action</th>
                <th className="p-4 text-right font-medium text-muted-foreground">Confidence</th>
                <th
                  className="p-4 text-right font-medium text-muted-foreground cursor-pointer hover:text-white"
                  onClick={() => handleSort('pnl')}
                >
                  <div className="flex items-center justify-end gap-1">
                    PnL <SortIcon field="pnl" />
                  </div>
                </th>
                <th className="p-4 text-left font-medium text-muted-foreground">Status</th>
              </tr>
            </thead>
            <tbody>
              {filteredDecisions.length === 0 ? (
                <tr>
                  <td colSpan={6} className="p-12 text-center">
                    <HistoryIcon className="w-12 h-12 text-muted-foreground/30 mx-auto mb-4" />
                    <p className="text-muted-foreground">No decisions found</p>
                  </td>
                </tr>
              ) : (
                filteredDecisions.map((decision) => (
                  <Dialog key={decision.id}>
                    <DialogTrigger asChild>
                      <tr
                        className="border-b border-white/5 hover:bg-white/5 cursor-pointer transition-colors"
                      >
                        <td className="p-4 text-sm">
                          {new Date(decision.created_at).toLocaleString()}
                        </td>
                        <td className="p-4 font-medium">{decision.symbol}</td>
                        <td className="p-4">
                          <GlowBadge
                            variant={
                              ['buy', 'open_long'].includes(decision.action?.toLowerCase())
                                ? 'success'
                                : ['sell', 'open_short'].includes(decision.action?.toLowerCase())
                                ? 'danger'
                                : ['close', 'close_long', 'close_short'].includes(decision.action?.toLowerCase())
                                ? 'warning'
                                : 'secondary'
                            }
                          >
                            {decision.action?.toUpperCase() || 'N/A'}
                          </GlowBadge>
                        </td>
                        <td className="p-4 text-right font-mono">{decision.confidence}%</td>
                        <td
                          className={`p-4 text-right font-mono font-medium ${
                            (decision.pnl || 0) >= 0 ? 'text-green-400' : 'text-red-400'
                          }`}
                        >
                          {decision.pnl !== undefined ? `$${decision.pnl.toFixed(2)}` : '-'}
                        </td>
                        <td className="p-4">
                          <GlowBadge
                            variant={decision.executed ? 'success' : 'secondary'}
                            dot={decision.executed}
                          >
                            {decision.executed ? 'Executed' : 'Pending'}
                          </GlowBadge>
                        </td>
                      </tr>
                    </DialogTrigger>
                    <DialogContent className="max-w-xl glass-card border-white/10">
                      <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                          {decision.symbol}
                          <GlowBadge
                            variant={
                              ['buy', 'open_long'].includes(decision.action?.toLowerCase())
                                ? 'success'
                                : ['sell', 'open_short'].includes(decision.action?.toLowerCase())
                                ? 'danger'
                                : ['close', 'close_long', 'close_short'].includes(decision.action?.toLowerCase())
                                ? 'warning'
                                : 'secondary'
                            }
                          >
                            {decision.action?.toUpperCase() || 'N/A'}
                          </GlowBadge>
                        </DialogTitle>
                      </DialogHeader>
                      <div className="space-y-4 mt-4">
                        <div className="grid grid-cols-3 gap-4">
                          <div>
                            <span className="text-sm text-muted-foreground">Confidence</span>
                            <p className="text-lg font-semibold">{decision.confidence}%</p>
                          </div>
                          <div>
                            <span className="text-sm text-muted-foreground">PnL</span>
                            <p
                              className={`text-lg font-semibold ${
                                (decision.pnl || 0) >= 0 ? 'text-green-400' : 'text-red-400'
                              }`}
                            >
                              {decision.pnl !== undefined ? `$${decision.pnl.toFixed(2)}` : 'N/A'}
                            </p>
                          </div>
                          <div>
                            <span className="text-sm text-muted-foreground">Status</span>
                            <p className="text-lg font-semibold">
                              {decision.executed ? 'Executed' : 'Pending'}
                            </p>
                          </div>
                        </div>
                        <div>
                          <span className="text-sm text-muted-foreground">Date</span>
                          <p className="font-medium">
                            {new Date(decision.created_at).toLocaleString()}
                          </p>
                        </div>
                        <div>
                          <span className="text-sm text-muted-foreground">AI Reasoning</span>
                          <p className="mt-1 text-sm text-muted-foreground bg-white/5 p-4 rounded-lg whitespace-pre-wrap">
                            {decision.reasoning}
                          </p>
                        </div>
                      </div>
                    </DialogContent>
                  </Dialog>
                ))
              )}
            </tbody>
          </table>
        </ScrollArea>
      </GlassCard>
    </div>
  );
}
