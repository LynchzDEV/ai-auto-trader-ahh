import { useEffect, useState } from 'react';
import { getTraders, getDecisions } from '../lib/api';
import type { Trader, Decision } from '../types';
import { RefreshCw } from 'lucide-react';

export default function Logs() {
  const [traders, setTraders] = useState<Trader[]>([]);
  const [selectedTrader, setSelectedTrader] = useState<string | null>(null);
  const [decisions, setDecisions] = useState<Decision[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    loadTraders();
  }, []);

  useEffect(() => {
    if (selectedTrader) {
      loadDecisions();
    }
  }, [selectedTrader]);

  const loadTraders = async () => {
    try {
      const res = await getTraders();
      const traderList = res.data.traders || [];
      setTraders(traderList);
      if (traderList.length > 0) {
        setSelectedTrader(traderList[0].id);
      }
    } catch (err) {
      console.error('Failed to load traders:', err);
    } finally {
      setLoading(false);
    }
  };

  const loadDecisions = async () => {
    if (!selectedTrader) return;
    try {
      const res = await getDecisions(selectedTrader);
      setDecisions(res.data.decisions || []);
    } catch (err) {
      console.error('Failed to load decisions:', err);
    }
  };

  const parseDecisions = (decisionsJson: string) => {
    try {
      return JSON.parse(decisionsJson);
    } catch {
      return [];
    }
  };

  if (loading) {
    return <div className="p-8 text-center">Loading...</div>;
  }

  return (
    <div className="p-4 lg:p-6 space-y-4 lg:space-y-6">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
        <h1 className="text-2xl font-bold">Trading Logs</h1>
        <div className="flex items-center gap-4">
          <select
            value={selectedTrader || ''}
            onChange={(e) => setSelectedTrader(e.target.value)}
            className="bg-slate-700 rounded px-3 py-2"
          >
            {traders.map((trader) => (
              <option key={trader.id} value={trader.id}>{trader.name}</option>
            ))}
          </select>
          <button
            onClick={loadDecisions}
            className="p-2 hover:bg-slate-700 rounded"
          >
            <RefreshCw size={20} />
          </button>
        </div>
      </div>

      {traders.length === 0 ? (
        <div className="bg-slate-800 rounded-lg p-8 text-center text-slate-400">
          No traders configured. Go to Config to create one.
        </div>
      ) : decisions.length === 0 ? (
        <div className="bg-slate-800 rounded-lg p-8 text-center text-slate-400">
          No decisions recorded yet. Start a trader to generate logs.
        </div>
      ) : (
        <div className="space-y-4">
          {decisions.map((decision) => {
            const parsed = parseDecisions(decision.decisions);
            return (
              <div key={decision.id} className="bg-slate-800 rounded-lg p-4">
                <div className="flex justify-between items-center mb-3">
                  <span className="text-slate-400 text-sm">
                    {new Date(decision.timestamp).toLocaleString()}
                  </span>
                  <span className={`px-2 py-0.5 text-xs rounded ${
                    decision.executed ? 'bg-green-600' : 'bg-slate-600'
                  }`}>
                    {decision.executed ? 'Executed' : 'Not Executed'}
                  </span>
                </div>
                <div className="space-y-2">
                  {parsed.map((dec: any, i: number) => (
                    <div key={i} className="bg-slate-700 rounded p-3">
                      <div className="flex justify-between items-center mb-2">
                        <span className="font-medium">{dec.symbol}</span>
                        <span className={`px-2 py-0.5 rounded text-sm ${
                          dec.action === 'BUY' ? 'bg-green-600' :
                          dec.action === 'SELL' ? 'bg-red-600' :
                          dec.action === 'CLOSE' ? 'bg-yellow-600' :
                          'bg-slate-600'
                        }`}>
                          {dec.action}
                        </span>
                      </div>
                      <div className="grid grid-cols-4 gap-2 text-sm text-slate-400">
                        <span>Confidence: {dec.confidence}%</span>
                        {dec.entry_price && <span>Entry: ${dec.entry_price}</span>}
                        {dec.stop_loss && <span>SL: ${dec.stop_loss}</span>}
                        {dec.take_profit && <span>TP: ${dec.take_profit}</span>}
                      </div>
                      {dec.reasoning && (
                        <p className="text-sm text-slate-300 mt-2">{dec.reasoning}</p>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
