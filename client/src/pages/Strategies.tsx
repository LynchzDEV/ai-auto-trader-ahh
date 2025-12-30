import { useEffect, useState } from 'react';
import { getStrategies, createStrategy, updateStrategy, deleteStrategy, getDefaultConfig } from '../lib/api';
import type { Strategy, StrategyConfig } from '../types';
import { Plus, Pencil, Trash2, X, Save } from 'lucide-react';

export default function Strategies() {
  const [strategies, setStrategies] = useState<Strategy[]>([]);
  const [loading, setLoading] = useState(true);
  const [editingStrategy, setEditingStrategy] = useState<Strategy | null>(null);
  const [isCreating, setIsCreating] = useState(false);
  const [defaultConfig, setDefaultConfig] = useState<StrategyConfig | null>(null);

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
      alert(err.response?.data?.error || 'Failed to save strategy');
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Are you sure you want to delete this strategy?')) return;
    try {
      await deleteStrategy(id);
      loadStrategies();
    } catch (err: any) {
      alert(err.response?.data?.error || 'Failed to delete strategy');
    }
  };

  if (loading) {
    return <div className="p-8 text-center">Loading...</div>;
  }

  return (
    <div className="p-6 space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold">Strategies</h1>
        <button
          onClick={handleCreate}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 rounded-lg"
        >
          <Plus size={20} />
          New Strategy
        </button>
      </div>

      {/* Strategy List */}
      <div className="grid gap-4">
        {strategies.length === 0 ? (
          <div className="bg-slate-800 rounded-lg p-8 text-center text-slate-400">
            No strategies yet. Create one to get started.
          </div>
        ) : (
          strategies.map((strategy) => (
            <div
              key={strategy.id}
              className="bg-slate-800 rounded-lg p-4 hover:bg-slate-750 transition-colors"
            >
              <div className="flex justify-between items-start">
                <div>
                  <h3 className="font-semibold text-lg">{strategy.name}</h3>
                  <p className="text-slate-400 text-sm mt-1">{strategy.description || 'No description'}</p>
                  <div className="flex gap-4 mt-3 text-sm text-slate-400">
                    <span>Interval: {strategy.config.trading_interval}min</span>
                    <span>Max Positions: {strategy.config.risk_control.max_positions}</span>
                    <span>Min Confidence: {strategy.config.risk_control.min_confidence}%</span>
                  </div>
                </div>
                <div className="flex gap-2">
                  <button
                    onClick={() => { setEditingStrategy(strategy); setIsCreating(false); }}
                    className="p-2 hover:bg-slate-700 rounded"
                  >
                    <Pencil size={18} />
                  </button>
                  <button
                    onClick={() => handleDelete(strategy.id)}
                    className="p-2 hover:bg-slate-700 rounded text-red-400"
                  >
                    <Trash2 size={18} />
                  </button>
                </div>
              </div>
            </div>
          ))
        )}
      </div>

      {/* Strategy Editor Modal */}
      {editingStrategy && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-slate-800 rounded-lg w-full max-w-4xl max-h-[90vh] overflow-y-auto m-4">
            <div className="flex justify-between items-center p-4 border-b border-slate-700">
              <h2 className="text-xl font-semibold">
                {isCreating ? 'Create Strategy' : 'Edit Strategy'}
              </h2>
              <button onClick={() => { setEditingStrategy(null); setIsCreating(false); }} className="p-2 hover:bg-slate-700 rounded">
                <X size={20} />
              </button>
            </div>

            <div className="p-4 space-y-6">
              {/* Basic Info */}
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm text-slate-400 mb-1">Name</label>
                  <input
                    type="text"
                    value={editingStrategy.name}
                    onChange={(e) => setEditingStrategy({ ...editingStrategy, name: e.target.value })}
                    className="w-full bg-slate-700 rounded px-3 py-2"
                  />
                </div>
                <div>
                  <label className="block text-sm text-slate-400 mb-1">Description</label>
                  <input
                    type="text"
                    value={editingStrategy.description}
                    onChange={(e) => setEditingStrategy({ ...editingStrategy, description: e.target.value })}
                    className="w-full bg-slate-700 rounded px-3 py-2"
                  />
                </div>
              </div>

              {/* Coin Source */}
              <div className="bg-slate-700/50 rounded-lg p-4">
                <h3 className="font-medium mb-3">Coin Source</h3>
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="block text-sm text-slate-400 mb-1">Source Type</label>
                    <select
                      value={editingStrategy.config.coin_source.source_type}
                      onChange={(e) => setEditingStrategy({
                        ...editingStrategy,
                        config: {
                          ...editingStrategy.config,
                          coin_source: { ...editingStrategy.config.coin_source, source_type: e.target.value }
                        }
                      })}
                      className="w-full bg-slate-700 rounded px-3 py-2"
                    >
                      <option value="static">Static List</option>
                      <option value="top_volume">Top by Volume</option>
                    </select>
                  </div>
                  <div>
                    <label className="block text-sm text-slate-400 mb-1">Trading Pairs (comma separated)</label>
                    <input
                      type="text"
                      value={editingStrategy.config.coin_source.static_coins.join(', ')}
                      onChange={(e) => setEditingStrategy({
                        ...editingStrategy,
                        config: {
                          ...editingStrategy.config,
                          coin_source: {
                            ...editingStrategy.config.coin_source,
                            static_coins: e.target.value.split(',').map(s => s.trim()).filter(Boolean)
                          }
                        }
                      })}
                      className="w-full bg-slate-700 rounded px-3 py-2"
                      placeholder="BTCUSDT, ETHUSDT"
                    />
                  </div>
                </div>
              </div>

              {/* Indicators */}
              <div className="bg-slate-700/50 rounded-lg p-4">
                <h3 className="font-medium mb-3">Technical Indicators</h3>
                <div className="grid grid-cols-3 gap-4 mb-4">
                  <div>
                    <label className="block text-sm text-slate-400 mb-1">Timeframe</label>
                    <select
                      value={editingStrategy.config.indicators.primary_timeframe}
                      onChange={(e) => setEditingStrategy({
                        ...editingStrategy,
                        config: {
                          ...editingStrategy.config,
                          indicators: { ...editingStrategy.config.indicators, primary_timeframe: e.target.value }
                        }
                      })}
                      className="w-full bg-slate-700 rounded px-3 py-2"
                    >
                      <option value="1m">1 minute</option>
                      <option value="5m">5 minutes</option>
                      <option value="15m">15 minutes</option>
                      <option value="1h">1 hour</option>
                      <option value="4h">4 hours</option>
                    </select>
                  </div>
                  <div>
                    <label className="block text-sm text-slate-400 mb-1">Kline Count</label>
                    <input
                      type="number"
                      value={editingStrategy.config.indicators.kline_count}
                      onChange={(e) => setEditingStrategy({
                        ...editingStrategy,
                        config: {
                          ...editingStrategy.config,
                          indicators: { ...editingStrategy.config.indicators, kline_count: parseInt(e.target.value) }
                        }
                      })}
                      className="w-full bg-slate-700 rounded px-3 py-2"
                    />
                  </div>
                  <div>
                    <label className="block text-sm text-slate-400 mb-1">Trading Interval (min)</label>
                    <input
                      type="number"
                      value={editingStrategy.config.trading_interval}
                      onChange={(e) => setEditingStrategy({
                        ...editingStrategy,
                        config: { ...editingStrategy.config, trading_interval: parseInt(e.target.value) }
                      })}
                      className="w-full bg-slate-700 rounded px-3 py-2"
                    />
                  </div>
                </div>
                <div className="flex flex-wrap gap-4">
                  {[
                    { key: 'enable_ema', label: 'EMA' },
                    { key: 'enable_macd', label: 'MACD' },
                    { key: 'enable_rsi', label: 'RSI' },
                    { key: 'enable_atr', label: 'ATR' },
                    { key: 'enable_boll', label: 'Bollinger' },
                    { key: 'enable_volume', label: 'Volume' },
                  ].map((ind) => (
                    <label key={ind.key} className="flex items-center gap-2 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={(editingStrategy.config.indicators as any)[ind.key]}
                        onChange={(e) => setEditingStrategy({
                          ...editingStrategy,
                          config: {
                            ...editingStrategy.config,
                            indicators: { ...editingStrategy.config.indicators, [ind.key]: e.target.checked }
                          }
                        })}
                        className="rounded"
                      />
                      <span>{ind.label}</span>
                    </label>
                  ))}
                </div>
              </div>

              {/* Risk Control */}
              <div className="bg-slate-700/50 rounded-lg p-4">
                <h3 className="font-medium mb-3">Risk Control</h3>
                <div className="grid grid-cols-3 gap-4">
                  <div>
                    <label className="block text-sm text-slate-400 mb-1">Max Positions</label>
                    <input
                      type="number"
                      value={editingStrategy.config.risk_control.max_positions}
                      onChange={(e) => setEditingStrategy({
                        ...editingStrategy,
                        config: {
                          ...editingStrategy.config,
                          risk_control: { ...editingStrategy.config.risk_control, max_positions: parseInt(e.target.value) }
                        }
                      })}
                      className="w-full bg-slate-700 rounded px-3 py-2"
                    />
                  </div>
                  <div>
                    <label className="block text-sm text-slate-400 mb-1">Max Leverage</label>
                    <input
                      type="number"
                      value={editingStrategy.config.risk_control.max_leverage}
                      onChange={(e) => setEditingStrategy({
                        ...editingStrategy,
                        config: {
                          ...editingStrategy.config,
                          risk_control: { ...editingStrategy.config.risk_control, max_leverage: parseInt(e.target.value) }
                        }
                      })}
                      className="w-full bg-slate-700 rounded px-3 py-2"
                    />
                  </div>
                  <div>
                    <label className="block text-sm text-slate-400 mb-1">Max Position % of Balance</label>
                    <input
                      type="number"
                      value={editingStrategy.config.risk_control.max_position_percent}
                      onChange={(e) => setEditingStrategy({
                        ...editingStrategy,
                        config: {
                          ...editingStrategy.config,
                          risk_control: { ...editingStrategy.config.risk_control, max_position_percent: parseFloat(e.target.value) }
                        }
                      })}
                      className="w-full bg-slate-700 rounded px-3 py-2"
                    />
                  </div>
                  <div>
                    <label className="block text-sm text-slate-400 mb-1">Min Position USD</label>
                    <input
                      type="number"
                      value={editingStrategy.config.risk_control.min_position_usd}
                      onChange={(e) => setEditingStrategy({
                        ...editingStrategy,
                        config: {
                          ...editingStrategy.config,
                          risk_control: { ...editingStrategy.config.risk_control, min_position_usd: parseFloat(e.target.value) }
                        }
                      })}
                      className="w-full bg-slate-700 rounded px-3 py-2"
                    />
                  </div>
                  <div>
                    <label className="block text-sm text-slate-400 mb-1">Min Confidence %</label>
                    <input
                      type="number"
                      value={editingStrategy.config.risk_control.min_confidence}
                      onChange={(e) => setEditingStrategy({
                        ...editingStrategy,
                        config: {
                          ...editingStrategy.config,
                          risk_control: { ...editingStrategy.config.risk_control, min_confidence: parseInt(e.target.value) }
                        }
                      })}
                      className="w-full bg-slate-700 rounded px-3 py-2"
                    />
                  </div>
                  <div>
                    <label className="block text-sm text-slate-400 mb-1">Min Risk/Reward Ratio</label>
                    <input
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
                      className="w-full bg-slate-700 rounded px-3 py-2"
                    />
                  </div>
                </div>
              </div>

              {/* Custom Prompt */}
              <div className="bg-slate-700/50 rounded-lg p-4">
                <h3 className="font-medium mb-3">Custom AI Prompt (Optional)</h3>
                <textarea
                  value={editingStrategy.config.custom_prompt}
                  onChange={(e) => setEditingStrategy({
                    ...editingStrategy,
                    config: { ...editingStrategy.config, custom_prompt: e.target.value }
                  })}
                  className="w-full bg-slate-700 rounded px-3 py-2 h-32 resize-none"
                  placeholder="Add custom instructions for the AI trading decisions..."
                />
              </div>
            </div>

            <div className="flex justify-end gap-3 p-4 border-t border-slate-700">
              <button
                onClick={() => { setEditingStrategy(null); setIsCreating(false); }}
                className="px-4 py-2 hover:bg-slate-700 rounded-lg"
              >
                Cancel
              </button>
              <button
                onClick={handleSave}
                className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 rounded-lg"
              >
                <Save size={18} />
                Save Strategy
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
