import { useEffect, useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { getStrategies, createStrategy, updateStrategy, deleteStrategy, getDefaultConfig, recommendPairs } from '../lib/api';
import type { Strategy, StrategyConfig } from '../types';
import {
  Plus,
  Pencil,
  Trash2,
  Save,
  RefreshCw,
  Layers,
  Shield,
  Activity,
  Clock,
  ChevronDown,
  ChevronUp,
  Brain,
  Target,
  BarChart3,
  Zap,
  Sparkles,
  Loader2,
  X,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Checkbox } from '@/components/ui/checkbox';
import { Slider } from '@/components/ui/slider';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { GlassCard } from '@/components/ui/glass-card';
import { GlowBadge } from '@/components/ui/glow-badge';
import { SpotlightCard } from '@/components/ui/spotlight-card';
import { useConfirm, useAlert } from '@/components/ui/confirm-modal';

// Moved outside component to prevent re-renders
const CollapsibleSection = ({
  title,
  icon: Icon,
  isExpanded,
  onToggle,
  children,
}: {
  title: string;
  icon: LucideIcon;
  isExpanded: boolean;
  onToggle: () => void;
  children: React.ReactNode;
}) => (
  <GlassCard className="p-0 overflow-hidden">
    <button
      type="button"
      onClick={onToggle}
      className="w-full flex items-center justify-between p-4 hover:bg-white/5 transition-colors"
    >
      <div className="flex items-center gap-3">
        <div className="p-2 rounded-lg bg-primary/20">
          <Icon className="w-4 h-4 text-primary" />
        </div>
        <h3 className="font-medium">{title}</h3>
      </div>
      {isExpanded ? (
        <ChevronUp className="w-4 h-4 text-muted-foreground" />
      ) : (
        <ChevronDown className="w-4 h-4 text-muted-foreground" />
      )}
    </button>
    <AnimatePresence initial={false}>
      {isExpanded && (
        <motion.div
          initial={{ height: 0, opacity: 0 }}
          animate={{ height: 'auto', opacity: 1 }}
          exit={{ height: 0, opacity: 0 }}
          transition={{ duration: 0.2 }}
          className="overflow-hidden"
        >
          <div className="px-4 pb-4">
            {children}
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  </GlassCard>
);

export default function Strategies() {
  const [strategies, setStrategies] = useState<Strategy[]>([]);
  const [loading, setLoading] = useState(true);
  const [editingStrategy, setEditingStrategy] = useState<Strategy | null>(null);
  const [isCreating, setIsCreating] = useState(false);
  const [isFindingPairs, setIsFindingPairs] = useState(false);
  const [defaultConfig, setDefaultConfig] = useState<StrategyConfig | null>(null);
  const [expandedSections, setExpandedSections] = useState<Record<string, boolean>>({
    coinSource: true,
    indicators: true,
    riskControl: true,
    aiPrompt: false,
  });
  const [coinInput, setCoinInput] = useState("");
  const { confirm, ConfirmDialog } = useConfirm();
  const { alert, AlertDialog } = useAlert();

  useEffect(() => {
    loadStrategies();
    loadDefaultConfig();
  }, []);

  const loadStrategies = async () => {
    try {
      const res = await getStrategies();
      setStrategies(res.data.strategies || []);
    } catch (err) {
      console.error('Failed to load strategies:', err);
    } finally {
      setLoading(false);
    }
  };

  const loadDefaultConfig = async () => {
    try {
      const res = await getDefaultConfig();
      setDefaultConfig(res.data);
    } catch (err) {
      console.error('Failed to load default config:', err);
    }
  };

  const handleCreate = () => {
    if (!defaultConfig) return;
    setEditingStrategy({
      id: '',
      name: 'New Strategy',
      description: '',
      is_active: false,
      config: defaultConfig,
      created_at: '',
      updated_at: '',
    });
    setIsCreating(true);
  };

  const handleSave = async () => {
    if (!editingStrategy) return;
    try {
      if (isCreating) {
        await createStrategy({
          name: editingStrategy.name,
          description: editingStrategy.description,
          config: editingStrategy.config,
        });
      } else {
        await updateStrategy(editingStrategy.id, {
          name: editingStrategy.name,
          description: editingStrategy.description,
          config: editingStrategy.config,
        });
      }
      setEditingStrategy(null);
      setIsCreating(false);
      loadStrategies();
    } catch (err: any) {
      alert({
        title: 'Error',
        description: err.response?.data?.error || 'Failed to save strategy',
        variant: 'danger',
      });
    }
  };

  const handleDelete = async (id: string) => {
    const confirmed = await confirm({
      title: 'Delete Strategy',
      description: 'Are you sure you want to delete this strategy? This action cannot be undone.',
      confirmText: 'Delete',
      variant: 'danger',
    });
    if (!confirmed) return;
    try {
      await deleteStrategy(id);
      loadStrategies();
    } catch (err: any) {
      alert({
        title: 'Error',
        description: err.response?.data?.error || 'Failed to delete strategy',
        variant: 'danger',
      });
    }
  };

  const toggleSection = (section: string) => {
    setExpandedSections((prev) => ({ ...prev, [section]: !prev[section] }));
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
          <span className="text-muted-foreground">Loading strategies...</span>
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
            <Layers className="w-6 h-6 lg:w-8 lg:h-8" />
            Strategies
          </h1>
          <p className="text-sm lg:text-base text-muted-foreground">Configure trading strategies and risk parameters</p>
        </motion.div>

        <div className="flex gap-2">
          <Button variant="outline" size="icon" onClick={loadStrategies} className="glass">
            <RefreshCw className="h-4 w-4" />
          </Button>
          <Button onClick={handleCreate} disabled={!defaultConfig} className="hidden sm:flex">
            <Plus className="w-4 h-4 mr-2" />
            New Strategy
          </Button>
          <Button onClick={handleCreate} disabled={!defaultConfig} size="icon" className="sm:hidden">
            <Plus className="w-4 h-4" />
          </Button>
        </div>
      </div>

      {/* Strategy List */}
      <div className="grid gap-4">
        {strategies.length === 0 ? (
          <GlassCard className="p-12 text-center">
            <Layers className="w-16 h-16 text-muted-foreground/30 mx-auto mb-4" />
            <h3 className="text-xl font-medium mb-2">No Strategies Yet</h3>
            <p className="text-muted-foreground">Create a strategy to configure your trading rules.</p>
          </GlassCard>
        ) : (
          strategies.map((strategy, index) => (
            <motion.div
              key={strategy.id}
              initial={{ opacity: 0, y: 20 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: index * 0.05 }}
            >
              <SpotlightCard className="p-5">
                <div className="flex justify-between items-start">
                  <div className="flex-1">
                    <div className="flex items-center gap-3 mb-2">
                      <div className="p-2 rounded-lg bg-primary/20">
                        <Target className="w-4 h-4 text-primary" />
                      </div>
                      <h3 className="font-semibold text-lg">{strategy.name}</h3>
                      <GlowBadge variant={strategy.is_active ? 'success' : 'secondary'}>
                        {strategy.is_active ? 'Active' : 'Inactive'}
                      </GlowBadge>
                    </div>
                    <p className="text-muted-foreground text-sm mb-3">
                      {strategy.description || 'No description'}
                    </p>

                    {/* Quick Stats */}
                    <div className="grid grid-cols-1 xs:grid-cols-2 lg:grid-cols-4 gap-3 lg:gap-4 mt-4">
                      <div className="flex items-center gap-2 text-sm">
                        <Clock className="w-4 h-4 text-blue-400" />
                        <span className="text-muted-foreground">Interval:</span>
                        <span className="font-medium">{strategy.config.trading_interval}m</span>
                      </div>
                      <div className="flex items-center gap-2 text-sm">
                        <Shield className="w-4 h-4 text-yellow-400" />
                        <span className="text-muted-foreground">Max Positions:</span>
                        <span className="font-medium">{strategy.config.risk_control.max_positions}</span>
                      </div>
                      <div className="flex items-center gap-2 text-sm">
                        <Activity className="w-4 h-4 text-green-400" />
                        <span className="text-muted-foreground">Min Confidence:</span>
                        <span className="font-medium">{strategy.config.risk_control.min_confidence}%</span>
                      </div>
                      <div className="flex items-center gap-2 text-sm">
                        <Zap className="w-4 h-4 text-purple-400" />
                        <span className="text-muted-foreground">Max Leverage:</span>
                        <span className="font-medium">{strategy.config.risk_control.max_leverage}x</span>
                      </div>
                    </div>

                    {/* Enabled Indicators */}
                    <div className="flex flex-wrap gap-2 mt-4">
                      {strategy.config.indicators.enable_ema && (
                        <GlowBadge variant="secondary">EMA</GlowBadge>
                      )}
                      {strategy.config.indicators.enable_macd && (
                        <GlowBadge variant="secondary">MACD</GlowBadge>
                      )}
                      {strategy.config.indicators.enable_rsi && (
                        <GlowBadge variant="secondary">RSI</GlowBadge>
                      )}
                      {strategy.config.indicators.enable_atr && (
                        <GlowBadge variant="secondary">ATR</GlowBadge>
                      )}
                      {strategy.config.indicators.enable_boll && (
                        <GlowBadge variant="secondary">BOLL</GlowBadge>
                      )}
                      {strategy.config.indicators.enable_volume && (
                        <GlowBadge variant="secondary">VOL</GlowBadge>
                      )}
                    </div>
                  </div>
                  <div className="flex gap-2 ml-4">
                    <Button
                      variant="outline"
                      size="icon"
                      className="glass"
                      onClick={() => { setEditingStrategy(strategy); setIsCreating(false); }}
                    >
                      <Pencil className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="outline"
                      size="icon"
                      className="glass text-red-400 hover:text-red-300"
                      onClick={() => handleDelete(strategy.id)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              </SpotlightCard>
            </motion.div>
          ))
        )}
      </div>

      {/* Confirmation and Alert Dialogs */}
      {ConfirmDialog}
      {AlertDialog}

      {/* Strategy Editor Modal */}
      <Dialog open={!!editingStrategy} onOpenChange={(open) => !open && setEditingStrategy(null)}>
        <DialogContent className="w-[95vw] max-w-4xl glass-card border-white/10 max-h-[85vh] overflow-y-auto p-4 lg:p-6">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Layers className="w-5 h-5" />
              {isCreating ? 'Create Strategy' : 'Edit Strategy'}
            </DialogTitle>
          </DialogHeader>

          {editingStrategy && (
            <div className="space-y-4 mt-4">
              {/* Basic Info */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label>Name</Label>
                  <Input
                    value={editingStrategy.name}
                    onChange={(e) => setEditingStrategy({ ...editingStrategy, name: e.target.value })}
                    className="glass"
                  />
                </div>
                <div className="space-y-2">
                  <Label>Description</Label>
                  <Input
                    value={editingStrategy.description}
                    onChange={(e) => setEditingStrategy({ ...editingStrategy, description: e.target.value })}
                    className="glass"
                    placeholder="Describe your strategy"
                  />
                </div>
              </div>

              {/* Coin Source */}
              <CollapsibleSection title="Coin Source" icon={Target} isExpanded={expandedSections.coinSource} onToggle={() => toggleSection('coinSource')}>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label>Source Type</Label>
                    <Select
                      value={editingStrategy.config.coin_source.source_type}
                      onValueChange={(v) => setEditingStrategy({
                        ...editingStrategy,
                        config: {
                          ...editingStrategy.config,
                          coin_source: { ...editingStrategy.config.coin_source, source_type: v }
                        }
                      })}
                    >
                      <SelectTrigger className="glass">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="static">Static List</SelectItem>
                        <SelectItem value="top_volume">Top by Volume</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>
                <div className="space-y-2">
                  <div className="flex justify-between items-center">
                    <Label>Trading Pairs</Label>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-6 text-xs text-primary hover:text-primary/80"
                      onClick={async () => {
                        if (isFindingPairs || !editingStrategy) return;
                        setIsFindingPairs(true);
                        try {
                          // Dynamic count: Max Positions (min) + 2
                          const maxPos = editingStrategy.config.risk_control.max_positions || 3;
                          const targetCount = maxPos + 2;

                          const res = await recommendPairs({
                            count: targetCount,
                            turbo: editingStrategy.config.turbo_mode // Pass Turbo Mode
                          });
                          if (res.data?.pairs) {
                            setEditingStrategy({
                              ...editingStrategy,
                              config: {
                                ...editingStrategy.config,
                                coin_source: {
                                  ...editingStrategy.config.coin_source,
                                  source_type: 'static',
                                  static_coins: res.data.pairs
                                }
                              }
                            });
                            alert({ title: 'Smart Find', description: `Found ${res.data.pairs.length} optimal pairs!`, variant: 'success' });
                          }
                        } catch (err) {
                          console.error(err);
                          alert({ title: 'Error', description: 'Failed to find pairs', variant: 'danger' });
                        } finally {
                          setIsFindingPairs(false);
                        }
                      }}
                      disabled={isFindingPairs}
                    >
                      {isFindingPairs ? <Loader2 className="w-3 h-3 mr-1 animate-spin" /> : <Sparkles className="w-3 h-3 mr-1" />}
                      Smart Find
                    </Button>
                  </div>
                  <div className="flex flex-col gap-2">
                    <div className="flex flex-wrap gap-2 p-3 rounded-md border border-white/10 bg-white/5 min-h-[42px] focus-within:ring-2 focus-within:ring-primary/20 transition-all">
                      {editingStrategy.config.coin_source.static_coins.map((coin, idx) => (
                        <GlowBadge key={coin + idx} variant="secondary" className="pl-2 pr-1 h-7 flex items-center gap-1 cursor-default hover:bg-white/20">
                          {coin}
                          <div
                            onClick={(e) => {
                              e.stopPropagation();
                              const newCoins = [...editingStrategy.config.coin_source.static_coins];
                              newCoins.splice(idx, 1);
                              setEditingStrategy({
                                ...editingStrategy,
                                config: {
                                  ...editingStrategy.config,
                                  coin_source: { ...editingStrategy.config.coin_source, static_coins: newCoins }
                                }
                              });
                            }}
                            className="p-1 rounded-full hover:bg-black/20 cursor-pointer transition-colors"
                          >
                            <X className="w-3 h-3 text-muted-foreground hover:text-red-400" />
                          </div>
                        </GlowBadge>
                      ))}
                      <input
                        className="flex-1 bg-transparent border-none outline-none text-sm min-w-[120px] h-7 placeholder:text-muted-foreground/50"
                        placeholder={editingStrategy.config.coin_source.static_coins.length === 0 ? "Type coin (e.g. BTCUSDT) & Enter..." : ""}
                        value={coinInput}
                        onChange={(e) => setCoinInput(e.target.value.toUpperCase())}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' || e.key === ',' || e.key === ' ') {
                            e.preventDefault();
                            const val = coinInput.trim().replace(/,/g, '');
                            if (val) {
                              if (!editingStrategy.config.coin_source.static_coins.includes(val)) {
                                setEditingStrategy({
                                  ...editingStrategy,
                                  config: {
                                    ...editingStrategy.config,
                                    coin_source: {
                                      ...editingStrategy.config.coin_source,
                                      static_coins: [...editingStrategy.config.coin_source.static_coins, val]
                                    }
                                  }
                                });
                              }
                              setCoinInput('');
                            }
                          }
                          if (e.key === 'Backspace' && !coinInput && editingStrategy.config.coin_source.static_coins.length > 0) {
                            const newCoins = [...editingStrategy.config.coin_source.static_coins];
                            newCoins.pop();
                            setEditingStrategy({
                              ...editingStrategy,
                              config: {
                                ...editingStrategy.config,
                                coin_source: { ...editingStrategy.config.coin_source, static_coins: newCoins }
                              }
                            });
                          }
                        }}
                      />
                    </div>
                    <p className="text-xs text-muted-foreground px-1">
                      Type symbol and press Enter, Space, or Comma to add. Click X or Backspace to remove.
                    </p>
                  </div>
                </div>

              </CollapsibleSection>

              {/* Technical Indicators */}
              <CollapsibleSection title="Technical Indicators" icon={BarChart3} isExpanded={expandedSections.indicators} onToggle={() => toggleSection('indicators')}>
                <div className="space-y-4">
                  <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                    <div className="space-y-2">
                      <Label>Timeframe</Label>
                      <Select
                        value={editingStrategy.config.indicators.primary_timeframe}
                        onValueChange={(v) => setEditingStrategy({
                          ...editingStrategy,
                          config: {
                            ...editingStrategy.config,
                            indicators: { ...editingStrategy.config.indicators, primary_timeframe: v }
                          }
                        })}
                      >
                        <SelectTrigger className="glass">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="1m">1 minute</SelectItem>
                          <SelectItem value="3m">3 minutes</SelectItem>
                          <SelectItem value="5m">5 minutes</SelectItem>
                          <SelectItem value="15m">15 minutes</SelectItem>
                          <SelectItem value="1h">1 hour</SelectItem>
                          <SelectItem value="4h">4 hours</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-2">
                      <Label>Kline Count</Label>
                      <Input
                        type="number"
                        value={editingStrategy.config.indicators.kline_count}
                        onChange={(e) => setEditingStrategy({
                          ...editingStrategy,
                          config: {
                            ...editingStrategy.config,
                            indicators: { ...editingStrategy.config.indicators, kline_count: parseInt(e.target.value) }
                          }
                        })}
                        className="glass"
                      />
                    </div>
                    <div className="space-y-2">
                      <Label>Trading Interval (min)</Label>
                      <Input
                        type="number"
                        value={editingStrategy.config.trading_interval}
                        onChange={(e) => setEditingStrategy({
                          ...editingStrategy,
                          config: { ...editingStrategy.config, trading_interval: parseInt(e.target.value) }
                        })}
                        className="glass"
                      />
                    </div>

                    <label className="space-y-2 cursor-pointer group col-span-full">
                      <Label className="cursor-pointer group-hover:text-yellow-400 transition-colors flex items-center gap-2">
                        <Zap className="w-4 h-4 text-yellow-400" />
                        Turbo Mode (High Risk)
                      </Label>
                      <div className="flex items-center h-10 px-3 rounded-md border border-white/10 bg-white/5 hover:bg-white/10 transition-colors">
                        <Checkbox
                          checked={editingStrategy.config.turbo_mode}
                          onCheckedChange={async (c) => {
                            if (c) {
                              const ok = await confirm({
                                title: '⚠️ ENABLE TURBO MODE? ⚠️',
                                description: 'WARNING: This mode activates EXTREME RISK protocols. You could lose 80-90% of your wallet in exchange for high potential rewards. The bot will ignore standard safety patterns. Are you sure?',
                                confirmText: 'I ACCEPT THE RISK',
                                variant: 'danger',
                              });
                              if (!ok) return;
                            }
                            setEditingStrategy({
                              ...editingStrategy,
                              config: { ...editingStrategy.config, turbo_mode: !!c }
                            });
                          }}
                          className="mr-2 data-[state=checked]:bg-yellow-400 data-[state=checked]:border-yellow-400 data-[state=checked]:text-black"
                        />
                        <span className={`text-sm ${editingStrategy.config.turbo_mode ? 'text-yellow-400 font-bold' : 'text-muted-foreground'}`}>
                          {editingStrategy.config.turbo_mode ? 'AGGRESSIVE (HIGH VOLATILITY)' : 'Standard Safety'}
                        </span>
                      </div>
                    </label>
                  </div>

                  <div className="grid grid-cols-2 sm:grid-cols-3 gap-3 lg:gap-4 mt-4">
                    {[
                      { key: 'enable_ema', label: 'EMA', desc: 'Exponential Moving Average' },
                      { key: 'enable_macd', label: 'MACD', desc: 'Moving Average Convergence' },
                      { key: 'enable_rsi', label: 'RSI', desc: 'Relative Strength Index' },
                      { key: 'enable_atr', label: 'ATR', desc: 'Average True Range' },
                      { key: 'enable_boll', label: 'Bollinger', desc: 'Bollinger Bands' },
                      { key: 'enable_volume', label: 'Volume', desc: 'Volume Analysis' },
                    ].map((ind) => (
                      <label
                        key={ind.key}
                        className="flex items-start gap-3 p-3 rounded-lg bg-white/5 hover:bg-white/10 cursor-pointer transition-colors"
                      >
                        <Checkbox
                          checked={(editingStrategy.config.indicators as any)[ind.key]}
                          onCheckedChange={(checked) => setEditingStrategy({
                            ...editingStrategy,
                            config: {
                              ...editingStrategy.config,
                              indicators: { ...editingStrategy.config.indicators, [ind.key]: checked }
                            }
                          })}
                        />
                        <div>
                          <span className="font-medium">{ind.label}</span>
                          <p className="text-xs text-muted-foreground">{ind.desc}</p>
                        </div>
                      </label>
                    ))}
                  </div>
                </div>
              </CollapsibleSection>

              {/* Risk Control */}
              <CollapsibleSection title="Risk Control" icon={Shield} isExpanded={expandedSections.riskControl} onToggle={() => toggleSection('riskControl')}>
                <div className="space-y-6">
                  {/* Sliders for visual parameters */}
                  <div className="space-y-4">
                    <div className="space-y-3">
                      <div className="flex justify-between items-center">
                        <Label>Max Positions</Label>
                        <span className="text-sm font-mono text-primary">
                          {editingStrategy.config.risk_control.max_positions}
                        </span>
                      </div>
                      <Slider
                        value={[editingStrategy.config.risk_control.max_positions]}
                        onValueChange={([v]) => setEditingStrategy({
                          ...editingStrategy,
                          config: {
                            ...editingStrategy.config,
                            risk_control: { ...editingStrategy.config.risk_control, max_positions: v }
                          }
                        })}
                        min={1}
                        max={20}
                        step={1}
                        className="w-full"
                      />
                    </div>

                    <div className="space-y-3">
                      <div className="flex justify-between items-center">
                        <Label>Max Leverage</Label>
                        <span className="text-sm font-mono text-primary">
                          {editingStrategy.config.risk_control.max_leverage}x
                        </span>
                      </div>
                      <Slider
                        value={[editingStrategy.config.risk_control.max_leverage]}
                        onValueChange={([v]) => setEditingStrategy({
                          ...editingStrategy,
                          config: {
                            ...editingStrategy.config,
                            risk_control: { ...editingStrategy.config.risk_control, max_leverage: v }
                          }
                        })}
                        min={1}
                        max={50}
                        step={1}
                        className="w-full"
                      />
                    </div>

                    <div className="space-y-3">
                      <div className="flex justify-between items-center">
                        <Label>Min Confidence</Label>
                        <span className="text-sm font-mono text-primary">
                          {editingStrategy.config.risk_control.min_confidence}%
                        </span>
                      </div>
                      <Slider
                        value={[editingStrategy.config.risk_control.min_confidence]}
                        onValueChange={([v]) => setEditingStrategy({
                          ...editingStrategy,
                          config: {
                            ...editingStrategy.config,
                            risk_control: { ...editingStrategy.config.risk_control, min_confidence: v }
                          }
                        })}
                        min={0}
                        max={100}
                        step={5}
                        className="w-full"
                      />
                    </div>

                    <div className="space-y-3">
                      <div className="flex justify-between items-center">
                        <Label>Max Position % of Balance</Label>
                        <span className="text-sm font-mono text-primary">
                          {editingStrategy.config.risk_control.max_position_percent}%
                        </span>
                      </div>
                      <Slider
                        value={[editingStrategy.config.risk_control.max_position_percent]}
                        onValueChange={([v]) => setEditingStrategy({
                          ...editingStrategy,
                          config: {
                            ...editingStrategy.config,
                            risk_control: { ...editingStrategy.config.risk_control, max_position_percent: v }
                          }
                        })}
                        min={1}
                        max={100}
                        step={1}
                        className="w-full"
                      />
                    </div>
                  </div>

                  {/* Numeric inputs for precise values */}
                  <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 pt-4 border-t border-white/10">
                    <div className="space-y-2">
                      <Label>Min Position USD</Label>
                      <Input
                        type="number"
                        value={editingStrategy.config.risk_control.min_position_usd}
                        onChange={(e) => setEditingStrategy({
                          ...editingStrategy,
                          config: {
                            ...editingStrategy.config,
                            risk_control: { ...editingStrategy.config.risk_control, min_position_usd: parseFloat(e.target.value) }
                          }
                        })}
                        className="glass"
                      />
                    </div>
                    <div className="space-y-2">
                      <Label>Min Risk/Reward</Label>
                      <Input
                        type="number"
                        step="0.1"
                        value={editingStrategy.config.risk_control.min_risk_reward_ratio}
                        onChange={(e) => setEditingStrategy({
                          ...editingStrategy,
                          config: {
                            ...editingStrategy.config,
                            risk_control: { ...editingStrategy.config.risk_control, min_risk_reward_ratio: parseFloat(e.target.value) }
                          }
                        })}
                        className="glass"
                      />
                    </div>
                  </div>
                </div>
              </CollapsibleSection>

              {/* AI Settings */}
              <CollapsibleSection title="AI Settings" icon={Brain} isExpanded={expandedSections.aiPrompt} onToggle={() => toggleSection('aiPrompt')}>
                <div className="space-y-4">
                  {/* Reasoning Mode Toggle */}
                  <div className="p-4 rounded-lg bg-white/5 border border-white/10">
                    <div className="flex items-center justify-between mb-3">
                      <div className="flex items-center gap-3">
                        <div className="p-2 rounded-lg bg-purple-500/20">
                          <Zap className="w-4 h-4 text-purple-400" />
                        </div>
                        <div>
                          <h4 className="font-medium">Reasoning Mode</h4>
                          <p className="text-xs text-muted-foreground">Enable chain-of-thought reasoning for smarter decisions (slower)</p>
                        </div>
                      </div>
                      <Checkbox
                        checked={editingStrategy.config.ai?.enable_reasoning ?? false}
                        onCheckedChange={(checked) => setEditingStrategy({
                          ...editingStrategy,
                          config: {
                            ...editingStrategy.config,
                            ai: {
                              ...editingStrategy.config.ai,
                              enable_reasoning: checked as boolean
                            }
                          }
                        })}
                      />
                    </div>

                    {editingStrategy.config.ai?.enable_reasoning && (
                      <div className="space-y-2 pt-3 border-t border-white/10">
                        <Label>Reasoning Model</Label>
                        <Select
                          value={editingStrategy.config.ai?.reasoning_model || 'deepseek/deepseek-r1'}
                          onValueChange={(v) => setEditingStrategy({
                            ...editingStrategy,
                            config: {
                              ...editingStrategy.config,
                              ai: {
                                ...editingStrategy.config.ai,
                                reasoning_model: v
                              }
                            }
                          })}
                        >
                          <SelectTrigger className="glass">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="deepseek/deepseek-r1">DeepSeek R1</SelectItem>
                            <SelectItem value="deepseek/deepseek-r1-0528">DeepSeek R1 (0528)</SelectItem>
                            <SelectItem value="openai/o1">OpenAI o1</SelectItem>
                            <SelectItem value="openai/o1-mini">OpenAI o1-mini</SelectItem>
                          </SelectContent>
                        </Select>
                        <p className="text-xs text-muted-foreground">
                          Reasoning models think step-by-step before deciding. Expect 10-60s response times.
                        </p>
                      </div>
                    )}
                  </div>

                  {/* Custom Prompt */}
                  <div className="space-y-2">
                    <Label>Custom AI Prompt</Label>
                    <p className="text-sm text-muted-foreground mb-2">
                      Add custom instructions for the AI trading decisions. This will be appended to the system prompt.
                    </p>
                    <Textarea
                      value={editingStrategy.config.custom_prompt}
                      onChange={(e) => setEditingStrategy({
                        ...editingStrategy,
                        config: { ...editingStrategy.config, custom_prompt: e.target.value }
                      })}
                      className="glass min-h-[120px] resize-none"
                      placeholder="Add custom instructions for the AI trading decisions..."
                    />
                  </div>
                </div>
              </CollapsibleSection>

              {/* Actions */}
              <div className="flex justify-end gap-3 pt-4">
                <Button variant="outline" onClick={() => { setEditingStrategy(null); setIsCreating(false); }}>
                  Cancel
                </Button>
                <Button onClick={handleSave}>
                  <Save className="w-4 h-4 mr-2" />
                  Save Strategy
                </Button>
              </div>
            </div>
          )
          }
        </DialogContent >
      </Dialog >
    </div >
  );
}
