import { FlaskConical } from 'lucide-react';

export default function Backtest() {
  return (
    <div className="p-6">
      <div className="flex items-center gap-3 mb-6">
        <div className="p-3 rounded-xl bg-primary/20">
          <FlaskConical className="w-6 h-6 text-primary" />
        </div>
        <div>
          <h1 className="text-2xl font-bold">Backtesting</h1>
          <p className="text-muted-foreground">Test strategies on historical data</p>
        </div>
      </div>
      <div className="glass-card p-8 text-center">
        <p className="text-muted-foreground">Backtesting UI coming soon...</p>
      </div>
    </div>
  );
}
