import { History as HistoryIcon } from 'lucide-react';

export default function History() {
  return (
    <div className="p-6">
      <div className="flex items-center gap-3 mb-6">
        <div className="p-3 rounded-xl bg-primary/20">
          <HistoryIcon className="w-6 h-6 text-primary" />
        </div>
        <div>
          <h1 className="text-2xl font-bold">Trade History</h1>
          <p className="text-muted-foreground">Complete log of all trades</p>
        </div>
      </div>
      <div className="glass-card p-8 text-center">
        <p className="text-muted-foreground">Trade History coming soon...</p>
      </div>
    </div>
  );
}
