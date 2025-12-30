import { useEffect, useState } from 'react';
import { getTraders, getStatus, getPositions, startTrader, stopTrader } from '../lib/api';
import type { Trader, Position } from '../types';
import { Play, Square, RefreshCw } from 'lucide-react';

export default function Dashboard() {
  const [traders, setTraders] = useState<Trader[]>([]);
  const [selectedTrader, setSelectedTrader] = useState<string | null>(null);
  const [status, setStatus] = useState<any>(null);
  const [positions, setPositions] = useState<Position[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    loadTraders();
  }, []);

  useEffect(() => {
    if (selectedTrader) {
      loadTraderData();
      const interval = setInterval(loadTraderData, 10000);
      return () => clearInterval(interval);
    }
  }, [selectedTrader]);

  const loadTraders = async () => {
    try {
      const res = await getTraders();
      setTraders(res.data.traders || []);
      if (res.data.traders?.length > 0 && !selectedTrader) {
        setSelectedTrader(res.data.traders[0].id);
      }
    } catch (err) {
      console.error('Failed to load traders:', err);
    } finally {
      setLoading(false);
    }
  };

  const loadTraderData = async () => {
    if (!selectedTrader) return;
    try {
      const [statusRes, positionsRes] = await Promise.all([
        getStatus(selectedTrader),
        getPositions(selectedTrader),
      ]);
      setStatus(statusRes.data);
      setPositions(positionsRes.data.positions || []);
    } catch (err) {
      console.error('Failed to load trader data:', err);
    }
  };

  const handleStart = async (id: string) => {
    try {
      await startTrader(id);
      loadTraders();
      loadTraderData();
    } catch (err: any) {
      alert(err.response?.data?.error || 'Failed to start trader');
    }
  };

  const handleStop = async (id: string) => {
    try {
      await stopTrader(id);
      loadTraders();
      loadTraderData();
    } catch (err) {
      console.error('Failed to stop trader:', err);
    }
  };

  if (loading) {
    return <div className="p-8 text-center">Loading...</div>;
  }

  return (
    <div className="p-6 space-y-6">
      <h1 className="text-2xl font-bold">Dashboard</h1>

      {/* Trader Selection */}
      <div className="bg-gray-800 rounded-lg p-4">
        <h2 className="text-lg font-semibold mb-4">Traders</h2>
        {traders.length === 0 ? (
          <p className="text-gray-400">No traders configured. Go to Config to create one.</p>
        ) : (
          <div className="space-y-2">
            {traders.map((trader) => (
              <div
                key={trader.id}
                className={`flex items-center justify-between p-3 rounded ${
                  selectedTrader === trader.id ? 'bg-blue-600' : 'bg-gray-700 hover:bg-gray-600'
                } cursor-pointer`}
                onClick={() => setSelectedTrader(trader.id)}
              >
                <div>
                  <span className="font-medium">{trader.name}</span>
                  <span className={`ml-2 px-2 py-0.5 text-xs rounded ${
                    trader.is_running ? 'bg-green-500' : 'bg-gray-500'
                  }`}>
                    {trader.is_running ? 'Running' : 'Stopped'}
                  </span>
                </div>
                <div className="flex gap-2">
                  {trader.is_running ? (
                    <button
                      onClick={(e) => { e.stopPropagation(); handleStop(trader.id); }}
                      className="p-2 bg-red-600 hover:bg-red-500 rounded"
                    >
                      <Square size={16} />
                    </button>
                  ) : (
                    <button
                      onClick={(e) => { e.stopPropagation(); handleStart(trader.id); }}
                      className="p-2 bg-green-600 hover:bg-green-500 rounded"
                    >
                      <Play size={16} />
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Status & Positions */}
      {selectedTrader && status && (
        <>
          {/* Account Info */}
          <div className="bg-gray-800 rounded-lg p-4">
            <div className="flex justify-between items-center mb-4">
              <h2 className="text-lg font-semibold">Status</h2>
              <button onClick={loadTraderData} className="p-2 hover:bg-gray-700 rounded">
                <RefreshCw size={16} />
              </button>
            </div>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              <div>
                <p className="text-gray-400 text-sm">Strategy</p>
                <p className="font-medium">{status.strategy || 'Default'}</p>
              </div>
              <div>
                <p className="text-gray-400 text-sm">Status</p>
                <p className={`font-medium ${status.running ? 'text-green-400' : 'text-gray-400'}`}>
                  {status.running ? 'Running' : 'Stopped'}
                </p>
              </div>
              <div>
                <p className="text-gray-400 text-sm">Trading Pairs</p>
                <p className="font-medium">{status.pairs?.join(', ') || '-'}</p>
              </div>
              <div>
                <p className="text-gray-400 text-sm">Active Positions</p>
                <p className="font-medium">{positions.length}</p>
              </div>
            </div>
          </div>

          {/* Positions */}
          <div className="bg-gray-800 rounded-lg p-4">
            <h2 className="text-lg font-semibold mb-4">Positions</h2>
            {positions.length === 0 ? (
              <p className="text-gray-400">No open positions</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full">
                  <thead>
                    <tr className="text-left text-gray-400 border-b border-gray-700">
                      <th className="pb-2">Symbol</th>
                      <th className="pb-2">Side</th>
                      <th className="pb-2">Size</th>
                      <th className="pb-2">Entry</th>
                      <th className="pb-2">Mark</th>
                      <th className="pb-2">PnL</th>
                      <th className="pb-2">PnL %</th>
                    </tr>
                  </thead>
                  <tbody>
                    {positions.map((pos, i) => (
                      <tr key={i} className="border-b border-gray-700">
                        <td className="py-2">{pos.symbol}</td>
                        <td className={pos.side === 'LONG' ? 'text-green-400' : 'text-red-400'}>
                          {pos.side}
                        </td>
                        <td>{Math.abs(pos.amount).toFixed(4)}</td>
                        <td>${pos.entry_price.toFixed(2)}</td>
                        <td>${pos.mark_price.toFixed(2)}</td>
                        <td className={pos.pnl >= 0 ? 'text-green-400' : 'text-red-400'}>
                          ${pos.pnl.toFixed(2)}
                        </td>
                        <td className={pos.pnl_percent >= 0 ? 'text-green-400' : 'text-red-400'}>
                          {pos.pnl_percent.toFixed(2)}%
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

          {/* Latest Decisions */}
          {status.decisions && Object.keys(status.decisions).length > 0 && (
            <div className="bg-gray-800 rounded-lg p-4">
              <h2 className="text-lg font-semibold mb-4">Latest AI Decisions</h2>
              <div className="space-y-3">
                {Object.entries(status.decisions).map(([symbol, dec]: [string, any]) => (
                  <div key={symbol} className="bg-gray-700 rounded p-3">
                    <div className="flex justify-between items-center mb-2">
                      <span className="font-medium">{symbol}</span>
                      <span className={`px-2 py-0.5 rounded text-sm ${
                        dec.action === 'BUY' ? 'bg-green-600' :
                        dec.action === 'SELL' ? 'bg-red-600' :
                        dec.action === 'CLOSE' ? 'bg-yellow-600' :
                        'bg-gray-600'
                      }`}>
                        {dec.action}
                      </span>
                    </div>
                    <p className="text-sm text-gray-300">Confidence: {dec.confidence}%</p>
                    <p className="text-sm text-gray-400 mt-1">{dec.reasoning}</p>
                  </div>
                ))}
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}
