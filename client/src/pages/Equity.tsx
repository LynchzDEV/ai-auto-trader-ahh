import { TrendingUp } from 'lucide-react';

export default function Equity() {
  return (
    <div className="p-6">
      <div className="flex items-center gap-3 mb-6">
        <div className="p-3 rounded-xl bg-primary/20">
          <TrendingUp className="w-6 h-6 text-primary" />
        </div>
        <div>
          <h1 className="text-2xl font-bold">Equity Charts</h1>
          <p className="text-muted-foreground">Track portfolio performance over time</p>
        </div>
      </div>
      <div className="glass-card p-8 text-center">
        <p className="text-muted-foreground">Equity Charts coming soon...</p>
      </div>
    </div>
  );
}
