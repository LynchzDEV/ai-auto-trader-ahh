import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import Layout from './components/Layout';
import AuthGate from './components/AuthGate';
import Dashboard from './pages/Dashboard';
import Strategies from './pages/Strategies';
import Config from './pages/Config';
import Logs from './pages/Logs';
import Backtest from './pages/Backtest';
import Debate from './pages/Debate';
import Equity from './pages/Equity';
import History from './pages/History';

function App() {
  return (
    <AuthGate>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Layout />}>
            <Route index element={<Dashboard />} />
            <Route path="backtest" element={<Backtest />} />
            <Route path="debate" element={<Debate />} />
            <Route path="equity" element={<Equity />} />
            <Route path="history" element={<History />} />
            <Route path="strategies" element={<Strategies />} />
            <Route path="config" element={<Config />} />
            <Route path="logs" element={<Logs />} />
            {/* Catch-all: redirect non-existent paths to root */}
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </AuthGate>
  );
}

export default App;
