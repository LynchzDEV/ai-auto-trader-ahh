import { useState, useEffect } from 'react';
import { NavLink, Outlet, useLocation } from 'react-router-dom';
import {
  LayoutDashboard,
  Settings,
  Sparkles,
  Activity,
  FlaskConical,
  MessageSquare,
  TrendingUp,
  History,
  Trophy,
  Zap,
  MoreHorizontal,
  X,
  LogOut
} from 'lucide-react';
import { motion, AnimatePresence } from 'framer-motion';
import { Dock, DockIcon, DockSeparator } from '@/components/ui/dock';

import { useAuth } from '../contexts/AuthContext';
import { API_BASE } from '@/lib/api';
import { toast } from 'sonner';

// All navigation items
const navItems = [
  { to: '/', icon: LayoutDashboard, label: 'Dashboard', description: 'Monitor bots' },
  { to: '/backtest', icon: FlaskConical, label: 'Backtest', description: 'Test strategies' },
  { to: '/debate', icon: MessageSquare, label: 'Debate', description: 'AI consensus' },
  { to: '/equity', icon: TrendingUp, label: 'Equity', description: 'Performance' },
  { to: '/history', icon: History, label: 'History', description: 'Trade log' },
  { to: '/ranking', icon: Trophy, label: 'Ranking', description: 'Symbol profits' },
  { to: '/strategies', icon: Sparkles, label: 'Strategies', description: 'Define rules' },
  { to: '/config', icon: Settings, label: 'Config', description: 'API keys' },
  { to: '/logs', icon: Activity, label: 'Logs', description: 'AI decisions' },
];

// Primary items shown in dock (first 6, including Ranking)
const primaryNavItems = navItems.slice(0, 6);
// Secondary items shown in "More" menu
const secondaryNavItems = navItems.slice(6);

