import { useEffect, useState, useMemo, useRef } from 'react';
import { motion, useSpring, useTransform, MotionValue } from 'framer-motion';
import * as d3 from 'd3-force';
import {
    Trophy,
    Medal,
    TrendingUp,
    TrendingDown,
    RefreshCw,
    Info,
} from 'lucide-react';
import { getTraders, getTrades } from '../lib/api';
import { Button } from '@/components/ui/button';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { GlassCard } from '@/components/ui/glass-card';
import { StatCard } from '@/components/ui/stat-card';
import {
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
} from '@/components/ui/tooltip';

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

interface SymbolProfit {
    symbol: string;
    pnl: number;
    tradeCount: number;
    winCount: number;
    lossCount: number;
    winRate: number;
}

interface BubbleData {
    id: string;
    symbol: string;
    pnl: number;
    tradeCount: number;
    winCount: number;
    lossCount: number;
    winRate: number;
    size: number;
    color: string;
    // D3 properties
    x?: number;
    y?: number;
    fx?: number | null;
    fy?: number | null;
    vx?: number;
    vy?: number;
}

// Curated palette of distinct, vibrant colors
const SYMBOL_COLORS = [
    '#FF6B6B', // Coral Red
    '#4ECDC4', // Teal
    '#45B7D1', // Sky Blue
    '#96E6A1', // Mint Green
    '#DDA0DD', // Plum
    '#F7DC6F', // Sunflower
    '#BB8FCE', // Lavender
    '#85C1E9', // Light Blue
    '#F8B500', // Golden Yellow
    '#00D4AA', // Turquoise
    '#FF8C42', // Orange
    '#98D8C8', // Seafoam
    '#C39BD3', // Orchid
    '#7DCEA0', // Emerald
    '#F1948A', // Salmon
    '#76D7C4', // Aquamarine
    '#F0B27A', // Peach
    '#A9CCE3', // Powder Blue
    '#D7BDE2', // Mauve
    '#A3E4D7', // Pale Turquoise
];

