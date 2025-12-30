import { ReactNode } from 'react';
import { motion } from 'framer-motion';
import { LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';
import { NumberTicker } from './number-ticker';

interface StatCardProps {
  title: string;
  value: number;
  icon?: LucideIcon;
  prefix?: string;
  suffix?: string;
  decimals?: number;
  change?: number;
  changeLabel?: string;
  colorize?: boolean;
  className?: string;
  iconClassName?: string;
  delay?: number;
}

export function StatCard({
  title,
  value,
  icon: Icon,
  prefix = '',
  suffix = '',
  decimals = 2,
  change,
  changeLabel,
  colorize = false,
  className,
  iconClassName,
  delay = 0,
}: StatCardProps) {
  const isPositive = value >= 0;
  const isChangePositive = change !== undefined && change >= 0;

  return (
    <motion.div
      initial={{ opacity: 0, y: 20 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ delay: delay * 0.1 }}
      className={cn(
        'glass-card p-5 spotlight-card',
        className
      )}
    >
      <div className="flex items-start justify-between mb-3">
        <span className="text-sm text-muted-foreground font-medium">{title}</span>
        {Icon && (
          <div className={cn(
            'p-2 rounded-lg',
            colorize && isPositive ? 'bg-green-500/20 text-green-400' :
            colorize && !isPositive ? 'bg-red-500/20 text-red-400' :
            'bg-primary/20 text-primary',
            iconClassName
          )}>
            <Icon className="w-4 h-4" />
          </div>
        )}
      </div>

      <div className={cn(
        'text-2xl font-bold tabular-nums',
        colorize && (isPositive ? 'text-green-400' : 'text-red-400')
      )}>
        <NumberTicker
          value={value}
          prefix={prefix}
          suffix={suffix}
          decimalPlaces={decimals}
          delay={delay * 0.1}
          colorize={colorize}
        />
      </div>

      {change !== undefined && (
        <div className="flex items-center gap-1 mt-2">
          <span className={cn(
            'text-xs font-medium',
            isChangePositive ? 'text-green-400' : 'text-red-400'
          )}>
            {isChangePositive ? '+' : ''}{change.toFixed(2)}%
          </span>
          {changeLabel && (
            <span className="text-xs text-muted-foreground">{changeLabel}</span>
          )}
        </div>
      )}
    </motion.div>
  );
}

// Mini stat for compact displays
interface MiniStatProps {
  label: string;
  value: string | number;
  colorize?: boolean;
  className?: string;
}

export function MiniStat({ label, value, colorize = false, className }: MiniStatProps) {
  const numValue = typeof value === 'number' ? value : parseFloat(value);
  const isPositive = !isNaN(numValue) && numValue >= 0;

  return (
    <div className={cn('flex flex-col', className)}>
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className={cn(
        'text-sm font-semibold tabular-nums',
        colorize && !isNaN(numValue) && (isPositive ? 'text-green-400' : 'text-red-400')
      )}>
        {value}
      </span>
    </div>
  );
}

// Progress stat with visual bar
interface ProgressStatProps {
  label: string;
  value: number;
  max?: number;
  suffix?: string;
  color?: 'primary' | 'success' | 'danger' | 'warning';
  className?: string;
}

export function ProgressStat({
  label,
  value,
  max = 100,
  suffix = '%',
  color = 'primary',
  className,
}: ProgressStatProps) {
  const percentage = Math.min((value / max) * 100, 100);

  const barColors = {
    primary: 'bg-primary',
    success: 'bg-green-500',
    danger: 'bg-red-500',
    warning: 'bg-amber-500',
  };

  return (
    <div className={cn('space-y-2', className)}>
      <div className="flex justify-between items-center">
        <span className="text-sm text-muted-foreground">{label}</span>
        <span className="text-sm font-medium tabular-nums">
          {value.toFixed(1)}{suffix}
        </span>
      </div>
      <div className="h-2 bg-white/5 rounded-full overflow-hidden">
        <motion.div
          className={cn('h-full rounded-full', barColors[color])}
          initial={{ width: 0 }}
          animate={{ width: `${percentage}%` }}
          transition={{ duration: 0.5, ease: 'easeOut' }}
        />
      </div>
    </div>
  );
}
