import { useEffect, useRef, useState } from 'react';
import { motion, useSpring, useTransform, useMotionValue } from 'framer-motion';
import { cn } from '@/lib/utils';

interface NumberTickerProps {
  value: number;
  direction?: 'up' | 'down';
  className?: string;
  decimalPlaces?: number;
  prefix?: string;
  suffix?: string;
  delay?: number;
  duration?: number;
  colorize?: boolean;
}

export function NumberTicker({
  value,
  direction = 'up',
  className,
  decimalPlaces = 2,
  prefix = '',
  suffix = '',
  delay = 0,
  duration = 1,
  colorize = false,
}: NumberTickerProps) {
  const ref = useRef<HTMLSpanElement>(null);
  const [hasAnimated, setHasAnimated] = useState(false);
  const motionValue = useMotionValue(direction === 'down' ? value : 0);

  const springValue = useSpring(motionValue, {
    stiffness: 100,
    damping: 30,
    duration: duration * 1000,
  });

  const displayValue = useTransform(springValue, (latest) => {
    return `${prefix}${latest.toFixed(decimalPlaces)}${suffix}`;
  });

  useEffect(() => {
    const timer = setTimeout(() => {
      motionValue.set(direction === 'down' ? 0 : value);
      setHasAnimated(true);
    }, delay * 1000);

    return () => clearTimeout(timer);
  }, [motionValue, delay, value, direction]);

  useEffect(() => {
    if (hasAnimated) {
      motionValue.set(value);
    }
  }, [value, hasAnimated, motionValue]);

  const colorClass = colorize
    ? value >= 0
      ? 'text-green-500'
      : 'text-red-500'
    : '';

  return (
    <motion.span
      ref={ref}
      className={cn('tabular-nums font-mono', colorClass, className)}
    >
      {displayValue}
    </motion.span>
  );
}

// Simpler variant for quick number displays
interface StatNumberProps {
  value: number;
  prefix?: string;
  suffix?: string;
  decimals?: number;
  colorize?: boolean;
  className?: string;
}

export function StatNumber({
  value,
  prefix = '',
  suffix = '',
  decimals = 2,
  colorize = false,
  className,
}: StatNumberProps) {
  const [displayValue, setDisplayValue] = useState(0);
  const [prevValue, setPrevValue] = useState(0);

  useEffect(() => {
    setPrevValue(displayValue);
    setDisplayValue(value);
  }, [value]);

  const diff = displayValue - prevValue;
  const animationClass = diff > 0 ? 'price-up' : diff < 0 ? 'price-down' : '';
  const colorClass = colorize
    ? displayValue >= 0
      ? 'text-green-500'
      : 'text-red-500'
    : '';

  return (
    <motion.span
      key={displayValue}
      initial={{ opacity: 0.5, y: diff > 0 ? 5 : -5 }}
      animate={{ opacity: 1, y: 0 }}
      className={cn(
        'tabular-nums font-mono',
        animationClass,
        colorClass,
        className
      )}
    >
      {prefix}
      {displayValue.toFixed(decimals)}
      {suffix}
    </motion.span>
  );
}
