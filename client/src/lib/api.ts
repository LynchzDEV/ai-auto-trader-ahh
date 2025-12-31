import axios from 'axios';

// Use relative path in production (nginx will proxy to server)
// Use localhost in development
const API_BASE = import.meta.env.PROD ? '/api' : 'http://localhost:8080/api';

// Storage key for access passkey
const ACCESS_KEY_STORAGE = 'trader_access_key';

const api = axios.create({
  baseURL: API_BASE,
  headers: {
    'Content-Type': 'application/json',
  },
});

// Add access key to all requests if available
api.interceptors.request.use((config) => {
  const accessKey = localStorage.getItem(ACCESS_KEY_STORAGE);
  if (accessKey) {
    config.headers['X-Access-Key'] = accessKey;
  }
  return config;
});

// Auth API
export const verifyPasskey = (passkey: string) =>
  axios.post(`${API_BASE}/auth/verify`, { passkey });

export const setAccessKey = (key: string) => {
  localStorage.setItem(ACCESS_KEY_STORAGE, key);
};

export const clearAccessKey = () => {
  localStorage.removeItem(ACCESS_KEY_STORAGE);
};

export const getStoredAccessKey = () => {
  return localStorage.getItem(ACCESS_KEY_STORAGE);
};

export const isAuthenticated = () => {
  return !!localStorage.getItem(ACCESS_KEY_STORAGE);
};

// Strategy API
export const getStrategies = () => api.get('/strategies');
export const getStrategy = (id: string) => api.get(`/strategies/${id}`);
export const createStrategy = (data: any) => api.post('/strategies', data);
export const updateStrategy = (id: string, data: any) => api.put(`/strategies/${id}`, data);
export const deleteStrategy = (id: string) => api.delete(`/strategies/${id}`);
export const activateStrategy = (id: string) => api.post(`/strategies/${id}/activate`);
export const getDefaultConfig = () => api.get('/strategies/default-config');

// Trader API
export const getTraders = () => api.get('/traders');
export const getTrader = (id: string) => api.get(`/traders/${id}`);
export const createTrader = (data: any) => api.post('/traders', data);
export const updateTrader = (id: string, data: any) => api.put(`/traders/${id}`, data);
export const deleteTrader = (id: string) => api.delete(`/traders/${id}`);
export const startTrader = (id: string) => api.post(`/traders/${id}/start`);
export const stopTrader = (id: string) => api.post(`/traders/${id}/stop`);

// Data API
export const getStatus = (traderId: string) => api.get(`/status?trader_id=${traderId}`);
export const getAccount = (traderId: string) => api.get(`/account?trader_id=${traderId}`);
export const getPositions = (traderId: string) => api.get(`/positions?trader_id=${traderId}`);
export const getDecisions = (traderId: string) => api.get(`/decisions?trader_id=${traderId}`);
export const getEquityHistory = (traderId: string) => api.get(`/equity-history?trader_id=${traderId}`);

// Health
export const getHealth = () => api.get('/health');

// Backtest API
export const listBacktests = () => api.get('/backtest');
export const startBacktest = (data: any) => api.post('/backtest/start', data);
export const stopBacktest = (runId: string) => api.post(`/backtest/${runId}/stop`);
export const getBacktestStatus = (runId: string) => api.get(`/backtest/${runId}/status`);
export const getBacktestMetrics = (runId: string) => api.get(`/backtest/${runId}/metrics`);
export const getBacktestEquity = (runId: string) => api.get(`/backtest/${runId}/equity`);
export const getBacktestTrades = (runId: string) => api.get(`/backtest/${runId}/trades`);
export const deleteBacktest = (runId: string) => api.delete(`/backtest/${runId}`);

// Debate API
export const listDebates = () => api.get('/debate/sessions');
export const createDebate = (data: any) => api.post('/debate/sessions', data);
export const getDebate = (sessionId: string) => api.get(`/debate/sessions/${sessionId}`);
export const startDebate = (sessionId: string) => api.post(`/debate/sessions/${sessionId}/start`);
export const stopDebate = (sessionId: string) => api.post(`/debate/sessions/${sessionId}/stop`);
export const deleteDebate = (sessionId: string) => api.delete(`/debate/sessions/${sessionId}`);

export default api;