export default function Layout() {
  const location = useLocation();
  const [isMoreOpen, setIsMoreOpen] = useState(false);
  const { logout, authRequired } = useAuth();

  // Listen for server events (SSE)
  useEffect(() => {
    const eventSource = new EventSource(`${API_BASE}/events`);

    eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        if (data.type === 'error') {
          toast.error(data.trader_id ? `Trader Alert: ${data.trader_id}` : 'System Alert', {
            description: data.message,
            duration: 8000,
          });
        }
      } catch (e) {
        console.error('Failed to parse event', e);
      }
    };

    eventSource.onerror = () => {
      // Optional: retry logic or silent fail. EventSource auto-retries.
      // console.error('SSE Error:', err);
      eventSource.close();
    };

    return () => {
      eventSource.close();
    };
  }, []);

  const SidebarContent = () => (
    <>
      {/* Logo */}
      <div className="p-4 lg:p-6 border-b border-white/5">
        <motion.div
          className="flex items-center gap-3"
          initial={{ opacity: 0, x: -20 }}
          animate={{ opacity: 1, x: 0 }}
        >
          <div className="w-10 h-10 rounded-xl bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center glow-primary">
            <Zap className="w-5 h-5 text-white" />
          </div>
          <div>
            <h1 className="text-lg lg:text-xl font-bold text-gradient">Passive Income</h1>
            <p className="text-xs text-muted-foreground">AI-Powered Trading Ahh</p>
          </div>
        </motion.div>
      </div>

      {/* Navigation */}
      <nav className="flex-1 p-2 lg:p-4 space-y-1 overflow-y-auto">
        {navItems.map((item, index) => {
          const isActive = location.pathname === item.to ||
            (item.to !== '/' && location.pathname.startsWith(item.to));

          return (
            <motion.div
              key={item.to}
              initial={{ opacity: 0, x: -20 }}
              animate={{ opacity: 1, x: 0 }}
              transition={{ delay: index * 0.05 }}
            >
              <NavLink
                to={item.to}
                className={`group flex items-center gap-3 px-3 lg:px-4 py-2.5 lg:py-3 rounded-xl transition-all duration-200 ${isActive
                  ? 'bg-primary/20 text-white glow-border'
                  : 'text-muted-foreground hover:text-white hover:bg-white/5'
                  }`}
              >
                <div className={`p-1.5 lg:p-2 rounded-lg transition-colors ${isActive
                  ? 'bg-primary/30'
                  : 'bg-white/5 group-hover:bg-white/10'
                  }`}>
                  <item.icon className="w-4 h-4" />
                </div>
                <div className="flex-1 min-w-0">
                  <span className="font-medium text-sm">{item.label}</span>
                  <p className={`text-xs transition-colors truncate ${isActive ? 'text-white/60' : 'text-muted-foreground/60'
                    }`}>
                    {item.description}
                  </p>
                </div>
                {isActive && (
                  <motion.div
                    layoutId="activeIndicator"
                    className="w-1 h-8 bg-primary rounded-full flex-shrink-0"
                  />
                )}
              </NavLink>
            </motion.div>
          );
        })}
      </nav>

      {/* Footer with Logout & Status */}
      <div className="p-2 lg:p-4 border-t border-white/5 space-y-3">
        {/* Logout Button */}
        {authRequired && (
          <button
            onClick={logout}
            className="w-full flex items-center gap-3 px-3 lg:px-4 py-2.5 lg:py-3 rounded-xl transition-all text-red-400 hover:text-red-300 hover:bg-red-400/10"
          >
            <div className="p-1.5 lg:p-2 rounded-lg bg-red-400/10">
              <LogOut className="w-4 h-4" />
            </div>
            <div className="flex-1 min-w-0 text-left">
              <span className="font-medium text-sm">Log Out</span>
              <p className="text-xs text-red-400/60">Sign out of session</p>
            </div>
          </button>
        )}

        {/* Status Card */}
        <div className="glass-card p-3 lg:p-4">
          <div className="flex items-center gap-2 mb-2">
            <div className="w-2 h-2 rounded-full bg-green-500 pulse-live" />
            <span className="text-xs text-muted-foreground">System Status</span>
          </div>
          <div className="grid grid-cols-2 gap-2 text-xs">
            <div>
              <span className="text-muted-foreground">API</span>
              <p className="text-green-400 font-medium">Connected</p>
            </div>
            <div>
              <span className="text-muted-foreground">Exchange</span>
              <p className="text-green-400 font-medium">Online</p>
            </div>
          </div>
        </div>
      </div>
    </>
  );

  return (
    <div className="flex min-h-screen bg-[#0a0a0f] grid-bg">
      {/* Desktop Sidebar */}
      <aside className="hidden lg:flex w-72 glass-sidebar flex-col flex-shrink-0">
        <SidebarContent />
      </aside>

      {/* Main Content */}
      <main className="flex-1 overflow-auto pb-24 lg:pb-0">
        <motion.div
          key={location.pathname}
          initial={{ opacity: 0, y: 10 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: -10 }}
          transition={{ duration: 0.2 }}
        >
          <Outlet />
        </motion.div>
      </main>

      {/* Mobile Bottom Dock */}
      <div className="lg:hidden fixed bottom-0 left-0 right-0 z-50 flex justify-center pb-safe">
        <div className="pb-2">
          <Dock>
            {primaryNavItems.map((item) => (
              <DockIcon key={item.to} to={item.to} label={item.label}>
                <item.icon className="w-5 h-5" />
              </DockIcon>
            ))}
            <DockSeparator />
            <DockIcon
              to="#"
              label="More"
              onClick={() => setIsMoreOpen(true)}
            >
              <MoreHorizontal className="w-5 h-5" />
            </DockIcon>
          </Dock>
        </div>
      </div>

      {/* More Menu Sheet */}
      <AnimatePresence>
        {isMoreOpen && (
          <>
            {/* Backdrop */}
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              onClick={() => setIsMoreOpen(false)}
              className="lg:hidden fixed inset-0 bg-black/60 backdrop-blur-sm z-50"
            />
            {/* Sheet */}
            <motion.div
              initial={{ y: '100%' }}
              animate={{ y: 0 }}
              exit={{ y: '100%' }}
              transition={{ type: 'spring', damping: 25, stiffness: 300 }}
              className="lg:hidden fixed bottom-0 left-0 right-0 z-50 bg-[#0c0c12] border-t border-white/10 rounded-t-3xl"
            >
              <div className="p-4">
                {/* Handle */}
                <div className="w-12 h-1 bg-white/20 rounded-full mx-auto mb-4" />

                {/* Header */}
                <div className="flex items-center justify-between mb-4">
                  <h3 className="text-lg font-semibold">More</h3>
                  <button
                    onClick={() => setIsMoreOpen(false)}
                    className="p-2 rounded-full hover:bg-white/10 transition-colors"
                  >
                    <X className="w-5 h-5" />
                  </button>
                </div>

                {/* Secondary Nav Items */}
                <div className="space-y-2">
                  {secondaryNavItems.map((item) => {
                    const isActive = location.pathname === item.to ||
                      (item.to !== '/' && location.pathname.startsWith(item.to));

                    return (
                      <NavLink
                        key={item.to}
                        to={item.to}
                        onClick={() => setIsMoreOpen(false)}
                        className={`flex items-center gap-3 p-3 rounded-xl transition-all ${isActive
                          ? 'bg-primary/20 text-white'
                          : 'text-muted-foreground hover:text-white hover:bg-white/5'
                          }`}
                      >
                        <div className={`p-2 rounded-lg ${isActive ? 'bg-primary/30' : 'bg-white/5'
                          }`}>
                          <item.icon className="w-5 h-5" />
                        </div>
                        <div>
                          <span className="font-medium">{item.label}</span>
                          <p className="text-xs text-muted-foreground">{item.description}</p>
                        </div>
                      </NavLink>
                    );
                  })}
                </div>

                {authRequired && (
                  <button
                    onClick={() => {
                      logout();
                      setIsMoreOpen(false);
                    }}
                    className="w-full flex items-center gap-3 p-3 rounded-xl transition-all text-red-400 hover:text-red-300 hover:bg-white/5 mt-2"
                  >
                    <div className="p-2 rounded-lg bg-red-400/10">
                      <LogOut className="w-5 h-5" />
                    </div>
                    <div>
                      <span className="font-medium">Log Out</span>
                      <p className="text-xs text-red-400/60">Sign out of session</p>
                    </div>
                  </button>
                )}

                {/* Status */}
                <div className="mt-4 p-3 glass-card">
                  <div className="flex items-center gap-2">
                    <div className="w-2 h-2 rounded-full bg-green-500 pulse-live" />
                    <span className="text-sm text-muted-foreground">System Online</span>
                  </div>
                </div>

                {/* Safe area spacer */}
                <div className="h-safe" />
              </div>
            </motion.div>
          </>
        )}
      </AnimatePresence>
    </div>
  );
}
