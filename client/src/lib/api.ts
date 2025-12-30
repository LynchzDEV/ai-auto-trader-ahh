import axios from 'axios';

const API_BASE = 'http://localhost:8080/api';

const api = axios.create({
  baseURL: API_BASE,
  headers: {
    'Content-Type': 'application/json',
  },
});

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

export default api;
