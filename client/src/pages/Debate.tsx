import { useEffect, useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import {
  MessageSquare,
  Play,
  Square,
  Trash2,
  RefreshCw,
  Plus,
  Users,
  CheckCircle2,
  XCircle,
  Brain,
} from 'lucide-react';
import {
  listDebates,
  createDebate,
  getDebate,
  startDebate,
  stopDebate,
  deleteDebate,
} from '../lib/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Checkbox } from '@/components/ui/checkbox';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { GlassCard } from '@/components/ui/glass-card';
import { GlowBadge } from '@/components/ui/glow-badge';
import { ProgressStat } from '@/components/ui/stat-card';
import { SpotlightCard, AnimatedBorderCard } from '@/components/ui/spotlight-card';

// Personality types with colors and emojis
const PERSONALITIES = [
  { id: 'bull', name: 'Bull', emoji: '🐂', color: '#22C55E', description: 'Optimistic, looks for long opportunities' },
  { id: 'bear', name: 'Bear', emoji: '🐻', color: '#EF4444', description: 'Skeptical, focuses on risks' },
  { id: 'analyst', name: 'Analyst', emoji: '📊', color: '#3B82F6', description: 'Neutral, data-driven analysis' },
  { id: 'contrarian', name: 'Contrarian', emoji: '🔄', color: '#F59E0B', description: 'Challenges majority opinion' },
  { id: 'risk_manager', name: 'Risk Manager', emoji: '🛡️', color: '#8B5CF6', description: 'Focuses on position sizing' },
];

const AI_MODELS = [
  { id: 'google/gemini-2.5-flash', name: 'Gemini 2.5 Flash', provider: 'openrouter' },
  { id: 'openai/gpt-oss-120b', name: 'GPT-OSS-120B', provider: 'openrouter' },
  { id: 'x-ai/grok-4.1-fast', name: 'Grok 4.1 Fast', provider: 'openrouter' },
  { id: 'deepseek/deepseek-v3.2', name: 'DeepSeek V3.2', provider: 'openrouter' },
  { id: 'openai/gpt-5-mini', name: 'GPT-5 Mini', provider: 'openrouter' },
  { id: 'openai/gpt-4.1-nano', name: 'GPT-4.1 Nano', provider: 'openrouter' },
  { id: 'openai/gpt-4o-mini', name: 'GPT-4o Mini', provider: 'openrouter' },
];

interface DebateSession {
  id: string;
  name: string;
  status: string;
  symbols: string[];
  max_rounds: number;
  current_round: number;
  created_at: string;
  participants?: any[];
  messages?: any[];
  votes?: any[];
  final_decisions?: any[];
  auto_cycle?: boolean;
  cycle_interval_minutes?: number;
  cycle_count?: number;
  next_cycle_at?: string;
}

interface Message {
  id: string;
  round: number;
  ai_model_name: string;
  personality: string;
  message_type: string;
  content: string;
  confidence: number;
  created_at: string;
}

export default function Debate() {
  const [sessions, setSessions] = useState<DebateSession[]>([]);
  const [selectedSession, setSelectedSession] = useState<string | null>(null);
  const [sessionDetails, setSessionDetails] = useState<DebateSession | null>(null);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [showCreateDialog, setShowCreateDialog] = useState(false);

  // Form state
  const [formData, setFormData] = useState({
    name: '',
    symbols: 'BTCUSDT,ETHUSDT',
    max_rounds: 3,
    auto_execute: false,
    trader_id: '',
    participants: [] as { ai_model_id: string; ai_model_name: string; provider: string; personality: string }[],
    auto_cycle: false,
    cycle_interval_minutes: 5,
  });

  // Selected model and personality for adding participants
  const [selectedModel, setSelectedModel] = useState(AI_MODELS[0].id);
  const [selectedPersonality, setSelectedPersonality] = useState(PERSONALITIES[0].id);

  useEffect(() => {
    loadData();
  }, []);

  useEffect(() => {
    if (selectedSession) {
      loadSessionDetails(selectedSession);
      const interval = setInterval(() => loadSessionDetails(selectedSession), 5000);
      return () => clearInterval(interval);
    }
  }, [selectedSession]);

  const loadData = async () => {
    try {
      const sessionsRes = await listDebates().catch(() => ({ data: { sessions: [] } }));
      setSessions(sessionsRes.data.sessions || []);
      if (sessionsRes.data.sessions?.length > 0 && !selectedSession) {
        setSelectedSession(sessionsRes.data.sessions[0].id);
      }
    } catch (err) {
      console.error('Failed to load data:', err);
    } finally {
      setLoading(false);
    }
  };

  const loadSessionDetails = async (sessionId: string) => {
    try {
      const res = await getDebate(sessionId);
      setSessionDetails(res.data);
    } catch (err) {
      console.error('Failed to load session details:', err);
    }
  };

  const handleAddParticipant = (modelId: string, personality: string) => {
    const model = AI_MODELS.find((m) => m.id === modelId);
    if (!model) return;

    setFormData({
      ...formData,
      participants: [
        ...formData.participants,
        {
          ai_model_id: model.id,
          ai_model_name: model.name,
          provider: model.provider,
          personality,
        },
      ],
    });
  };

  const handleRemoveParticipant = (index: number) => {
    setFormData({
      ...formData,
      participants: formData.participants.filter((_, i) => i !== index),
    });
  };

  const handleCreateDebate = async () => {
    if (formData.participants.length < 2) {
      alert('Please add at least 2 participants');
      return;
    }

    setCreating(true);
    try {
      const data = {
        name: formData.name || `Debate ${new Date().toLocaleDateString()}`,
        symbols: formData.symbols.split(',').map((s) => s.trim()),
        max_rounds: formData.max_rounds,
        auto_execute: formData.auto_execute,
        trader_id: formData.trader_id,
        participants: formData.participants,
        auto_cycle: formData.auto_cycle,
        cycle_interval_minutes: formData.cycle_interval_minutes,
      };
      const res = await createDebate(data);
      setSelectedSession(res.data.id || res.data.session_id);
      setShowCreateDialog(false);
      setFormData({
        name: '',
        symbols: 'BTCUSDT,ETHUSDT',
        max_rounds: 3,
        auto_execute: false,
        trader_id: '',
        participants: [],
        auto_cycle: false,
        cycle_interval_minutes: 5,
      });
      await loadData();
    } catch (err: any) {
      alert(err.response?.data?.error || 'Failed to create debate');
    } finally {
      setCreating(false);
    }
  };

  const handleStartDebate = async (sessionId: string) => {
    try {
      await startDebate(sessionId);
      await loadSessionDetails(sessionId);
    } catch (err) {
      console.error('Failed to start debate:', err);
    }
  };

  const handleStopDebate = async (sessionId: string) => {
    try {
      await stopDebate(sessionId);
      await loadSessionDetails(sessionId);
    } catch (err) {
      console.error('Failed to stop debate:', err);
    }
  };

  const handleDeleteDebate = async (sessionId: string) => {
    if (!confirm('Are you sure you want to delete this debate?')) return;
    try {
      await deleteDebate(sessionId);
      if (selectedSession === sessionId) {
        setSelectedSession(null);
        setSessionDetails(null);
      }
      await loadData();
    } catch (err) {
      console.error('Failed to delete debate:', err);
    }
  };

  const getPersonality = (id: string) => PERSONALITIES.find((p) => p.id === id);

  if (loading) {
    return (
      <div className="flex items-center justify-center h-screen">
        <div className="flex flex-col items-center gap-4">
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="relative"
          >
            <Brain className="w-12 h-12 text-primary" />
            <motion.div
              className="absolute inset-0 w-12 h-12 border-2 border-primary/30 rounded-full"
              animate={{ scale: [1, 1.3, 1], opacity: [0.5, 0, 0.5] }}
              transition={{ duration: 2, repeat: Infinity, ease: 'easeInOut' }}
            />
          </motion.div>
          <motion.span
            className="text-muted-foreground"
            animate={{ opacity: [0.5, 1, 0.5] }}
            transition={{ duration: 2, repeat: Infinity, ease: 'easeInOut' }}
          >
            Loading debates...
          </motion.span>
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
            <MessageSquare className="w-8 h-8" />
            Debate Arena
          </h1>
          <p className="text-muted-foreground">
            Multi-AI consensus trading decisions
          </p>
        </motion.div>

        <div className="flex gap-2">
          <Button variant="outline" size="icon" onClick={loadData} className="glass">
            <RefreshCw className="h-4 w-4" />
          </Button>
          <Dialog open={showCreateDialog} onOpenChange={setShowCreateDialog}>
            <DialogTrigger asChild>
              <Button>
                <Plus className="w-4 h-4 mr-2" />
                New Debate
              </Button>
            </DialogTrigger>
            <DialogContent className="max-w-2xl glass-card border-white/10">
              <DialogHeader>
                <DialogTitle>Create New Debate</DialogTitle>
              </DialogHeader>

              <div className="space-y-4 mt-4">
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label>Session Name</Label>
                    <Input
                      value={formData.name}
                      onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                      placeholder="e.g., Morning Analysis"
                      className="glass"
                    />
                  </div>
                  <div className="space-y-2">
                    <Label>Symbols</Label>
                    <Input
                      value={formData.symbols}
                      onChange={(e) => setFormData({ ...formData, symbols: e.target.value })}
                      placeholder="BTCUSDT,ETHUSDT"
                      className="glass"
                    />
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label>Max Rounds</Label>
                    <Select
                      value={formData.max_rounds.toString()}
                      onValueChange={(v) => setFormData({ ...formData, max_rounds: Number(v) })}
                    >
                      <SelectTrigger className="glass">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="1">1 Round</SelectItem>
                        <SelectItem value="2">2 Rounds</SelectItem>
                        <SelectItem value="3">3 Rounds</SelectItem>
                        <SelectItem value="5">5 Rounds</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="space-y-2">
                    <Label>Execute Trades</Label>
                    <div className="flex items-center gap-2 h-10">
                      <Checkbox
                        checked={formData.auto_execute}
                        onCheckedChange={(v) => setFormData({ ...formData, auto_execute: !!v })}
                      />
                      <span className="text-sm text-muted-foreground">
                        Auto-execute consensus decisions
                      </span>
                    </div>
                  </div>
                </div>

                {/* Auto-Cycle Settings */}
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label>Auto-Cycle</Label>
                    <div className="flex items-center gap-2 h-10">
                      <Checkbox
                        checked={formData.auto_cycle}
                        onCheckedChange={(v) => setFormData({ ...formData, auto_cycle: !!v })}
                      />
                      <span className="text-sm text-muted-foreground">
                        Run debates continuously
                      </span>
                    </div>
                  </div>
                  {formData.auto_cycle && (
                    <div className="space-y-2">
                      <Label>Cycle Interval (minutes)</Label>
                      <Select
                        value={String(formData.cycle_interval_minutes)}
                        onValueChange={(v) => setFormData({ ...formData, cycle_interval_minutes: parseInt(v) })}
                      >
                        <SelectTrigger className="glass">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="1">1 min</SelectItem>
                          <SelectItem value="3">3 mins</SelectItem>
                          <SelectItem value="5">5 mins</SelectItem>
                          <SelectItem value="10">10 mins</SelectItem>
                          <SelectItem value="15">15 mins</SelectItem>
                          <SelectItem value="30">30 mins</SelectItem>
                          <SelectItem value="60">1 hour</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                  )}
                </div>

                {/* Participants */}
                <div className="space-y-3">
                  <Label>AI Participants ({formData.participants.length})</Label>
                  <div className="flex gap-2">
                    <Select value={selectedModel} onValueChange={setSelectedModel}>
                      <SelectTrigger className="glass flex-1">
                        <SelectValue placeholder="Select AI Model" />
                      </SelectTrigger>
                      <SelectContent>
                        {AI_MODELS.map((model) => (
                          <SelectItem key={model.id} value={model.id}>
                            {model.name}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Select value={selectedPersonality} onValueChange={setSelectedPersonality}>
                      <SelectTrigger className="glass flex-1">
                        <SelectValue placeholder="Personality" />
                      </SelectTrigger>
                      <SelectContent>
                        {PERSONALITIES.map((p) => (
                          <SelectItem key={p.id} value={p.id}>
                            {p.emoji} {p.name}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Button
                      variant="default"
                      onClick={() => handleAddParticipant(selectedModel, selectedPersonality)}
                    >
                      Add
                    </Button>
                  </div>

                  {/* Quick add buttons */}
                  <div className="flex flex-wrap gap-2">
                    {PERSONALITIES.map((p) => (
                      <Button
                        key={p.id}
                        variant="outline"
                        size="sm"
                        className="glass"
                        onClick={() => handleAddParticipant(selectedModel, p.id)}
                      >
                        {p.emoji} Add {p.name}
                      </Button>
                    ))}
                  </div>

                  {/* Selected participants */}
                  <div className="space-y-2">
                    {formData.participants.map((p, i) => {
                      const personality = getPersonality(p.personality);
                      return (
                        <div
                          key={i}
                          className="flex items-center justify-between p-2 rounded-lg bg-white/5"
                          style={{ borderLeft: `3px solid ${personality?.color}` }}
                        >
                          <div className="flex items-center gap-2">
                            <span>{personality?.emoji}</span>
                            <span className="text-sm">{p.ai_model_name}</span>
                            <GlowBadge variant="secondary">{personality?.name}</GlowBadge>
                          </div>
                          <Button
                            size="icon"
                            variant="ghost"
                            className="h-6 w-6"
                            onClick={() => handleRemoveParticipant(i)}
                          >
                            <XCircle className="h-4 w-4" />
                          </Button>
                        </div>
                      );
                    })}
                  </div>
                </div>

                <Button
                  className="w-full"
                  onClick={handleCreateDebate}
                  disabled={creating || formData.participants.length < 2}
                >
                  {creating ? 'Creating...' : 'Create Debate'}
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        {/* Sessions List */}
        <GlassCard className="lg:col-span-1 p-0 overflow-hidden">
          <div className="p-4 border-b border-white/5">
            <h2 className="font-semibold flex items-center gap-2">
              <Users className="w-4 h-4 text-primary" />
              Debate Sessions
            </h2>
          </div>

          <ScrollArea className="h-[600px]">
            <div className="p-4 space-y-2">
              {sessions.length === 0 ? (
                <p className="text-sm text-muted-foreground text-center py-8">
                  No debates yet. Create one to get started.
                </p>
              ) : (
                sessions.map((session) => (
                  <motion.div
                    key={session.id}
                    initial={{ opacity: 0, x: -10 }}
                    animate={{ opacity: 1, x: 0 }}
                    className={`p-4 rounded-lg cursor-pointer transition-all ${
                      selectedSession === session.id
                        ? 'bg-primary/20 border border-primary/30'
                        : 'bg-white/5 hover:bg-white/10'
                    }`}
                    onClick={() => setSelectedSession(session.id)}
                  >
                    <div className="flex items-center justify-between mb-2">
                      <span className="font-medium">{session.name}</span>
                      <div className="flex gap-1">
                        {session.auto_cycle && (
                          <GlowBadge variant="info" pulse>
                            🔄 Auto
                          </GlowBadge>
                        )}
                        <GlowBadge
                          variant={
                            session.status === 'running'
                              ? 'info'
                              : session.status === 'completed'
                              ? 'success'
                              : session.status === 'voting'
                              ? 'warning'
                              : 'secondary'
                          }
                          pulse={session.status === 'running'}
                        >
                          {session.status}
                        </GlowBadge>
                      </div>
                    </div>
                    <div className="text-xs text-muted-foreground mb-2">
                      {session.symbols.join(', ')}
                      {session.auto_cycle && session.cycle_count ? ` • Cycle #${session.cycle_count}` : ''}
                    </div>
                    <div className="flex items-center justify-between">
                      <span className="text-xs text-muted-foreground">
                        Round {session.current_round}/{session.max_rounds}
                      </span>
                      <div className="flex gap-1">
                        {(session.status === 'pending' || (session.status === 'completed' && session.auto_cycle)) && (
                          <Button
                            size="icon"
                            className="h-6 w-6 bg-green-600 hover:bg-green-500"
                            onClick={(e) => {
                              e.stopPropagation();
                              handleStartDebate(session.id);
                            }}
                            title={session.auto_cycle ? 'Start auto-cycle' : 'Start debate'}
                          >
                            <Play className="h-3 w-3" />
                          </Button>
                        )}
                        {session.status === 'running' && (
                          <Button
                            size="icon"
                            variant="destructive"
                            className="h-6 w-6"
                            onClick={(e) => {
                              e.stopPropagation();
                              handleStopDebate(session.id);
                            }}
                          >
                            <Square className="h-3 w-3" />
                          </Button>
                        )}
                        <Button
                          size="icon"
                          variant="ghost"
                          className="h-6 w-6"
                          onClick={(e) => {
                            e.stopPropagation();
                            handleDeleteDebate(session.id);
                          }}
                        >
                          <Trash2 className="h-3 w-3" />
                        </Button>
                      </div>
                    </div>
                  </motion.div>
                ))
              )}
            </div>
          </ScrollArea>
        </GlassCard>

        {/* Debate Content */}
        <div className="lg:col-span-2 space-y-6">
          {sessionDetails ? (
            <>
              {/* Participants */}
              <div className="flex gap-3 flex-wrap">
                {sessionDetails.participants?.map((p: any) => {
                  const personality = getPersonality(p.personality);
                  return (
                    <motion.div
                      key={p.id}
                      initial={{ opacity: 0, scale: 0.9 }}
                      animate={{ opacity: 1, scale: 1 }}
                      className="flex items-center gap-2 px-4 py-2 rounded-xl glass"
                      style={{ borderLeft: `3px solid ${personality?.color}` }}
                    >
                      <span className="text-xl">{personality?.emoji}</span>
                      <div>
                        <p className="text-sm font-medium">{p.ai_model_name}</p>
                        <p className="text-xs text-muted-foreground">{personality?.name}</p>
                      </div>
                    </motion.div>
                  );
                })}
              </div>

              {/* Messages Feed */}
              <GlassCard className="p-0 overflow-hidden">
                <div className="p-4 border-b border-white/5">
                  <div className="flex items-center justify-between">
                    <div>
                      <h2 className="font-semibold flex items-center gap-2">
                        Debate Feed
                        {sessionDetails.status === 'running' && (
                          <motion.span
                            className="w-2 h-2 bg-primary rounded-full"
                            animate={{ opacity: [0.3, 1, 0.3] }}
                            transition={{ duration: 1.5, repeat: Infinity, ease: 'easeInOut' }}
                          />
                        )}
                      </h2>
                      <p className="text-sm text-muted-foreground">
                        Round {sessionDetails.current_round} of {sessionDetails.max_rounds}
                      </p>
                    </div>
                    {sessionDetails.status === 'running' && (
                      <GlowBadge variant="info" pulse glow>
                        <Brain className="w-3 h-3 mr-1" />
                        AI Thinking...
                      </GlowBadge>
                    )}
                    {sessionDetails.status === 'completed' && (
                      <GlowBadge variant="success" glow>
                        <CheckCircle2 className="w-3 h-3 mr-1" />
                        Completed
                      </GlowBadge>
                    )}
                  </div>
                  {/* Progress bar */}
                  {sessionDetails.max_rounds > 0 && (
                    <div className="mt-3">
                      <div className="flex justify-between text-xs text-muted-foreground mb-1">
                        <span>Progress</span>
                        <span>{Math.round((sessionDetails.current_round / sessionDetails.max_rounds) * 100)}%</span>
                      </div>
                      <div className="h-2 bg-white/10 rounded-full overflow-hidden">
                        <motion.div
                          className="h-full bg-gradient-to-r from-primary to-blue-400"
                          initial={{ width: 0 }}
                          animate={{ width: `${(sessionDetails.current_round / sessionDetails.max_rounds) * 100}%` }}
                          transition={{ duration: 0.5 }}
                        />
                      </div>
                    </div>
                  )}
                </div>

                <ScrollArea className="h-[400px]">
                  <div className="p-4 space-y-4">
                    {sessionDetails.messages?.length === 0 ? (
                      <div className="text-center py-12">
                        {sessionDetails.status === 'running' ? (
                          <>
                            <motion.div
                              className="flex items-center justify-center gap-2 mb-4"
                              initial={{ opacity: 0 }}
                              animate={{ opacity: 1 }}
                            >
                              <Brain className="w-12 h-12 text-primary" />
                              <motion.div
                                className="absolute w-16 h-16 border-2 border-primary/20 rounded-full"
                                animate={{ scale: [1, 1.2, 1], opacity: [0.5, 0, 0.5] }}
                                transition={{ duration: 2, repeat: Infinity, ease: 'easeInOut' }}
                              />
                            </motion.div>
                            <motion.p
                              className="text-muted-foreground"
                              animate={{ opacity: [0.5, 1, 0.5] }}
                              transition={{ duration: 2, repeat: Infinity, ease: 'easeInOut' }}
                            >
                              AI participants are thinking...
                            </motion.p>
                            <p className="text-sm text-muted-foreground/60 mt-1">This may take a moment</p>
                          </>
                        ) : (
                          <>
                            <MessageSquare className="w-12 h-12 text-muted-foreground/30 mx-auto mb-4" />
                            <p className="text-muted-foreground">
                              {sessionDetails.status === 'pending'
                                ? 'Start the debate to see messages'
                                : 'Waiting for messages...'}
                            </p>
                          </>
                        )}
                      </div>
                    ) : (
                      <AnimatePresence>
                        {sessionDetails.messages?.map((msg: Message) => {
                          const personality = getPersonality(msg.personality);
                          return (
                            <motion.div
                              key={msg.id}
                              initial={{ opacity: 0, y: 20 }}
                              animate={{ opacity: 1, y: 0 }}
                              className="rounded-lg bg-white/5 overflow-hidden"
                              style={{ borderLeft: `3px solid ${personality?.color}` }}
                            >
                              <div className="p-4">
                                <div className="flex items-center justify-between mb-2">
                                  <div className="flex items-center gap-2">
                                    <span className="text-lg">{personality?.emoji}</span>
                                    <span className="font-medium">{msg.ai_model_name}</span>
                                    <GlowBadge variant="secondary">{msg.message_type}</GlowBadge>
                                  </div>
                                  <span className="text-xs text-muted-foreground">
                                    Round {msg.round}
                                  </span>
                                </div>
                                <p className="text-sm text-muted-foreground whitespace-pre-wrap">
                                  {msg.content}
                                </p>
                                {msg.confidence > 0 && (
                                  <div className="mt-3">
                                    <ProgressStat
                                      label="Confidence"
                                      value={msg.confidence}
                                      max={100}
                                      color={
                                        msg.confidence >= 70
                                          ? 'success'
                                          : msg.confidence >= 40
                                          ? 'warning'
                                          : 'danger'
                                      }
                                    />
                                  </div>
                                )}
                              </div>
                            </motion.div>
                          );
                        })}
                        {/* Typing indicator when debate is running */}
                        {sessionDetails.status === 'running' && (
                          <motion.div
                            initial={{ opacity: 0 }}
                            animate={{ opacity: 1 }}
                            className="flex items-center gap-3 p-4 rounded-lg bg-white/5 border border-white/10"
                          >
                            <div className="flex gap-1.5">
                              <motion.div
                                className="w-2 h-2 bg-primary rounded-full"
                                animate={{ opacity: [0.3, 1, 0.3] }}
                                transition={{ duration: 1.2, repeat: Infinity, delay: 0 }}
                              />
                              <motion.div
                                className="w-2 h-2 bg-primary rounded-full"
                                animate={{ opacity: [0.3, 1, 0.3] }}
                                transition={{ duration: 1.2, repeat: Infinity, delay: 0.3 }}
                              />
                              <motion.div
                                className="w-2 h-2 bg-primary rounded-full"
                                animate={{ opacity: [0.3, 1, 0.3] }}
                                transition={{ duration: 1.2, repeat: Infinity, delay: 0.6 }}
                              />
                            </div>
                            <motion.span
                              className="text-sm text-muted-foreground"
                              animate={{ opacity: [0.6, 1, 0.6] }}
                              transition={{ duration: 2, repeat: Infinity, ease: 'easeInOut' }}
                            >
                              AI is formulating response...
                            </motion.span>
                          </motion.div>
                        )}
                      </AnimatePresence>
                    )}
                  </div>
                </ScrollArea>
              </GlassCard>

              {/* Final Decisions */}
              {sessionDetails.final_decisions && sessionDetails.final_decisions.length > 0 && (
                <AnimatedBorderCard className="p-6">
                  <h3 className="font-semibold mb-4 flex items-center gap-2">
                    <CheckCircle2 className="w-5 h-5 text-green-400" />
                    Final Consensus Decisions
                  </h3>
                  <div className="grid gap-3">
                    {sessionDetails.final_decisions.map((dec: any, i: number) => (
                      <SpotlightCard key={i} className="p-4">
                        <div className="flex items-center justify-between mb-2">
                          <span className="font-semibold">{dec.symbol}</span>
                          <GlowBadge
                            variant={
                              dec.action.includes('long')
                                ? 'success'
                                : dec.action.includes('short')
                                ? 'danger'
                                : 'secondary'
                            }
                            glow
                          >
                            {dec.action.toUpperCase()}
                          </GlowBadge>
                        </div>
                        <div className="grid grid-cols-3 gap-4 text-sm mb-2">
                          <div>
                            <span className="text-muted-foreground">Confidence</span>
                            <p className="font-medium">{dec.confidence}%</p>
                          </div>
                          <div>
                            <span className="text-muted-foreground">Leverage</span>
                            <p className="font-medium">{dec.leverage}x</p>
                          </div>
                          <div>
                            <span className="text-muted-foreground">Position %</span>
                            <p className="font-medium">{dec.position_pct}%</p>
                          </div>
                        </div>
                        <p className="text-sm text-muted-foreground">{dec.reasoning}</p>
                      </SpotlightCard>
                    ))}
                  </div>
                </AnimatedBorderCard>
              )}
            </>
          ) : (
            <GlassCard className="p-12 text-center">
              <MessageSquare className="w-16 h-16 text-muted-foreground/30 mx-auto mb-4" />
              <h3 className="text-xl font-medium mb-2">No Debate Selected</h3>
              <p className="text-muted-foreground">
                Create a new debate or select one from the list to view the discussion.
              </p>
            </GlassCard>
          )}
        </div>
      </div>
    </div>
  );
}