const Bubble = ({
    bubble,
    x,
    y,
    selectedBubble,
    setSelectedBubble,
    onDragStart,
    onDragEnd,
    containerRef,
}: {
    bubble: BubbleData;
    x: MotionValue<number>;
    y: MotionValue<number>;
    selectedBubble: string | null;
    setSelectedBubble: (id: string | null) => void;
    onDragStart: (id: string) => void;
    onDragEnd: (id: string) => void;
    containerRef: React.RefObject<HTMLDivElement | null>;
}) => {
    const [isDragging, setIsDragging] = useState(false);

    const springConfig = { damping: 20, stiffness: 300 };
    const springX = useSpring(x, springConfig);
    const springY = useSpring(y, springConfig);

    const scale = useTransform(
        [springX, springY],
        () => selectedBubble === bubble.id ? 1.15 : 1
    );

    const isSelected = selectedBubble === bubble.id;
    const displaySymbol = bubble.symbol.replace('USDT', '');

    // Dynamic z-index: Selected > Dragging > Default
    const zIndex = isSelected ? 60 : (isDragging ? 50 : 10);

    return (
        <motion.div
            className="absolute cursor-grab active:cursor-grabbing"
            style={{
                x: isDragging ? x : springX,
                y: isDragging ? y : springY,
                scale,
                width: `${bubble.size}px`,
                height: `${bubble.size}px`,
                zIndex,
            }}
            drag
            dragConstraints={containerRef}
            dragMomentum={false} // We let D3 handle momentum
            onDragStart={() => {
                setIsDragging(true);
                onDragStart(bubble.id);
            }}
            onDrag={(_, info) => {
                // Update MotionValue directly from drag gesture
                x.set(x.get() + info.delta.x);
                y.set(y.get() + info.delta.y);
            }}
            onDragEnd={() => {
                setIsDragging(false);
                onDragEnd(bubble.id);
            }}
            onClick={() => setSelectedBubble(isSelected ? null : bubble.id)}
            initial={{ scale: 0, opacity: 0 }}
            animate={{ scale: 1, opacity: 1 }}
            transition={{
                type: 'spring',
                stiffness: 260,
                damping: 20,
                delay: Math.random() * 0.5
            }}
            whileHover={{ scale: 1.1, zIndex: 45 }}
        >
            <div
                className={`
                    w-full h-full rounded-full flex flex-col items-center justify-center
                    transition-all duration-300 relative select-none overflow-hidden
                    ${isSelected ? 'ring-2 ring-white/50 ring-offset-2 ring-offset-transparent' : ''}
                `}
                style={{
                    backgroundColor: bubble.color,
                    boxShadow: isDragging
                        ? `0 10px 30px rgba(0,0,0,0.5), inset 0 0 20px rgba(0,0,0,0.2)`
                        : `inset 0 0 20px rgba(0,0,0,0.2), 0 4px 10px rgba(0,0,0,0.3)`,
                    // Enforce flex column via inline style to be safe
                    display: 'flex',
                    flexDirection: 'column',
                }}
            >
                {/* Glossy reflection effect */}
                <div className="absolute top-0 left-0 w-full h-1/2 bg-gradient-to-b from-white/20 to-transparent pointer-events-none" />

                {/* Content */}
                <span
                    className="font-bold text-white drop-shadow-md text-center leading-none pointer-events-none z-10"
                    style={{
                        fontSize: `${Math.max(bubble.size / 5, 12)}px`,
                        marginBottom: '2px', // tiny gap
                    }}
                >
                    {displaySymbol}
                </span>
                <span
                    className="font-mono font-medium drop-shadow-md text-center text-white/90 pointer-events-none z-10"
                    style={{
                        fontSize: `${Math.max(bubble.size / 7, 10)}px`,
                    }}
                >
                    ${bubble.pnl.toFixed(2)}
                </span>

                {/* Trade count badge */}
                {bubble.size > 80 && (
                    <span
                        className="text-white/80 mt-1 text-center pointer-events-none z-10"
                        style={{
                            fontSize: `${Math.max(bubble.size / 9, 9)}px`,
                            lineHeight: 1,
                        }}
                    >
                        {bubble.tradeCount}t
                    </span>
                )}
            </div>

            {/* Info popup on selection */}
            {isSelected && (
                <motion.div
                    initial={{ opacity: 0, y: 10 }}
                    animate={{ opacity: 1, y: 0 }}
                    className="absolute top-full left-1/2 -translate-x-1/2 mt-2"
                    style={{ zIndex: 100, width: 'max-content' }}
                >
                    <div className="glass-card p-3 min-w-[150px] text-center pointer-events-none">
                        <div className="font-semibold mb-1">{bubble.symbol}</div>
                        <div className={`text - lg font - mono font - bold ${bubble.pnl >= 0 ? 'text-green-400' : 'text-red-400'} `}>
                            ${bubble.pnl.toFixed(4)}
                        </div>
                        <div className="text-xs text-muted-foreground mt-1 space-y-0.5">
                            <div>Trades: {bubble.tradeCount}</div>
                            <div>Win: {bubble.winCount} | Loss: {bubble.lossCount}</div>
                            <div>Win Rate: {bubble.winRate.toFixed(1)}%</div>
                        </div>
                    </div>
                </motion.div>
            )}
        </motion.div>
    );
};

