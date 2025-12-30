import { useEffect, useState } from 'react';
import { getTraders, getStrategies, createTrader, updateTrader, deleteTrader } from '../lib/api';
import type { Trader, Strategy } from '../types';
import { Plus, Pencil, Trash2, X, Save, Eye, EyeOff } from 'lucide-react';

export default function Config() {
  const [traders, setTraders] = useState<Trader[]>([]);
  const [strategies, setStrategies] = useState<Strategy[]>([]);
  const [loading, setLoading] = useState(true);
  const [editingTrader, setEditingTrader] = useState<Partial<Trader> | null>(null);
  const [isCreating, setIsCreating] = useState(false);
  const [showSecrets, setShowSecrets] = useState<Record<string, boolean>>({});

  useEffect(() => {
    loadData();
  }, []);

  const loadData = async () => {
    try {
      const [tradersRes, strategiesRes] = await Promise.all([
        getTraders(),
        getStrategies(),
      ]);
      setTraders(tradersRes.data.traders || []);
      setStrategies(strategiesRes.data.strategies || []);
    } catch (err) {
      console.error('Failed to load data:', err);
    } finally {
      setLoading(false);
    }
  };

  const handleCreate = () => {
    setEditingTrader({
      name: 'New Trader',
      strategy_id: strategies[0]?.id || '',
      exchange: 'binance',
      initial_balance: 1000,
      config: {
        ai_provider: 'openrouter',
        ai_model: 'anthropic/claude-sonnet-4',
        api_key: '',
        secret_key: '',
        testnet: true,
      },
    });
    setIsCreating(true);
  };

  const handleSave = async () => {
    if (!editingTrader) return;
    try {
      if (isCreating) {
        await createTrader(editingTrader);
      } else {
        await updateTrader(editingTrader.id!, editingTrader);
      }
      setEditingTrader(null);
      setIsCreating(false);
      loadData();
    } catch (err: any) {
      alert(err.response?.data?.error || 'Failed to save trader');
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Are you sure you want to delete this trader?')) return;
    try {
      await deleteTrader(id);
      loadData();
    } catch (err: any) {
      alert(err.response?.data?.error || 'Failed to delete trader');
    }
  };

  const toggleShowSecret = (field: string) => {
    setShowSecrets((prev) => ({ ...prev, [field]: !prev[field] }));
  };

  if (loading) {
    return <div className="p-8 text-center">Loading...</div>;
  }

  return (
    <div className="p-6 space-y-6">
      <div className="flex justify-between items-center">
        <h1 className="text-2xl font-bold">Configuration</h1>
        <button
          onClick={handleCreate}
          className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 rounded-lg"
          disabled={strategies.length === 0}
        >
          <Plus size={20} />
          New Trader
        </button>
      </div>

      {strategies.length === 0 && (
        <div className="bg-yellow-900/30 border border-yellow-600 rounded-lg p-4 text-yellow-200">
          You need to create a strategy first before creating a trader. Go to the Strategies page.
        </div>
      )}

      {/* Trader List */}
      <div className="grid gap-4">
        {traders.length === 0 ? (
          <div className="bg-slate-800 rounded-lg p-8 text-center text-slate-400">
            No traders configured yet. Create one to get started.
          </div>
        ) : (
          traders.map((trader) => (
            <div
              key={trader.id}
              className="bg-slate-800 rounded-lg p-4"
            >
              <div className="flex justify-between items-start">
                <div>
                  <div className="flex items-center gap-3">
                    <h3 className="font-semibold text-lg">{trader.name}</h3>
                    <span className={`px-2 py-0.5 text-xs rounded ${
                      trader.is_running ? 'bg-green-600' : 'bg-slate-600'
                    }`}>
                      {trader.is_running ? 'Running' : 'Stopped'}
                    </span>
                    {trader.config?.testnet && (
                      <span className="px-2 py-0.5 text-xs rounded bg-yellow-600">Testnet</span>
                    )}
                  </div>
                  <div className="flex gap-4 mt-2 text-sm text-slate-400">
                    <span>Exchange: {trader.exchange}</span>
                    <span>Strategy: {strategies.find(s => s.id === trader.strategy_id)?.name || 'Unknown'}</span>
                    <span>AI: {trader.config?.ai_model}</span>
                  </div>
                </div>
                <div className="flex gap-2">
                  <button
                    onClick={() => { setEditingTrader(trader); setIsCreating(false); }}
                    className="p-2 hover:bg-slate-700 rounded"
                  >
                    <Pencil size={18} />
                  </button>
                  <button
                    onClick={() => handleDelete(trader.id)}
                    className="p-2 hover:bg-slate-700 rounded text-red-400"
                    disabled={trader.is_running}
                  >
                    <Trash2 size={18} />
                  </button>
                </div>
              </div>
            </div>
          ))
        )}
      </div>

      {/* Trader Editor Modal */}
      {editingTrader && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-slate-800 rounded-lg w-full max-w-2xl max-h-[90vh] overflow-y-auto m-4">
            <div className="flex justify-between items-center p-4 border-b border-slate-700">
              <h2 className="text-xl font-semibold">
                {isCreating ? 'Create Trader' : 'Edit Trader'}
              </h2>
              <button onClick={() => { setEditingTrader(null); setIsCreating(false); }} className="p-2 hover:bg-slate-700 rounded">
                <X size={20} />
              </button>
            </div>

            <div className="p-4 space-y-4">
              {/* Basic Info */}
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm text-slate-400 mb-1">Name</label>
                  <input
                    type="text"
                    value={editingTrader.name || ''}
                    onChange={(e) => setEditingTrader({ ...editingTrader, name: e.target.value })}
                    className="w-full bg-slate-700 rounded px-3 py-2"
                  />
                </div>
                <div>
                  <label className="block text-sm text-slate-400 mb-1">Strategy</label>
                  <select
                    value={editingTrader.strategy_id || ''}
                    onChange={(e) => setEditingTrader({ ...editingTrader, strategy_id: e.target.value })}
                    className="w-full bg-slate-700 rounded px-3 py-2"
                  >
                    {strategies.map((s) => (
                      <option key={s.id} value={s.id}>{s.name}</option>
                    ))}
                  </select>
                </div>
              </div>

              {/* Exchange Settings */}
              <div className="bg-slate-700/50 rounded-lg p-4">
                <h3 className="font-medium mb-3">Exchange Settings</h3>
                <div className="space-y-4">
                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <label className="block text-sm text-slate-400 mb-1">Exchange</label>
                      <select
                        value={editingTrader.exchange || 'binance'}
                        onChange={(e) => setEditingTrader({ ...editingTrader, exchange: e.target.value })}
                        className="w-full bg-slate-700 rounded px-3 py-2"
                      >
                        <option value="binance">Binance Futures</option>
                      </select>
                    </div>
                    <div className="flex items-center">
                      <label className="flex items-center gap-2 cursor-pointer mt-6">
                        <input
                          type="checkbox"
                          checked={editingTrader.config?.testnet ?? true}
                          onChange={(e) => setEditingTrader({
                            ...editingTrader,
                            config: { ...editingTrader.config!, testnet: e.target.checked }
                          })}
                          className="rounded"
                        />
                        <span>Use Testnet</span>
                      </label>
                    </div>
                  </div>
                  <div>
                    <label className="block text-sm text-slate-400 mb-1">API Key</label>
                    <div className="relative">
                      <input
                        type={showSecrets['api_key'] ? 'text' : 'password'}
                        value={editingTrader.config?.api_key || ''}
                        onChange={(e) => setEditingTrader({
                          ...editingTrader,
                          config: { ...editingTrader.config!, api_key: e.target.value }
                        })}
                        className="w-full bg-slate-700 rounded px-3 py-2 pr-10"
                        placeholder="Your Binance API Key"
                      />
                      <button
                        type="button"
                        onClick={() => toggleShowSecret('api_key')}
                        className="absolute right-2 top-1/2 -translate-y-1/2 p-1 hover:bg-slate-600 rounded"
                      >
                        {showSecrets['api_key'] ? <EyeOff size={18} /> : <Eye size={18} />}
                      </button>
                    </div>
                  </div>
                  <div>
                    <label className="block text-sm text-slate-400 mb-1">Secret Key</label>
                    <div className="relative">
                      <input
                        type={showSecrets['secret_key'] ? 'text' : 'password'}
                        value={editingTrader.config?.secret_key || ''}
                        onChange={(e) => setEditingTrader({
                          ...editingTrader,
                          config: { ...editingTrader.config!, secret_key: e.target.value }
                        })}
                        className="w-full bg-slate-700 rounded px-3 py-2 pr-10"
                        placeholder="Your Binance Secret Key"
                      />
                      <button
                        type="button"
                        onClick={() => toggleShowSecret('secret_key')}
                        className="absolute right-2 top-1/2 -translate-y-1/2 p-1 hover:bg-slate-600 rounded"
                      >
                        {showSecrets['secret_key'] ? <EyeOff size={18} /> : <Eye size={18} />}
                      </button>
                    </div>
                  </div>
                </div>
              </div>

              {/* AI Settings */}
              <div className="bg-slate-700/50 rounded-lg p-4">
                <h3 className="font-medium mb-3">AI Settings</h3>
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="block text-sm text-slate-400 mb-1">AI Provider</label>
                    <select
                      value={editingTrader.config?.ai_provider || 'openrouter'}
                      onChange={(e) => setEditingTrader({
                        ...editingTrader,
                        config: { ...editingTrader.config!, ai_provider: e.target.value }
                      })}
                      className="w-full bg-slate-700 rounded px-3 py-2"
                    >
                      <option value="openrouter">OpenRouter</option>
                    </select>
                  </div>
                  <div>
                    <label className="block text-sm text-slate-400 mb-1">AI Model</label>
                    <select
                      value={editingTrader.config?.ai_model || 'anthropic/claude-sonnet-4'}
                      onChange={(e) => setEditingTrader({
                        ...editingTrader,
                        config: { ...editingTrader.config!, ai_model: e.target.value }
                      })}
                      className="w-full bg-slate-700 rounded px-3 py-2"
                    >
                      <option value="anthropic/claude-sonnet-4">Claude Sonnet 4</option>
                      <option value="anthropic/claude-3.5-sonnet">Claude 3.5 Sonnet</option>
                      <option value="openai/gpt-4o">GPT-4o</option>
                      <option value="openai/gpt-4o-mini">GPT-4o Mini</option>
                      <option value="google/gemini-pro-1.5">Gemini Pro 1.5</option>
                      <option value="deepseek/deepseek-chat">DeepSeek Chat</option>
                    </select>
                  </div>
                </div>
              </div>

              <div className="text-sm text-slate-400">
                Note: OpenRouter API key is configured via environment variable (OPENROUTER_API_KEY).
              </div>
            </div>

            <div className="flex justify-end gap-3 p-4 border-t border-slate-700">
              <button
                onClick={() => { setEditingTrader(null); setIsCreating(false); }}
                className="px-4 py-2 hover:bg-slate-700 rounded-lg"
              >
                Cancel
              </button>
              <button
                onClick={handleSave}
                className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 rounded-lg"
              >
                <Save size={18} />
                Save Trader
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
