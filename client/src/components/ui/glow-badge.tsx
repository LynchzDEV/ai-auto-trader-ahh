import { forwardRef, HTMLAttributes } from 'react';
import { cn } from '@/lib/utils';
import { cva, type VariantProps } from 'class-variance-authority';

const glowBadgeVariants = cva(
  'inline-flex items-center rounded-full px-3 py-1 text-xs font-medium transition-all duration-200',
  {
    variants: {
      variant: {
        default:
          'bg-primary/20 text-primary border border-primary/30 hover:border-primary/50',
        success:
          'bg-green-500/20 text-green-400 border border-green-500/30 hover:border-green-500/50 hover:shadow-[0_0_12px_rgba(34,197,94,0.3)]',
        danger:
          'bg-red-500/20 text-red-400 border border-red-500/30 hover:border-red-500/50 hover:shadow-[0_0_12px_rgba(239,68,68,0.3)]',
        warning:
          'bg-amber-500/20 text-amber-400 border border-amber-500/30 hover:border-amber-500/50 hover:shadow-[0_0_12px_rgba(245,158,11,0.3)]',
        info:
          'bg-blue-500/20 text-blue-400 border border-blue-500/30 hover:border-blue-500/50 hover:shadow-[0_0_12px_rgba(59,130,246,0.3)]',
        purple:
          'bg-purple-500/20 text-purple-400 border border-purple-500/30 hover:border-purple-500/50 hover:shadow-[0_0_12px_rgba(139,92,246,0.3)]',
        secondary:
          'bg-white/5 text-muted-foreground border border-white/10 hover:border-white/20',
      },
      glow: {
        true: '',
        false: '',
      },
      pulse: {
        true: '',
        false: '',
      },
    },
    compoundVariants: [
      {
        variant: 'success',
        glow: true,
        className: 'shadow-[0_0_12px_rgba(34,197,94,0.4)]',
      },
      {
        variant: 'danger',
        glow: true,
        className: 'shadow-[0_0_12px_rgba(239,68,68,0.4)]',
      },
      {
        variant: 'warning',
        glow: true,
        className: 'shadow-[0_0_12px_rgba(245,158,11,0.4)]',
      },
      {
        variant: 'info',
        glow: true,
        className: 'shadow-[0_0_12px_rgba(59,130,246,0.4)]',
      },
      {
        variant: 'purple',
        glow: true,
        className: 'shadow-[0_0_12px_rgba(139,92,246,0.4)]',
      },
      {
        variant: 'success',
        pulse: true,
        className: 'animate-pulse',
      },
      {
        variant: 'danger',
        pulse: true,
        className: 'animate-pulse',
      },
    ],
    defaultVariants: {
      variant: 'default',
      glow: false,
      pulse: false,
    },
  }
);

interface GlowBadgeProps
  extends HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof glowBadgeVariants> {
  dot?: boolean;
}

const GlowBadge = forwardRef<HTMLSpanElement, GlowBadgeProps>(
  ({ className, variant, glow, pulse, dot, children, ...props }, ref) => {
    return (
      <span
        ref={ref}
        className={cn(glowBadgeVariants({ variant, glow, pulse }), className)}
        {...props}
      >
        {dot && (
          <span
            className={cn(
              'mr-2 h-2 w-2 rounded-full',
              variant === 'success' && 'bg-green-400',
              variant === 'danger' && 'bg-red-400',
              variant === 'warning' && 'bg-amber-400',
              variant === 'info' && 'bg-blue-400',
              variant === 'purple' && 'bg-purple-400',
              variant === 'secondary' && 'bg-gray-400',
              !variant && 'bg-primary',
              pulse && 'animate-pulse'
            )}
          />
        )}
        {children}
      </span>
    );
  }
);

GlowBadge.displayName = 'GlowBadge';

export { GlowBadge, glowBadgeVariants };
