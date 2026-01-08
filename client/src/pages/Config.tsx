import { useEffect, useState } from 'react';
import { motion, Reorder, useDragControls } from 'framer-motion';
import { getTraders, getStrategies, createTrader, updateTrader, deleteTrader, getSettings, updateSettings } from '../lib/api';
import type { Trader, Strategy } from '../types';
import { Plus, Pencil, Trash2, Save, Eye, EyeOff, Settings, RefreshCw, Zap, AlertTriangle, Key, Globe, GripVertical } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Checkbox } from '@/components/ui/checkbox';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { GlassCard } from '@/components/ui/glass-card';
import { GlowBadge } from '@/components/ui/glow-badge';
import { SpotlightCard } from '@/components/ui/spotlight-card';
import { useConfirm, useAlert } from '@/components/ui/confirm-modal';

interface GlobalSettings {
  openrouter_api_key: string;
  openrouter_model: string;
  binance_api_key: string;
  binance_secret_key: string;
  binance_testnet: boolean;
}

const TraderItem = ({
  trader,
  strategies,
  handleEdit,
  handleDelete
}: {
  trader: Trader;
  strategies: Strategy[];
  handleEdit: (t: Trader) => void;
  handleDelete: (id: string) => void;
}) => {
  const dragControls = useDragControls();

  return (
    <Reorder.Item
      value={trader}
      dragListener={false}
      dragControls={dragControls}
      className="relative mb-4 last:mb-0"
    >
      <SpotlightCard className="p-5 pl-12">
        {/* Drag Handle */}
        <div
          className="absolute left-3 top-1/2 -translate-y-1/2 cursor-grab active:cursor-grabbing text-muted-foreground/40 hover:text-muted-foreground p-2 touch-none"
          onPointerDown={(e) => dragControls.start(e)}
        >
          <GripVertical className="w-5 h-5" />
        </div>

        <div className="flex justify-between items-start">
          <div>
            <div className="flex items-center gap-3 mb-2">
              <div className="p-2 rounded-lg bg-primary/20">
                <Zap className="w-4 h-4 text-primary" />
              </div>
              <h3 className="font-semibold text-lg">{trader.name}</h3>
              <GlowBadge
                variant={trader.status === 'running' ? 'success' : 'secondary'}
                dot={trader.status === 'running'}
                pulse={trader.status === 'running'}
              >
                {trader.status === 'running' ? 'Running' : 'Stopped'}
              </GlowBadge>
              {trader.config?.testnet && (
                <GlowBadge variant="warning">Testnet</GlowBadge>
              )}
            </div>
            <div className="flex gap-6 text-sm text-muted-foreground">
              <span>Exchange: <span className="text-foreground">{trader.exchange}</span></span>
              <span>Strategy: <span className="text-foreground">{strategies.find(s => s.id === trader.strategy_id)?.name || 'Unknown'}</span></span>
              <span>AI: <span className="text-foreground">{trader.config?.ai_model}</span></span>
            </div>
          </div>
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="icon"
              className="glass"
              onClick={() => handleEdit(trader)}
            >
              <Pencil className="h-4 w-4" />
            </Button>
            <Button
              variant="outline"
              size="icon"
              className="glass text-red-400 hover:text-red-300"
              onClick={() => handleDelete(trader.id)}
              disabled={trader.status === 'running'}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </SpotlightCard>
    </Reorder.Item>
  );
};

