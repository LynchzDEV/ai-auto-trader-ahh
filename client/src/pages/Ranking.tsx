import { useEffect, useState, useMemo, useRef } from 'react';
import { motion, useMotionValue, useSpring, useTransform } from 'framer-motion';
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

interface Bubble {
    id: string;
    symbol: string;
    pnl: number;
    tradeCount: number;
    winCount: number;
    lossCount: number;
    winRate: number;
    size: number;
    x: number;
    y: number;
    vx: number;
    vy: number;
    color: string;
    isDragging?: boolean;
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
    selectedBubble,
    setSelectedBubble,
    onDragStart,
    onDragEnd,
    onDragUpdate,
    containerRef,
}: {
    bubble: Bubble;
    selectedBubble: string | null;
    setSelectedBubble: (id: string | null) => void;
    onDragStart: (id: string) => void;
    onDragEnd: (id: string, vx: number, vy: number) => void;
    onDragUpdate: (id: string, x: number, y: number) => void;
    containerRef: React.RefObject<HTMLDivElement | null>;
}) => {
    const x = useMotionValue(bubble.x);
    const y = useMotionValue(bubble.y);

    // Sync MotionValues with state, but ONLY if not dragging to avoid fighting
    useEffect(() => {
        if (!bubble.isDragging) {
            x.set(bubble.x);
            y.set(bubble.y);
        }
    }, [bubble.x, bubble.y, bubble.isDragging, x, y]);

    const springConfig = { damping: 20, stiffness: 300 };
    const springX = useSpring(x, springConfig);
    const springY = useSpring(y, springConfig);

    const scale = useTransform(
        [springX, springY],
        () => selectedBubble === bubble.id ? 1.15 : 1
    );

    const isSelected = selectedBubble === bubble.id;
    const isDragging = bubble.isDragging;
    const displaySymbol = bubble.symbol.replace('USDT', '');

    // Dynamic z-index: Selected > Dragging > Default
    const zIndex = isSelected ? 60 : (isDragging ? 50 : 10);

    return (
        <motion.div
            className="absolute cursor-grab active:cursor-grabbing"
            style={{
                x: isDragging ? x : springX, // Use raw x while dragging for responsiveness
                y: isDragging ? y : springY,
                scale,
                width: bubble.size,
                height: bubble.size,
                zIndex,
            }}
            drag
            dragConstraints={containerRef}
            dragMomentum={false} // We handle momentum via physics engine
            onDragStart={() => onDragStart(bubble.id)}
            onDrag={() => {
                // Update parent state for collision calculations
                onDragUpdate(bubble.id, x.get(), y.get());
            }}
            onDragEnd={(_, info) => {
                onDragEnd(bubble.id, info.velocity.x, info.velocity.y);
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
                    rounded-full flex flex-col items-center justify-center
                    transition-all duration-300 relative select-none
                    ${isSelected ? 'ring-2 ring-white/50 ring-offset-2 ring-offset-transparent' : ''}
                `}
                style={{
                    width: `${bubble.size}px`,
                    height: `${bubble.size}px`,
                    minWidth: `${bubble.size}px`,
                    minHeight: `${bubble.size}px`,
                    borderRadius: '50%',
                    backgroundColor: bubble.color,
                    boxShadow: isDragging
                        ? `0 10px 30px rgba(0,0,0,0.5), 0 0 ${bubble.size / 2}px ${bubble.color}`
                        : `0 0 ${bubble.size / 2}px ${bubble.color}, 0 4px 20px rgba(0,0,0,0.4)`,
                }}
            >
                {/* Content */}
                <span
                    className="font-bold text-white drop-shadow-lg text-center leading-tight pointer-events-none"
                    style={{
                        fontSize: `${Math.max(bubble.size / 5, 12)}px`,
                        textShadow: '0 2px 4px rgba(0,0,0,0.5)',
                    }}
                >
                    {displaySymbol}
                </span>
                <span
                    className="font-mono font-medium drop-shadow text-center text-white/90 pointer-events-none"
                    style={{
                        fontSize: `${Math.max(bubble.size / 7, 9)}px`,
                        textShadow: '0 1px 3px rgba(0,0,0,0.5)',
                    }}
                >
                    ${bubble.pnl.toFixed(2)}
                </span>

                {/* Trade count badge */}
                {bubble.size > 90 && (
                    <span
                        className="text-white/70 mt-0.5 text-center pointer-events-none"
                        style={{
                            fontSize: `${Math.max(bubble.size / 9, 8)}px`,
                        }}
                    >
                        {bubble.tradeCount} trades
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
                        <div className={`text-lg font-mono font-bold ${bubble.pnl >= 0 ? 'text-green-400' : 'text-red-400'}`}>
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
    const [bubbles, setBubbles] = useState<Bubble[]>([]);
    const [selectedBubble, setSelectedBubble] = useState<string | null>(null);
    const containerRef = useRef<HTMLDivElement>(null);

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

    // Calculate profit by symbol
    const profitBySymbol = useMemo((): SymbolProfit[] => {
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

        // Calculate win rates (based on win+loss, not all trades - excludes breakeven)
        const results = Array.from(symbolMap.values()).map(s => ({
            ...s,
            winRate: (s.winCount + s.lossCount) > 0 ? (s.winCount / (s.winCount + s.lossCount)) * 100 : 0,
        }));

        return results.sort((a, b) => b.pnl - a.pnl);
    }, [trades]);

    const topSymbol = profitBySymbol[0];
    const worstSymbol = profitBySymbol[profitBySymbol.length - 1];
    const totalPnl = profitBySymbol.reduce((sum, s) => sum + s.pnl, 0);

    // Initialize bubbles with physics positions
    useEffect(() => {
        if (!containerRef.current || profitBySymbol.length === 0) return;

        const container = containerRef.current;
        const { width, height } = container.getBoundingClientRect();

        // Calculate size range based on absolute PnL values
        const pnlValues = profitBySymbol.map(s => Math.abs(s.pnl));
        const maxPnl = Math.max(...pnlValues, 0.01);
        const minPnl = Math.min(...pnlValues);

        const minSize = 50;
        const maxSize = Math.min(width, height) / 3;

        const newBubbles: Bubble[] = profitBySymbol.map((symbol, index) => {
            // Normalize size based on PnL
            const normalizedPnl = maxPnl === minPnl
                ? 0.5
                : (Math.abs(symbol.pnl) - minPnl) / (maxPnl - minPnl);
            const size = minSize + normalizedPnl * (maxSize - minSize);

            // Arrange in a circular pattern initially
            const angle = (index / profitBySymbol.length) * Math.PI * 2;
            const radius = Math.min(width, height) / 3;
            const centerX = width / 2 - size / 2;
            const centerY = height / 2 - size / 2;

            const x = centerX + Math.cos(angle) * radius * (0.5 + Math.random() * 0.5);
            const y = centerY + Math.sin(angle) * radius * (0.5 + Math.random() * 0.5);

            // Use curated color palette for distinct colors
            const color = SYMBOL_COLORS[index % SYMBOL_COLORS.length];

            return {
                id: symbol.symbol,
                symbol: symbol.symbol,
                pnl: symbol.pnl,
                tradeCount: symbol.tradeCount,
                winCount: symbol.winCount,
                lossCount: symbol.lossCount,
                winRate: symbol.winRate,
                size,
                x,
                y,
                vx: 0,
                vy: 0,
                color,
            };
        });

        setBubbles(newBubbles);

        // Animate bubbles into place
        newBubbles.forEach((bubble, index) => {
            setTimeout(() => {
                setBubbles(prev => prev.map(b =>
                    b.id === bubble.id ? { ...b, x: bubble.x, y: bubble.y } : b
                ));
            }, index * 100);
        });
    }, [profitBySymbol]);

    // Drag handlers
    const handleDragStart = (id: string) => {
        setBubbles(prev => prev.map(b =>
            b.id === id ? { ...b, isDragging: true, vx: 0, vy: 0 } : b
        ));
    };

    const handleDragUpdate = (id: string, x: number, y: number) => {
        setBubbles(prev => prev.map(b =>
            b.id === id ? { ...b, x, y } : b
        ));
    };

    const handleDragEnd = (id: string, vx: number, vy: number) => {
        setBubbles(prev => prev.map(b =>
            b.id === id ? { ...b, isDragging: false, vx, vy } : b
        ));
    };

    // Physics simulation
    useEffect(() => {
        if (bubbles.length < 2) return;

        const SPACING = 8;
        const DAMPING = 0.9;
        const COLLISION_STRENGTH = 0.8;
        const ATTRACTION_STRENGTH = 0.01;
        const CENTER_PULL = 0.005;

        const simulatePhysics = () => {
            setBubbles(prevBubbles => {
                const newBubbles = prevBubbles.map(b => ({ ...b }));
                const container = containerRef.current;
                if (!container) return prevBubbles;

                const { width, height } = container.getBoundingClientRect();

                // 1. Calculate Center of Mass (excluding dragged bubbles from mass calculation for stability)
                let totalMass = 0;
                let comX = 0;
                let comY = 0;
                let validCount = 0;

                for (const b of newBubbles) {
                    if (!b.isDragging) {
                        const mass = b.size;
                        comX += (b.x + b.size / 2) * mass;
                        comY += (b.y + b.size / 2) * mass;
                        totalMass += mass;
                        validCount++;
                    }
                }

                if (validCount > 0) {
                    comX /= totalMass;
                    comY /= totalMass;
                } else {
                    comX = width / 2;
                    comY = height / 2;
                }

                // 2. Apply Forces (Gravity, Center Pull)
                for (const bubble of newBubbles) {
                    if (bubble.isDragging) continue; // Skip physics forces for dragged bubble

                    const cx = bubble.x + bubble.size / 2;
                    const cy = bubble.y + bubble.size / 2;

                    // Pull toward center of mass
                    const dxCom = comX - cx;
                    const dyCom = comY - cy;
                    const distCom = Math.sqrt(dxCom * dxCom + dyCom * dyCom);

                    if (distCom > 1) {
                        bubble.vx += (dxCom / distCom) * ATTRACTION_STRENGTH * bubble.size;
                        bubble.vy += (dyCom / distCom) * ATTRACTION_STRENGTH * bubble.size;
                    }

                    // Pull toward container center
                    const dxCenter = (width / 2) - cx;
                    const dyCenter = (height / 2) - cy;
                    bubble.vx += dxCenter * CENTER_PULL;
                    bubble.vy += dyCenter * CENTER_PULL;
                }

                // 3. Resolve Collisions
                for (let i = 0; i < newBubbles.length; i++) {
                    for (let j = i + 1; j < newBubbles.length; j++) {
                        const b1 = newBubbles[i];
                        const b2 = newBubbles[j];

                        const c1x = b1.x + b1.size / 2;
                        const c1y = b1.y + b1.size / 2;
                        const c2x = b2.x + b2.size / 2;
                        const c2y = b2.y + b2.size / 2;

                        const dx = c2x - c1x;
                        const dy = c2y - c1y;
                        const distance = Math.sqrt(dx * dx + dy * dy);

                        // If one is being dragged, give it a larger "push" radius (force field)
                        const isDragInteraction = b1.isDragging || b2.isDragging;
                        const collisionBuffer = isDragInteraction ? 30 : SPACING;
                        const minDistance = (b1.size + b2.size) / 2 + collisionBuffer;

                        if (distance < minDistance && distance > 0) {
                            const overlap = minDistance - distance;
                            const normalX = dx / distance;
                            const normalY = dy / distance;

                            // Mass-based reaction
                            const totalSize = b1.size + b2.size;
                            const r1 = b2.size / totalSize; // b1 moves proportional to b2's size
                            const r2 = b1.size / totalSize;

                            // Stronger force for drag interactions to feel responsive
                            const force = overlap * (isDragInteraction ? 1.5 : COLLISION_STRENGTH); // Boost force for drag

                            // Logic: If one is dragged, it has infinite mass (doesn't move), other moves full overlap
                            if (b1.isDragging && !b2.isDragging) {
                                // b1 static, b2 moves away aggressively
                                newBubbles[j].x += normalX * overlap;
                                newBubbles[j].y += normalY * overlap;
                                // Impart significant velocity ("fling" or "knock" effect)
                                newBubbles[j].vx += normalX * force * 0.8;
                                newBubbles[j].vy += normalY * force * 0.8;
                            } else if (!b1.isDragging && b2.isDragging) {
                                // b2 static, b1 moves away aggressively
                                newBubbles[i].x -= normalX * overlap;
                                newBubbles[i].y -= normalY * overlap;
                                newBubbles[i].vx -= normalX * force * 0.8;
                                newBubbles[i].vy -= normalY * force * 0.8;
                            } else if (!b1.isDragging && !b2.isDragging) {
                                // Both move
                                newBubbles[i].x -= normalX * overlap * r1;
                                newBubbles[i].y -= normalY * overlap * r1;
                                newBubbles[j].x += normalX * overlap * r2;
                                newBubbles[j].y += normalY * overlap * r2;

                                // Bounce
                                newBubbles[i].vx -= normalX * force * 0.2;
                                newBubbles[i].vy -= normalY * force * 0.2;
                                newBubbles[j].vx += normalX * force * 0.2;
                                newBubbles[j].vy += normalY * force * 0.2;
                            }
                        }
                    }
                }

                // 4. Update Position & Boundaries
                for (const bubble of newBubbles) {
                    if (bubble.isDragging) continue;

                    // Apply velocity
                    bubble.x += bubble.vx * 0.1;
                    bubble.y += bubble.vy * 0.1;

                    // Damping
                    bubble.vx *= DAMPING;
                    bubble.vy *= DAMPING;

                    // Boundaries
                    const padding = 10;
                    if (bubble.x < padding) {
                        bubble.x = padding;
                        bubble.vx = Math.abs(bubble.vx) * 0.5;
                    }
                    if (bubble.x > width - bubble.size - padding) {
                        bubble.x = width - bubble.size - padding;
                        bubble.vx = -Math.abs(bubble.vx) * 0.5;
                    }
                    if (bubble.y < padding) {
                        bubble.y = padding;
                        bubble.vy = Math.abs(bubble.vy) * 0.5;
                    }
                    if (bubble.y > height - bubble.size - padding) {
                        bubble.y = height - bubble.size - padding;
                        bubble.vy = -Math.abs(bubble.vy) * 0.5;
                    }
                }

                return newBubbles;
            });
        };

        const intervalId = setInterval(simulatePhysics, 16);
        return () => clearInterval(intervalId);
    }, [bubbles.length]);

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
                        Drag bubbles around • Size = Profit magnitude
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
                    value={profitBySymbol.length}
                    icon={Medal}
                    decimals={0}
                    delay={3}
                />
            </div>

            {/* Bubble Visualization */}
            <GlassCard className="flex-1 min-h-[500px] relative overflow-hidden" spotlight>
                <div className="absolute top-4 left-4 z-10 flex items-center gap-4 text-xs text-muted-foreground">
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
                        bubbles.map(bubble => (
                            <Bubble
                                key={bubble.id}
                                bubble={bubble}
                                selectedBubble={selectedBubble}
                                setSelectedBubble={setSelectedBubble}
                                onDragStart={handleDragStart}
                                onDragEnd={handleDragEnd}
                                onDragUpdate={handleDragUpdate}
                                containerRef={containerRef}
                            />
                        ))
                    )}
                </div>
            </GlassCard>

            {/* Leaderboard */}
            <GlassCard>
                <h3 className="text-lg font-semibold mb-4 flex items-center gap-2">
                    <Medal className="w-5 h-5 text-amber-400" />
                    Leaderboard
                </h3>
                <div className="grid gap-2">
                    {profitBySymbol.slice(0, 10).map((symbol, index) => (
                        <motion.div
                            key={symbol.symbol}
                            initial={{ opacity: 0, x: -20 }}
                            animate={{ opacity: 1, x: 0 }}
                            transition={{ delay: index * 0.05 }}
                            className={`
                flex items-center justify-between p-3 rounded-lg transition-colors
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
