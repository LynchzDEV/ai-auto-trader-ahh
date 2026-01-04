import { useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { ChevronDown } from 'lucide-react';
import { cn } from '@/lib/utils';

interface Column<T> {
  key: keyof T | string;
  label: string;
  render?: (value: any, item: T) => React.ReactNode;
  primary?: boolean; // Show in collapsed card view
  align?: 'left' | 'right' | 'center';
  className?: string;
}

interface MobileCardTableProps<T> {
  data: T[];
  columns: Column<T>[];
  keyExtractor: (item: T, index: number) => string | number;
  onRowClick?: (item: T) => void;
  emptyState?: React.ReactNode;
  className?: string;
}

function getNestedValue(obj: any, path: string): any {
  return path.split('.').reduce((acc, part) => acc?.[part], obj);
}

export function MobileCardTable<T>({
  data,
  columns,
  keyExtractor,
  onRowClick,
  emptyState,
  className,
}: MobileCardTableProps<T>) {
  const [expandedId, setExpandedId] = useState<string | number | null>(null);

  const primaryColumns = columns.filter(c => c.primary);
  const secondaryColumns = columns.filter(c => !c.primary);

  if (data.length === 0 && emptyState) {
    return <>{emptyState}</>;
  }

  return (
    <>
      {/* Desktop Table View */}
      <div className={cn("hidden lg:block", className)}>
        <table className="w-full trading-table">
          <thead className="sticky top-0 bg-[#12121a] z-10">
            <tr className="border-b border-white/5">
              {columns.map((col) => (
                <th
                  key={String(col.key)}
                  className={cn(
                    "p-4 font-medium text-muted-foreground",
                    col.align === 'right' && 'text-right',
                    col.align === 'center' && 'text-center',
                    col.className
                  )}
                >
                  {col.label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {data.map((item, index) => (
              <tr
                key={keyExtractor(item, index)}
                onClick={() => onRowClick?.(item)}
                className={cn(
                  "border-b border-white/5 hover:bg-white/5 transition-colors",
                  onRowClick && "cursor-pointer"
                )}
              >
                {columns.map((col) => {
                  const value = getNestedValue(item, String(col.key));
                  return (
                    <td
                      key={String(col.key)}
                      className={cn(
                        "p-4",
                        col.align === 'right' && 'text-right',
                        col.align === 'center' && 'text-center',
                        col.className
                      )}
                    >
                      {col.render ? col.render(value, item) : String(value ?? '-')}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Mobile Card View */}
      <div className={cn("lg:hidden space-y-3", className)}>
        {data.map((item, index) => {
          const id = keyExtractor(item, index);
          const isExpanded = expandedId === id;

          return (
            <motion.div
              key={id}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: index * 0.03 }}
              className="glass-card overflow-hidden"
            >
              {/* Card Header - Primary Info */}
              <button
                onClick={() => {
                  if (secondaryColumns.length > 0) {
                    setExpandedId(isExpanded ? null : id);
                  } else {
                    onRowClick?.(item);
                  }
                }}
                className="w-full p-4 text-left"
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="flex-1 min-w-0">
                    {/* First row - main identifiers */}
                    <div className="flex items-center gap-2 mb-2">
                      {primaryColumns.slice(0, 2).map((col) => {
                        const value = getNestedValue(item, String(col.key));
                        return (
                          <div key={String(col.key)} className="font-medium">
                            {col.render ? col.render(value, item) : String(value ?? '-')}
                          </div>
                        );
                      })}
                    </div>
                    {/* Second row - additional primary info */}
                    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
                      {primaryColumns.slice(2).map((col) => {
                        const value = getNestedValue(item, String(col.key));
                        return (
                          <div key={String(col.key)} className="flex items-center gap-1">
                            <span className="text-muted-foreground/60">{col.label}:</span>
                            {col.render ? col.render(value, item) : String(value ?? '-')}
                          </div>
                        );
                      })}
                    </div>
                  </div>

                  {/* Expand button */}
                  {secondaryColumns.length > 0 && (
                    <motion.div
                      animate={{ rotate: isExpanded ? 180 : 0 }}
                      className="p-1 rounded-full bg-white/5"
                    >
                      <ChevronDown className="w-4 h-4 text-muted-foreground" />
                    </motion.div>
                  )}
                </div>
              </button>

              {/* Expanded Content - Secondary Info */}
              <AnimatePresence>
                {isExpanded && secondaryColumns.length > 0 && (
                  <motion.div
                    initial={{ height: 0, opacity: 0 }}
                    animate={{ height: 'auto', opacity: 1 }}
                    exit={{ height: 0, opacity: 0 }}
                    transition={{ duration: 0.2 }}
                    className="overflow-hidden"
                  >
                    <div className="px-4 pb-4 pt-2 border-t border-white/5 space-y-2">
                      {secondaryColumns.map((col) => {
                        const value = getNestedValue(item, String(col.key));
                        return (
                          <div
                            key={String(col.key)}
                            className="flex items-center justify-between text-sm"
                          >
                            <span className="text-muted-foreground">{col.label}</span>
                            <span className={cn("font-mono", col.className)}>
                              {col.render ? col.render(value, item) : String(value ?? '-')}
                            </span>
                          </div>
                        );
                      })}

                      {/* Action button if row is clickable */}
                      {onRowClick && (
                        <button
                          onClick={() => onRowClick(item)}
                          className="w-full mt-2 py-2 text-sm text-primary hover:text-primary/80 transition-colors"
                        >
                          View Details
                        </button>
                      )}
                    </div>
                  </motion.div>
                )}
              </AnimatePresence>
            </motion.div>
          );
        })}
      </div>
    </>
  );
}

export type { Column };