export default function Config() {
  const [traders, setTraders] = useState<Trader[]>([]);
  const [strategies, setStrategies] = useState<Strategy[]>([]);
  const [loading, setLoading] = useState(true);
  const [editingTrader, setEditingTrader] = useState<Partial<Trader> | null>(null);
  const [isCreating, setIsCreating] = useState(false);
  const [showSecrets, setShowSecrets] = useState<Record<string, boolean>>({});
  const [globalSettings, setGlobalSettings] = useState<GlobalSettings>({
    openrouter_api_key: '',
    openrouter_model: 'deepseek/deepseek-v3.2',
    binance_api_key: '',
    binance_secret_key: '',
    binance_testnet: true,
  });
  const [settingsConfigured, setSettingsConfigured] = useState({ openrouter: false, binance: false });
  const [savingSettings, setSavingSettings] = useState(false);
  const { confirm, ConfirmDialog } = useConfirm();
  const { alert, AlertDialog } = useAlert();

  // Preset models list - used to detect if current model is custom
  const PRESET_MODELS = [
    'google/gemini-2.5-flash',
    'openai/gpt-oss-120b',
    'x-ai/grok-4.1-fast',
    'deepseek/deepseek-v3.2',
    'openai/gpt-5-mini',
    'openai/gpt-4.1-nano',
    'openai/gpt-4o-mini',
  ];

  useEffect(() => {
    loadData();
  }, []);

  const loadData = async () => {
    try {
      const [tradersRes, strategiesRes, settingsRes] = await Promise.all([
        getTraders(),
        getStrategies(),
        getSettings(),
      ]);
      const savedOrder = JSON.parse(localStorage.getItem('trader_order') || '[]');
      let loadedTraders = tradersRes.data.traders || [];

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
      setStrategies(strategiesRes.data.strategies || []);
      if (settingsRes.data.settings) {
        setGlobalSettings(settingsRes.data.settings);
      }
      if (settingsRes.data.configured) {
        setSettingsConfigured(settingsRes.data.configured);
      }
    } catch (err) {
      console.error('Failed to load data:', err);
    } finally {
      setLoading(false);
    }
  };

  const handleSaveSettings = async () => {
    setSavingSettings(true);
    try {
      await updateSettings(globalSettings);
      await loadData(); // Reload to get masked values
      alert({
        title: 'Settings Saved',
        description: 'Global settings have been saved successfully.',
        variant: 'success',
      });
    } catch (err: any) {
      alert({
        title: 'Error',
        description: err.response?.data?.error || 'Failed to save settings',
        variant: 'danger',
      });
    } finally {
      setSavingSettings(false);
    }
  };

  // Handle editing - auto-detect if model is custom
  const handleEdit = (trader: Trader) => {
    const isCustomModel = !!(trader.config?.ai_model && !PRESET_MODELS.includes(trader.config.ai_model));
    setEditingTrader({
      ...trader,
      config: {
        ...trader.config,
        use_custom_model: isCustomModel,
      },
    });
    setIsCreating(false);
  };

  const handleCreate = () => {
    setEditingTrader({
      name: 'New Trader',
      strategy_id: strategies[0]?.id || '',
      exchange: 'binance',
      initial_balance: 1000,
      config: {
        ai_provider: 'openrouter',
        ai_model: 'deepseek/deepseek-v3.2',
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
      alert({
        title: 'Error',
        description: err.response?.data?.error || 'Failed to save trader',
        variant: 'danger',
      });
    }
  };

  const handleDelete = async (id: string) => {
    const confirmed = await confirm({
      title: 'Delete Trader',
      description: 'Are you sure you want to delete this trader? This action cannot be undone.',
      confirmText: 'Delete',
      variant: 'danger',
    });
    if (!confirmed) return;

    try {
      console.log('Deleting trader:', id);
      await deleteTrader(id);
      console.log('Trader deleted successfully');
      await loadData();
    } catch (err: any) {
      console.error('Delete failed:', err);
      alert({
        title: 'Error',
        description: err.response?.data?.error || 'Failed to delete trader',
        variant: 'danger',
      });
    }
  };

  const toggleShowSecret = (field: string) => {
    setShowSecrets((prev) => ({ ...prev, [field]: !prev[field] }));
  };

  const handleReorder = (newOrder: Trader[]) => {
    setTraders(newOrder);
    const orderIds = newOrder.map(t => t.id);
    localStorage.setItem('trader_order', JSON.stringify(orderIds));
  };

  // Check if API key is used by another trader
  const getDuplicateApiKeyWarning = (): string | null => {
    if (!editingTrader?.config?.api_key) return null;

    const currentApiKey = editingTrader.config.api_key;
    const duplicateTraders = traders.filter(t =>
      t.id !== editingTrader.id &&
      t.config?.api_key === currentApiKey &&
      currentApiKey.length > 10 // Only check if key looks valid
    );

    if (duplicateTraders.length > 0) {
      const names = duplicateTraders.map(t => t.name).join(', ');
      return `This API key is already used by: ${names}. Using the same Binance wallet for multiple traders can cause position conflicts and unexpected behavior.`;
    }
    return null;
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
          <span className="text-muted-foreground">Loading configuration...</span>
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
            <Settings className="w-6 h-6 lg:w-8 lg:h-8" />
            Configuration
          </h1>
          <p className="text-sm lg:text-base text-muted-foreground">Manage your trading bots and API keys</p>
        </motion.div>

        <div className="flex gap-2">
          <Button variant="outline" size="icon" onClick={loadData} className="glass">
            <RefreshCw className="h-4 w-4" />
          </Button>
          <Button onClick={handleCreate} disabled={strategies.length === 0} className="hidden sm:flex">
            <Plus className="w-4 h-4 mr-2" />
            New Trader
          </Button>
          <Button onClick={handleCreate} disabled={strategies.length === 0} size="icon" className="sm:hidden">
            <Plus className="w-4 h-4" />
          </Button>
        </div>
      </div>

      {strategies.length === 0 && (
        <Alert className="glass border-yellow-500/30 bg-yellow-500/10">
          <AlertTriangle className="h-4 w-4 text-yellow-500" />
          <AlertDescription className="text-yellow-200">
            You need to create a strategy first before creating a trader. Go to the Strategies page.
          </AlertDescription>
        </Alert>
      )}

      {/* Global Settings */}
      <motion.div
        initial={{ opacity: 0, y: 20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ delay: 0.1 }}
      >
        <GlassCard className="p-5">
          <div className="flex items-center gap-3 mb-4">
            <div className="p-2 rounded-lg bg-blue-500/20">
              <Globe className="w-5 h-5 text-blue-400" />
            </div>
            <div>
              <h3 className="font-semibold text-lg">Global Settings</h3>
              <p className="text-sm text-muted-foreground">Configure API keys for OpenRouter and default Binance credentials</p>
            </div>
          </div>

          <div className="grid gap-6 lg:grid-cols-2">
            {/* OpenRouter Settings */}
            <div className="space-y-4">
              <div className="flex items-center gap-2 text-sm font-medium text-blue-400">
                <Key className="w-4 h-4" />
                OpenRouter AI
                {settingsConfigured.openrouter && (
                  <GlowBadge variant="success" className="ml-2">Configured</GlowBadge>
                )}
              </div>

              <div className="space-y-2">
                <Label>API Key</Label>
                <div className="relative">
                  <Input
                    type={showSecrets['global_openrouter'] ? 'text' : 'password'}
                    value={globalSettings.openrouter_api_key}
                    onChange={(e) => setGlobalSettings({ ...globalSettings, openrouter_api_key: e.target.value })}
                    className="glass pr-10"
                    placeholder="sk-or-..."
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="absolute right-1 top-1/2 -translate-y-1/2 h-8 w-8"
                    onClick={() => toggleShowSecret('global_openrouter')}
                  >
                    {showSecrets['global_openrouter'] ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </Button>
                </div>
              </div>

              <div className="space-y-2">
                <Label>Default Model</Label>
                <Select
                  value={globalSettings.openrouter_model}
                  onValueChange={(v) => setGlobalSettings({ ...globalSettings, openrouter_model: v })}
                >
                  <SelectTrigger className="glass">
                    <SelectValue placeholder="Select a model" />
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
            </div>

            {/* Default Binance Settings */}
            <div className="space-y-4">
              <div className="flex items-center gap-2 text-sm font-medium text-yellow-400">
                <Key className="w-4 h-4" />
                Default Binance (for Debate)
                {settingsConfigured.binance && (
                  <GlowBadge variant="success" className="ml-2">Configured</GlowBadge>
                )}
              </div>

              <div className="space-y-2">
                <Label>API Key</Label>
                <div className="relative">
                  <Input
                    type={showSecrets['global_binance_key'] ? 'text' : 'password'}
                    value={globalSettings.binance_api_key}
                    onChange={(e) => setGlobalSettings({ ...globalSettings, binance_api_key: e.target.value })}
                    className="glass pr-10"
                    placeholder="Binance API Key"
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="absolute right-1 top-1/2 -translate-y-1/2 h-8 w-8"
                    onClick={() => toggleShowSecret('global_binance_key')}
                  >
                    {showSecrets['global_binance_key'] ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </Button>
                </div>
              </div>

              <div className="space-y-2">
                <Label>Secret Key</Label>
                <div className="relative">
                  <Input
                    type={showSecrets['global_binance_secret'] ? 'text' : 'password'}
                    value={globalSettings.binance_secret_key}
                    onChange={(e) => setGlobalSettings({ ...globalSettings, binance_secret_key: e.target.value })}
                    className="glass pr-10"
                    placeholder="Binance Secret Key"
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    className="absolute right-1 top-1/2 -translate-y-1/2 h-8 w-8"
                    onClick={() => toggleShowSecret('global_binance_secret')}
                  >
                    {showSecrets['global_binance_secret'] ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </Button>
                </div>
              </div>

              <div className="flex items-center gap-2">
                <Checkbox
                  checked={globalSettings.binance_testnet}
                  onCheckedChange={(v) => setGlobalSettings({ ...globalSettings, binance_testnet: !!v })}
                />
                <Label className="cursor-pointer">Use Testnet</Label>
              </div>
            </div>
          </div>

          <div className="flex justify-end mt-6">
            <Button onClick={handleSaveSettings} disabled={savingSettings}>
              {savingSettings ? (
                <>
                  <RefreshCw className="w-4 h-4 mr-2 animate-spin" />
                  Saving...
                </>
              ) : (
                <>
                  <Save className="w-4 h-4 mr-2" />
                  Save Settings
                </>
              )}
            </Button>
          </div>
        </GlassCard>
      </motion.div>

      {/* Trader List */}
      <div className="grid gap-4">
        {traders.length === 0 ? (
          <GlassCard className="p-12 text-center">
            <Settings className="w-16 h-16 text-muted-foreground/30 mx-auto mb-4" />
            <h3 className="text-xl font-medium mb-2">No Traders Configured</h3>
            <p className="text-muted-foreground">Create a trader to start automated trading.</p>
          </GlassCard>
        ) : (
          <Reorder.Group axis="y" values={traders} onReorder={handleReorder} className="flex flex-col gap-4">
            {traders.map((trader) => (
              <TraderItem
                key={trader.id}
                trader={trader}
                strategies={strategies}
                handleEdit={handleEdit}
                handleDelete={handleDelete}
              />
            ))}
          </Reorder.Group>
        )}
      </div>

      {/* Confirmation and Alert Dialogs */}
      {ConfirmDialog}
      {AlertDialog}

      {/* Trader Editor Modal */}
      <Dialog open={!!editingTrader} onOpenChange={(open) => !open && setEditingTrader(null)}>
        <DialogContent className="max-w-2xl glass-card border-white/10 max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{isCreating ? 'Create Trader' : 'Edit Trader'}</DialogTitle>
          </DialogHeader>

          {editingTrader && (
            <div className="space-y-6 mt-4">
              {/* Basic Info */}
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label>Name</Label>
                  <Input
                    value={editingTrader.name || ''}
                    onChange={(e) => setEditingTrader({ ...editingTrader, name: e.target.value })}
                    className="glass"
                  />
                </div>
                <div className="space-y-2">
                  <Label>Strategy</Label>
                  <Select
                    value={editingTrader.strategy_id || ''}
                    onValueChange={(v) => setEditingTrader({ ...editingTrader, strategy_id: v })}
                  >
                    <SelectTrigger className="glass">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {strategies.map((s) => (
                        <SelectItem key={s.id} value={s.id}>{s.name}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>

              {/* Exchange Settings */}
              <GlassCard className="p-4">
                <h3 className="font-medium mb-4">Exchange Settings</h3>
                <div className="space-y-4">
                  {/* Binance Settings */}
                  <div className="p-3 rounded-lg border border-yellow-500/20 bg-yellow-500/5 space-y-3">
                    <div className="flex items-center gap-2 text-sm font-medium text-yellow-400">
                      <Key className="w-4 h-4" />
                      Binance Credentials
                    </div>
                    <p className="text-xs text-muted-foreground -mt-1">Leave empty to use global settings</p>

                    <div className="grid grid-cols-2 gap-4">
                      <div className="space-y-2">
                        <Label>Exchange</Label>
                        <Select
                          value={editingTrader.exchange || 'binance'}
                          onValueChange={(v) => setEditingTrader({ ...editingTrader, exchange: v })}
                        >
                          <SelectTrigger className="glass">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="binance">Binance Futures</SelectItem>
                          </SelectContent>
                        </Select>
                      </div>
                      <div className="flex items-center mt-6">
                        <div className="flex items-center gap-2">
                          <Checkbox
                            checked={editingTrader.config?.testnet ?? true}
                            onCheckedChange={(v) => setEditingTrader({
                              ...editingTrader,
                              config: { ...editingTrader.config!, testnet: !!v }
                            })}
                          />
                          <Label className="cursor-pointer">Use Testnet</Label>
                        </div>
                      </div>
                    </div>

                    <div className="space-y-2">
                      <Label>API Key</Label>
                      <div className="relative">
                        <Input
                          type={showSecrets['api_key'] ? 'text' : 'password'}
                          value={editingTrader.config?.api_key || ''}
                          onChange={(e) => setEditingTrader({
                            ...editingTrader,
                            config: { ...editingTrader.config!, api_key: e.target.value }
                          })}
                          className="glass pr-10"
                          placeholder="Leave empty for global settings"
                        />
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="absolute right-1 top-1/2 -translate-y-1/2 h-8 w-8"
                          onClick={() => toggleShowSecret('api_key')}
                        >
                          {showSecrets['api_key'] ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                        </Button>
                      </div>
                    </div>

                    <div className="space-y-2">
                      <Label>Secret Key</Label>
                      <div className="relative">
                        <Input
                          type={showSecrets['secret_key'] ? 'text' : 'password'}
                          value={editingTrader.config?.secret_key || ''}
                          onChange={(e) => setEditingTrader({
                            ...editingTrader,
                            config: { ...editingTrader.config!, secret_key: e.target.value }
                          })}
                          className="glass pr-10"
                          placeholder="Leave empty for global settings"
                        />
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="absolute right-1 top-1/2 -translate-y-1/2 h-8 w-8"
                          onClick={() => toggleShowSecret('secret_key')}
                        >
                          {showSecrets['secret_key'] ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                        </Button>
                      </div>
                    </div>

                    {/* Duplicate API key warning */}
                    {getDuplicateApiKeyWarning() && (
                      <Alert className="border-yellow-500/30 bg-yellow-500/10">
                        <AlertTriangle className="h-4 w-4 text-yellow-500" />
                        <AlertDescription className="text-yellow-200">
                          {getDuplicateApiKeyWarning()}
                        </AlertDescription>
                      </Alert>
                    )}
                  </div>

                  {/* OpenRouter Settings */}
                  <div className="p-3 rounded-lg border border-blue-500/20 bg-blue-500/5 space-y-3">
                    <div className="flex items-center gap-2 text-sm font-medium text-blue-400">
                      <Key className="w-4 h-4" />
                      OpenRouter AI
                    </div>
                    <p className="text-xs text-muted-foreground -mt-1">Leave empty to use global settings</p>

                    <div className="space-y-2">
                      <Label>API Key</Label>
                      <div className="relative">
                        <Input
                          type={showSecrets['openrouter_api_key'] ? 'text' : 'password'}
                          value={editingTrader.config?.openrouter_api_key || ''}
                          onChange={(e) => setEditingTrader({
                            ...editingTrader,
                            config: { ...editingTrader.config!, openrouter_api_key: e.target.value }
                          })}
                          className="glass pr-10"
                          placeholder="Leave empty for global settings"
                        />
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          className="absolute right-1 top-1/2 -translate-y-1/2 h-8 w-8"
                          onClick={() => toggleShowSecret('openrouter_api_key')}
                        >
                          {showSecrets['openrouter_api_key'] ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                        </Button>
                      </div>
                    </div>
                  </div>
                </div>
              </GlassCard>

              {/* AI Settings */}
              <GlassCard className="p-4">
                <h3 className="font-medium mb-4">AI Settings</h3>
                <div className="space-y-4">
                  <div className="grid grid-cols-2 gap-4">
                    <div className="space-y-2">
                      <Label>AI Provider</Label>
                      <Select
                        value={editingTrader.config?.ai_provider || 'openrouter'}
                        onValueChange={(v) => setEditingTrader({
                          ...editingTrader,
                          config: { ...editingTrader.config!, ai_provider: v }
                        })}
                      >
                        <SelectTrigger className="glass">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="openrouter">OpenRouter</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="flex items-center mt-6">
                      <div className="flex items-center gap-2">
                        <Checkbox
                          checked={editingTrader.config?.use_custom_model ?? false}
                          onCheckedChange={(v) => setEditingTrader({
                            ...editingTrader,
                            config: { ...editingTrader.config!, use_custom_model: !!v }
                          })}
                        />
                        <Label className="cursor-pointer">Use Custom Model</Label>
                      </div>
                    </div>
                  </div>

                  {editingTrader.config?.use_custom_model ? (
                    <div className="space-y-2">
                      <Label>Custom Model ID</Label>
                      <Input
                        value={editingTrader.config?.ai_model || ''}
                        onChange={(e) => setEditingTrader({
                          ...editingTrader,
                          config: { ...editingTrader.config!, ai_model: e.target.value }
                        })}
                        className="glass"
                        placeholder="e.g., anthropic/claude-3.5-sonnet, meta-llama/llama-3.1-70b"
                      />
                      <p className="text-xs text-muted-foreground">
                        Enter the full OpenRouter model ID. Find models at{' '}
                        <a href="https://openrouter.ai/models" target="_blank" rel="noopener noreferrer" className="text-primary hover:underline">
                          openrouter.ai/models
                        </a>
                      </p>
                    </div>
                  ) : (
                    <div className="space-y-2">
                      <Label>AI Model</Label>
                      <Select
                        value={editingTrader.config?.ai_model || 'deepseek/deepseek-v3.2'}
                        onValueChange={(v) => setEditingTrader({
                          ...editingTrader,
                          config: { ...editingTrader.config!, ai_model: v }
                        })}
                      >
                        <SelectTrigger className="glass">
                          <SelectValue placeholder={editingTrader.config?.ai_model || 'Select a model'} />
                        </SelectTrigger>
                        <SelectContent>
                          {/* Show current model if it's not in the predefined list */}
                          {editingTrader.config?.ai_model &&
                            !PRESET_MODELS.includes(editingTrader.config.ai_model) && (
                              <SelectItem value={editingTrader.config.ai_model}>
                                {editingTrader.config.ai_model} (current)
                              </SelectItem>
                            )}
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
                  )}

                  {/* Reasoning Mode Toggle */}
                  <div className="flex items-center justify-between p-3 rounded-lg bg-purple-500/10 border border-purple-500/20">
                    <div className="flex items-center gap-3">
                      <Zap className="w-4 h-4 text-purple-400" />
                      <div>
                        <span className="font-medium text-sm">Reasoning Mode</span>
                        <p className="text-xs text-muted-foreground">Enable chain-of-thought output parsing</p>
                      </div>
                    </div>
                    <Checkbox
                      checked={editingTrader.config?.enable_reasoning ?? false}
                      onCheckedChange={(v) => setEditingTrader({
                        ...editingTrader,
                        config: { ...editingTrader.config!, enable_reasoning: !!v }
                      })}
                    />
                  </div>
                </div>
              </GlassCard>

              <div className="flex justify-end gap-3">
                <Button variant="outline" onClick={() => { setEditingTrader(null); setIsCreating(false); }}>
                  Cancel
                </Button>
                <Button onClick={handleSave}>
                  <Save className="w-4 h-4 mr-2" />
                  Save Trader
                </Button>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}
