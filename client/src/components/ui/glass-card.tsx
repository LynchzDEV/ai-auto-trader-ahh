import { forwardRef } from 'react';
import type { HTMLAttributes, ReactNode } from 'react';
import { cn } from '@/lib/utils';

interface GlassCardProps extends HTMLAttributes<HTMLDivElement> {
  children: ReactNode;
  glow?: 'primary' | 'profit' | 'loss' | 'none';
  animate?: boolean;
  spotlight?: boolean;
}

const GlassCard = forwardRef<HTMLDivElement, GlassCardProps>(
  ({ className, children, glow = 'none', animate = false, spotlight = false, ...props }, ref) => {
    return (
      <div
        ref={ref}
        className={cn(
          'glass-card p-6',
          glow === 'primary' && 'glow-primary',
          glow === 'profit' && 'glow-profit',
          glow === 'loss' && 'glow-loss',
          animate && 'glow-border',
          spotlight && 'spotlight-card',
          className
        )}
        {...props}
      >
        {children}
      </div>
    );
  }
);

GlassCard.displayName = 'GlassCard';

export { GlassCard };
