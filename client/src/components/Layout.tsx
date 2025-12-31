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
  Zap,
  Menu,
  X
} from 'lucide-react';
import { motion, AnimatePresence } from 'framer-motion';

const navItems = [
  { to: '/', icon: LayoutDashboard, label: 'Dashboard', description: 'Monitor bots' },
  { to: '/backtest', icon: FlaskConical, label: 'Backtest', description: 'Test strategies' },
  { to: '/debate', icon: MessageSquare, label: 'Debate', description: 'AI consensus' },
  { to: '/equity', icon: TrendingUp, label: 'Equity', description: 'Performance' },
  { to: '/history', icon: History, label: 'History', description: 'Trade log' },
  { to: '/strategies', icon: Sparkles, label: 'Strategies', description: 'Define rules' },
  { to: '/config', icon: Settings, label: 'Config', description: 'API keys' },
  { to: '/logs', icon: Activity, label: 'Logs', description: 'AI decisions' },
];

export default function Layout() {
  const location = useLocation();
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState(false);

  // Close mobile menu on route change
  useEffect(() => {
    setIsMobileMenuOpen(false);
  }, [location.pathname]);

  // Close mobile menu when screen becomes large
  useEffect(() => {
    const handleResize = () => {
      if (window.innerWidth >= 1024) {
        setIsMobileMenuOpen(false);
      }
    };
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
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
                className={`group flex items-center gap-3 px-3 lg:px-4 py-2.5 lg:py-3 rounded-xl transition-all duration-200 ${
                  isActive
                    ? 'bg-primary/20 text-white glow-border'
                    : 'text-muted-foreground hover:text-white hover:bg-white/5'
                }`}
              >
                <div className={`p-1.5 lg:p-2 rounded-lg transition-colors ${
                  isActive
                    ? 'bg-primary/30'
                    : 'bg-white/5 group-hover:bg-white/10'
                }`}>
                  <item.icon className="w-4 h-4" />
                </div>
                <div className="flex-1 min-w-0">
                  <span className="font-medium text-sm">{item.label}</span>
                  <p className={`text-xs transition-colors truncate ${
                    isActive ? 'text-white/60' : 'text-muted-foreground/60'
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

      {/* Status Footer */}
      <div className="p-2 lg:p-4 border-t border-white/5">
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
      {/* Mobile Header */}
      <div className="lg:hidden fixed top-0 left-0 right-0 z-50 glass-sidebar border-b border-white/5">
        <div className="flex items-center justify-between p-3">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center">
              <Zap className="w-4 h-4 text-white" />
            </div>
            <span className="font-bold text-gradient">Passive Income</span>
          </div>
          <button
            onClick={() => setIsMobileMenuOpen(!isMobileMenuOpen)}
            className="p-2 rounded-lg hover:bg-white/10 transition-colors"
          >
            {isMobileMenuOpen ? <X className="w-6 h-6" /> : <Menu className="w-6 h-6" />}
          </button>
        </div>
      </div>

      {/* Mobile Sidebar Overlay */}
      <AnimatePresence>
        {isMobileMenuOpen && (
          <>
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              onClick={() => setIsMobileMenuOpen(false)}
              className="lg:hidden fixed inset-0 bg-black/60 z-40"
            />
            <motion.aside
              initial={{ x: '-100%' }}
              animate={{ x: 0 }}
              exit={{ x: '-100%' }}
              transition={{ type: 'spring', damping: 25, stiffness: 300 }}
              className="lg:hidden fixed top-0 left-0 bottom-0 w-72 glass-sidebar flex flex-col z-50 pt-16"
            >
              <SidebarContent />
            </motion.aside>
          </>
        )}
      </AnimatePresence>

      {/* Desktop Sidebar */}
      <aside className="hidden lg:flex w-72 glass-sidebar flex-col flex-shrink-0">
        <SidebarContent />
      </aside>

      {/* Main Content */}
      <main className="flex-1 overflow-auto pt-14 lg:pt-0">
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
    </div>
  );
}