export default function Ranking() {
    const [traders, setTraders] = useState<any[]>([]);
    const [selectedTrader, setSelectedTrader] = useState<string>('');
    const [trades, setTrades] = useState<Trade[]>([]);
    const [loading, setLoading] = useState(true);
    const [selectedBubble, setSelectedBubble] = useState<string | null>(null);

    // Refs
    const containerRef = useRef<HTMLDivElement>(null);
    const simulationRef = useRef<d3.Simulation<BubbleData, undefined> | null>(null);

    // Motion Values stored in a ref map to avoid re-renders
    const motionValues = useRef<Map<string, { x: MotionValue<number>; y: MotionValue<number> }>>(new Map());

    useEffect(() => {
        loadTraders();
    }, []);

    useEffect(() => {
        if (selectedTrader) {
            loadTrades();
        }
    }, [selectedTrader]);

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

    const loadTrades = async () => {
        try {
            const res = await getTrades(selectedTrader);
            setTrades(res.data.trades || []);
        } catch (err) {
            console.error('Failed to load trades:', err);
        }
    };

    // Calculate profit by symbol and prepare initial bubble data
    const bubbles = useMemo((): BubbleData[] => {
        if (!trades.length) return [];

        const symbolMap = new Map<string, SymbolProfit>();

        for (const trade of trades) {
            const existing = symbolMap.get(trade.symbol) || {
                symbol: trade.symbol,
                pnl: 0,
                tradeCount: 0,
                winCount: 0,
                lossCount: 0,
                winRate: 0,
            };
            existing.pnl += trade.realized_pnl || 0;
            existing.tradeCount += 1;
            if ((trade.realized_pnl || 0) > 0) {
                existing.winCount += 1;
            } else if ((trade.realized_pnl || 0) < 0) {
                existing.lossCount += 1;
            }
            symbolMap.set(trade.symbol, existing);
        }

        const stats = Array.from(symbolMap.values()).map(s => ({
            ...s,
            winRate: (s.winCount + s.lossCount) > 0 ? (s.winCount / (s.winCount + s.lossCount)) * 100 : 0,
        })).sort((a, b) => b.pnl - a.pnl);

        const pnlValues = stats.map(s => Math.abs(s.pnl));
        const maxPnl = Math.max(...pnlValues, 0.01);
        const minPnl = Math.min(...pnlValues);
        const minSize = 60;
        const maxSize = 180;

        return stats.map((s, i) => {
            const normalizedPnl = maxPnl === minPnl ? 0.5 : (Math.abs(s.pnl) - minPnl) / (maxPnl - minPnl);
            const size = minSize + normalizedPnl * (maxSize - minSize);

            // Ensure MotionValues exist for this bubble
            if (!motionValues.current.has(s.symbol)) {
                motionValues.current.set(s.symbol, {
                    x: new MotionValue(0),
                    y: new MotionValue(0)
                });
            }

            return {
                id: s.symbol,
                symbol: s.symbol,
                pnl: s.pnl,
                tradeCount: s.tradeCount,
                winCount: s.winCount,
                lossCount: s.lossCount,
                winRate: s.winRate,
                size,
                color: SYMBOL_COLORS[i % SYMBOL_COLORS.length],
                x: 0, // Initial, will be set by sim
                y: 0
            };
        });
    }, [trades]);

    const topSymbol = bubbles[0];
    const worstSymbol = bubbles[bubbles.length - 1];
    const totalPnl = bubbles.reduce((sum, s) => sum + s.pnl, 0);

    // Initialize D3 Simulation
    useEffect(() => {
        if (bubbles.length === 0 || !containerRef.current) return;

        const { width, height } = containerRef.current.getBoundingClientRect();
        const center = { x: width / 2, y: height / 2 };

        // Initialize positions randomly around center if they are 0
        bubbles.forEach((b) => {
            if (b.x === 0 && b.y === 0) {
                b.x = center.x + (Math.random() - 0.5) * 100;
                b.y = center.y + (Math.random() - 0.5) * 100;
            }
        });

        const simulation = d3.forceSimulation<BubbleData>(bubbles)
            .alpha(1)
            .alphaDecay(0.01)
            .velocityDecay(0.3)
            .force('charge', d3.forceManyBody().strength(5))
            .force('collide', d3.forceCollide()
                .radius((d: any) => d.size / 2 + 5) // Padding
                .strength(0.8)
                .iterations(3)
            )
            .force('x', d3.forceX(center.x).strength(0.08))
            .force('y', d3.forceY(center.y).strength(0.08))
            .on('tick', () => {
                bubbles.forEach(bubble => {
                    const mvs = motionValues.current.get(bubble.id);
                    if (mvs && bubble.x !== undefined && bubble.y !== undefined) {
                        // Center the bubble (d3 x/y is center, div is top-left)
                        // If not dragging (fx/fy not set), update UI from Sim
                        if (bubble.fx === undefined || bubble.fx === null) {
                            mvs.x.set(bubble.x - bubble.size / 2);
                            mvs.y.set(bubble.y - bubble.size / 2);
                        } else {
                            // If dragging, update D3's internal x/y to match the MotionValue
                            // This ensures D3's simulation state is consistent with the drag
                            bubble.x = mvs.x.get() + bubble.size / 2;
                            bubble.y = mvs.y.get() + bubble.size / 2;
                        }
                    }
                });
            });

        simulationRef.current = simulation;

        return () => {
            simulation.stop();
        };
    }, [bubbles]);

    // Drag Handlers connecting to D3
    const handleDragStart = (id: string) => {
        if (!simulationRef.current) return;
        simulationRef.current.alphaTarget(0.3).restart();

        const node = simulationRef.current.nodes().find(n => n.id === id);
        if (node) {
            const mvs = motionValues.current.get(id);
            if (mvs) {
                // Pin the node to its current position
                node.fx = mvs.x.get() + node.size / 2;
                node.fy = mvs.y.get() + node.size / 2;
                // Also update x/y to prevent jump
                node.x = node.fx;
                node.y = node.fy;
            }
        }
    };

    const handleDragEnd = (id: string) => {
        if (!simulationRef.current) return;
        simulationRef.current.alphaTarget(0);

        const node = simulationRef.current.nodes().find(n => n.id === id);
        if (node) {
            node.fx = null;
            node.fy = null;
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
                    <span className="text-muted-foreground">Loading ranking data...</span>
                </div>
            </div>
        );
    }

    return (
        <div className="p-4 lg:p-6 space-y-4 lg:space-y-6 h-full">
            {/* Header */}
            <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
                <motion.div
                    initial={{ opacity: 0, x: -20 }}
                    animate={{ opacity: 1, x: 0 }}
                >
                    <h1 className="text-2xl lg:text-3xl font-bold text-gradient flex items-center gap-3">
                        <Trophy className="w-6 h-6 lg:w-8 lg:h-8 text-amber-400" />
                        Symbol Ranking
                    </h1>
                    <p className="text-sm lg:text-base text-muted-foreground">
                        Drag bubbles to interact • Size = Profit magnitude
                        <br />
                        <span className="text-xs opacity-70">Powered by d3-force physics</span>
                    </p>
                </motion.div>

                <div className="flex gap-2 w-full sm:w-auto">
                    <Select value={selectedTrader} onValueChange={setSelectedTrader}>
                        <SelectTrigger className="flex-1 sm:w-[180px] glass">
                            <SelectValue placeholder="Select trader" />
                        </SelectTrigger>
                        <SelectContent>
                            {traders.map(t => (
                                <SelectItem key={t.id} value={t.id}>
                                    {t.name}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                    <TooltipProvider>
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <Button variant="outline" size="icon" className="glass">
                                    <Info className="h-4 w-4" />
                                </Button>
                            </TooltipTrigger>
                            <TooltipContent>
                                <p>Bubble size represents profit magnitude</p>
                                <p>Green = Profit, Red = Loss</p>
                                <p>Click a bubble for details</p>
                            </TooltipContent>
                        </Tooltip>
                    </TooltipProvider>
                    <Button
                        variant="outline"
                        size="icon"
                        onClick={loadTrades}
                        className="glass"
                    >
                        <RefreshCw className="h-4 w-4" />
                    </Button>
                </div>
            </div>

            {/* Stats */}
            <div className="grid gap-4 md:grid-cols-4">
                <StatCard
                    title="Best Symbol"
                    value={topSymbol?.pnl || 0}
                    icon={Trophy}
                    iconClassName="bg-amber-500/20 text-amber-400"
                    prefix="$"
                    decimals={4}
                    colorize
                    delay={0}
                />
                <StatCard
                    title="Worst Symbol"
                    value={worstSymbol?.pnl || 0}
                    icon={TrendingDown}
                    prefix="$"
                    decimals={4}
                    colorize
                    delay={1}
                />
                <StatCard
                    title="Total PnL"
                    value={totalPnl}
                    icon={totalPnl >= 0 ? TrendingUp : TrendingDown}
                    prefix="$"
                    decimals={4}
                    colorize
                    delay={2}
                />
                <StatCard
                    title="Symbols Traded"
                    value={bubbles.length}
                    icon={Medal}
                    decimals={0}
                    delay={3}
                />
            </div>

            {/* Bubble Visualization */}
            <GlassCard className="flex-1 min-h-[500px] relative overflow-hidden" spotlight>
                <div className="absolute top-4 left-4 z-10 flex items-center gap-4 text-xs text-muted-foreground pointer-events-none">
                    <div className="flex items-center gap-1.5">
                        <div className="w-3 h-3 rounded-full bg-green-500/50" />
                        <span>Profit</span>
                    </div>
                    <div className="flex items-center gap-1.5">
                        <div className="w-3 h-3 rounded-full bg-red-500/50" />
                        <span>Loss</span>
                    </div>
                    <div className="flex items-center gap-1.5">
                        <div className="w-6 h-6 rounded-full bg-white/10" />
                        <span>Size = |PnL|</span>
                    </div>
                </div>

                <div
                    ref={containerRef}
                    className="w-full h-full min-h-[500px] relative"
                    onClick={(e) => {
                        if (e.target === e.currentTarget) {
                            setSelectedBubble(null);
                        }
                    }}
                >
                    {bubbles.length === 0 ? (
                        <div className="flex items-center justify-center h-full">
                            <div className="text-center">
                                <Trophy className="w-16 h-16 text-muted-foreground/30 mx-auto mb-4" />
                                <p className="text-muted-foreground">No trade data available</p>
                                <p className="text-sm text-muted-foreground/60">
                                    Start trading to see your symbol rankings
                                </p>
                            </div>
                        </div>
                    ) : (
                        bubbles.map(bubble => {
                            const mvs = motionValues.current.get(bubble.id);
                            if (!mvs) return null;
                            return (
                                <Bubble
                                    key={bubble.id}
                                    bubble={bubble}
                                    x={mvs.x}
                                    y={mvs.y}
                                    selectedBubble={selectedBubble}
                                    setSelectedBubble={setSelectedBubble}
                                    onDragStart={handleDragStart}
                                    onDragEnd={handleDragEnd}
                                    containerRef={containerRef}
                                />
                            );
                        })
                    )}
                </div>
            </GlassCard>

            {/* Leaderboard - Only show top 10 */}
            <GlassCard>
                <h3 className="text-lg font-semibold mb-4 flex items-center gap-2">
                    <Medal className="w-5 h-5 text-amber-400" />
                    Leaderboard
                </h3>
                <div className="grid gap-2">
                    {bubbles.slice(0, 10).map((symbol, index) => (
                        <motion.div
                            key={symbol.symbol}
                            initial={{ opacity: 0, x: -20 }}
                            animate={{ opacity: 1, x: 0 }}
                            transition={{ delay: index * 0.05 }}
                            className={`
                                flex items-center justify-between p-3 rounded-lg transition-colors cursor-pointer
                                ${selectedBubble === symbol.symbol ? 'bg-white/10' : 'bg-white/5 hover:bg-white/10'}
                            `}
                            onClick={() => setSelectedBubble(symbol.symbol)}
                        >
                            <div className="flex items-center gap-3">
                                <span className={`
                                    w-7 h-7 rounded-full flex items-center justify-center text-sm font-bold
                                    ${index === 0 ? 'bg-amber-500/20 text-amber-400' :
                                        index === 1 ? 'bg-slate-300/20 text-slate-300' :
                                            index === 2 ? 'bg-amber-700/20 text-amber-600' :
                                                'bg-white/10 text-muted-foreground'}
                                `}>
                                    {index + 1}
                                </span>
                                <div>
                                    <span className="font-medium">{symbol.symbol.replace('USDT', '')}</span>
                                    <div className="text-xs text-muted-foreground">
                                        {symbol.tradeCount} trades • {symbol.winRate.toFixed(0)}% win rate
                                    </div>
                                </div>
                            </div>
                            <div className="text-right">
                                <div className={`font-mono font-medium ${symbol.pnl >= 0 ? 'text-green-400' : 'text-red-400'}`}>
                                    ${symbol.pnl.toFixed(4)}
                                </div>
                                <div className="text-xs text-muted-foreground">
                                    W: {symbol.winCount} / L: {symbol.lossCount}
                                </div>
                            </div>
                        </motion.div>
                    ))}
                </div>
            </GlassCard>
        </div>
    );
}
