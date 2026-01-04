import { createContext, useContext } from 'react';

interface AuthContextType {
    logout: () => void;
    isAuthenticated: boolean;
    authRequired: boolean;
}

export const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function useAuth() {
    const context = useContext(AuthContext);
    if (context === undefined) {
        throw new Error('useAuth must be used within an AuthGate');
    }
    return context;
}
