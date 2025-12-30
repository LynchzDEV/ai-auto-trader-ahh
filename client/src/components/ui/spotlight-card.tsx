import { useRef, useState, ReactNode, MouseEvent } from 'react';
import { motion } from 'framer-motion';
import { cn } from '@/lib/utils';

interface SpotlightCardProps {
  children: ReactNode;
  className?: string;
  spotlightColor?: string;
}

export function SpotlightCard({
  children,
  className,
  spotlightColor = 'rgba(59, 130, 246, 0.15)',
}: SpotlightCardProps) {
  const divRef = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState({ x: 0, y: 0 });
  const [opacity, setOpacity] = useState(0);

  const handleMouseMove = (e: MouseEvent<HTMLDivElement>) => {
    if (!divRef.current) return;

    const rect = divRef.current.getBoundingClientRect();
    setPosition({
      x: e.clientX - rect.left,
      y: e.clientY - rect.top,
    });
  };

  const handleMouseEnter = () => {
    setOpacity(1);
  };

  const handleMouseLeave = () => {
    setOpacity(0);
  };

  return (
    <div
      ref={divRef}
      onMouseMove={handleMouseMove}
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
      className={cn(
        'relative glass-card overflow-hidden',
        className
      )}
    >
      <div
        className="pointer-events-none absolute -inset-px opacity-0 transition duration-300"
        style={{
          opacity,
          background: `radial-gradient(600px circle at ${position.x}px ${position.y}px, ${spotlightColor}, transparent 40%)`,
        }}
      />
      {children}
    </div>
  );
}

// Animated border card variant
interface AnimatedBorderCardProps {
  children: ReactNode;
  className?: string;
  borderClassName?: string;
}

export function AnimatedBorderCard({
  children,
  className,
  borderClassName,
}: AnimatedBorderCardProps) {
  return (
    <div className={cn('relative p-[1px] overflow-hidden rounded-xl', borderClassName)}>
      <motion.div
        className="absolute inset-0"
        style={{
          background:
            'linear-gradient(90deg, #3b82f6, #8b5cf6, #ec4899, #22c55e, #3b82f6)',
          backgroundSize: '200% 100%',
        }}
        animate={{
          backgroundPosition: ['0% 50%', '100% 50%', '0% 50%'],
        }}
        transition={{
          duration: 4,
          ease: 'linear',
          repeat: Infinity,
        }}
      />
      <div
        className={cn(
          'relative glass-card rounded-xl',
          className
        )}
      >
        {children}
      </div>
    </div>
  );
}

// Shimmer button
interface ShimmerButtonProps {
  children: ReactNode;
  className?: string;
  onClick?: () => void;
  disabled?: boolean;
}

export function ShimmerButton({
  children,
  className,
  onClick,
  disabled,
}: ShimmerButtonProps) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'relative inline-flex items-center justify-center px-6 py-3 overflow-hidden font-medium rounded-xl transition-all duration-300',
        'bg-primary/20 text-primary-foreground border border-primary/30',
        'hover:bg-primary/30 hover:border-primary/50',
        'disabled:opacity-50 disabled:cursor-not-allowed',
        'shimmer',
        className
      )}
    >
      {children}
    </button>
  );
}
