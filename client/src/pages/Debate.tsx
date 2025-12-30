import { MessageSquare } from 'lucide-react';

export default function Debate() {
  return (
    <div className="p-6">
      <div className="flex items-center gap-3 mb-6">
        <div className="p-3 rounded-xl bg-primary/20">
          <MessageSquare className="w-6 h-6 text-primary" />
        </div>
        <div>
          <h1 className="text-2xl font-bold">Debate Arena</h1>
          <p className="text-muted-foreground">Multi-AI consensus trading decisions</p>
        </div>
      </div>
      <div className="glass-card p-8 text-center">
        <p className="text-muted-foreground">Debate Arena UI coming soon...</p>
      </div>
    </div>
  );
}
