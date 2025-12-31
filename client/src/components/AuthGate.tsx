import { useState, useEffect, type ReactNode } from 'react';
import { verifyPasskey, setAccessKey, getStoredAccessKey, clearAccessKey } from '../lib/api';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Lock, Unlock, AlertCircle, Loader2, Shield, LogOut } from 'lucide-react';

interface AuthGateProps {
    children: ReactNode;
}

export default function AuthGate({ children }: AuthGateProps) {
    const [isAuthenticated, setIsAuthenticated] = useState(false);
    const [isLoading, setIsLoading] = useState(true);
    const [passkey, setPasskey] = useState('');
    const [error, setError] = useState('');
    const [isVerifying, setIsVerifying] = useState(false);
    const [authRequired, setAuthRequired] = useState(true);
    const [connectionError, setConnectionError] = useState(false);

    useEffect(() => {
        checkAuth();
    }, []);

    const checkAuth = async () => {
        setConnectionError(false);
        const storedKey = getStoredAccessKey();

        if (storedKey) {
            // Verify the stored key is still valid
            try {
                const response = await verifyPasskey(storedKey);
                if (response.data.valid) {
                    setIsAuthenticated(true);
                    setAuthRequired(response.data.required);
                } else {
                    // Stored key is invalid, clear it
                    clearAccessKey();
                    setAuthRequired(response.data.required);
                    // Don't auto-authenticate, show login screen
                }
            } catch (err) {
                // If verify fails, could be network error - show login but don't mark as connection error
                // since they might have an old stored key that's invalid
                clearAccessKey();
            }
        } else {
            // No stored key - check if auth is required at all
            try {
                const response = await verifyPasskey('');
                if (response.data.valid && !response.data.required) {
                    // Server has no passkey configured, allow access
                    setIsAuthenticated(true);
                    setAuthRequired(false);
                }
                // If auth is required but no stored key, show login screen (do nothing)
            } catch (err) {
                // Network error - mark it so we can show retry option
                setConnectionError(true);
            }
        }

        // Small delay to prevent flash
        await new Promise(resolve => setTimeout(resolve, 100));
        setIsLoading(false);
    };

    const handleSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setError('');
        setConnectionError(false);
        setIsVerifying(true);

        try {
            const response = await verifyPasskey(passkey);

            if (response.data.valid) {
                setAccessKey(passkey);
                setIsAuthenticated(true);
                setAuthRequired(response.data.required);
            } else {
                setError('Invalid passkey. Please try again.');
                setPasskey('');
            }
        } catch (err: any) {
            setConnectionError(true);
            setError('Unable to connect to server. Check your connection.');
        } finally {
            setIsVerifying(false);
        }
    };

    const handleLogout = () => {
        clearAccessKey();
        setIsAuthenticated(false);
        setPasskey('');
    };

    const handleRetry = () => {
        setIsLoading(true);
        setConnectionError(false);
        checkAuth();
    };

    if (isLoading) {
        return (
            <div className="min-h-screen bg-background flex items-center justify-center">
                <div className="text-center">
                    <Loader2 className="h-12 w-12 animate-spin text-primary mx-auto mb-4" />
                    <p className="text-muted-foreground">Initializing secure connection...</p>
                </div>
            </div>
        );
    }

    if (connectionError && !isAuthenticated) {
        return (
            <div className="min-h-screen bg-background flex items-center justify-center p-4">
                <div className="glass-card rounded-2xl p-8 max-w-md w-full text-center">
                    <AlertCircle className="h-16 w-16 text-destructive mx-auto mb-4" />
                    <h2 className="text-xl font-bold text-foreground mb-2">Connection Error</h2>
                    <p className="text-muted-foreground mb-6">
                        Unable to connect to the server. Please check your connection and try again.
                    </p>
                    <Button onClick={handleRetry} className="w-full">
                        <Loader2 className={`mr-2 h-4 w-4 ${isLoading ? 'animate-spin' : 'hidden'}`} />
                        Retry Connection
                    </Button>
                </div>
            </div>
        );
    }

    if (isAuthenticated) {
        return (
            <>
                {authRequired && (
                    <button
                        onClick={handleLogout}
                        className="fixed bottom-4 right-4 z-50 p-3 rounded-full bg-secondary/80 backdrop-blur-sm border border-border hover:bg-secondary transition-colors group"
                        title="Sign out"
                    >
                        <LogOut className="h-4 w-4 text-muted-foreground group-hover:text-foreground transition-colors" />
                    </button>
                )}
                {children}
            </>
        );
    }

    return (
        <div className="min-h-screen bg-background grid-bg flex items-center justify-center p-4">
            {/* Background effects */}
            <div className="fixed inset-0 overflow-hidden pointer-events-none">
                <div className="absolute top-1/4 left-1/4 w-96 h-96 bg-primary/10 rounded-full blur-3xl animate-pulse" />
                <div className="absolute bottom-1/4 right-1/4 w-96 h-96 bg-chart-4/10 rounded-full blur-3xl animate-pulse" style={{ animationDelay: '1s' }} />
            </div>

            <div className="relative w-full max-w-md">
                {/* Animated border card */}
                <div className="animated-border rounded-2xl p-[1px]">
                    <div className="glass-card rounded-2xl p-8 bg-background-secondary/90">
                        {/* Header */}
                        <div className="text-center mb-8">
                            <div className="inline-flex items-center justify-center w-16 h-16 rounded-full bg-primary/10 mb-4 glow-primary">
                                <Shield className="h-8 w-8 text-primary" />
                            </div>
                            <h1 className="text-2xl font-bold text-foreground mb-2">
                                Secure Access
                            </h1>
                            <p className="text-muted-foreground text-sm">
                                Enter your passkey to access the trading terminal
                            </p>
                        </div>

                        {/* Form */}
                        <form onSubmit={handleSubmit} className="space-y-6">
                            <div className="relative">
                                <div className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground">
                                    <Lock className="h-5 w-5" />
                                </div>
                                <Input
                                    type="password"
                                    placeholder="Enter passkey"
                                    value={passkey}
                                    onChange={(e) => setPasskey(e.target.value)}
                                    className="pl-11 h-12 bg-background/50 border-border/50 focus:border-primary/50 text-lg tracking-widest"
                                    autoFocus
                                    disabled={isVerifying}
                                />
                            </div>

                            {error && (
                                <div className="flex items-center gap-2 text-destructive text-sm bg-destructive/10 rounded-lg p-3">
                                    <AlertCircle className="h-4 w-4 flex-shrink-0" />
                                    <span>{error}</span>
                                </div>
                            )}

                            <Button
                                type="submit"
                                className="w-full h-12 text-lg font-medium bg-primary hover:bg-primary/90 glow-primary transition-all duration-300"
                                disabled={!passkey || isVerifying}
                            >
                                {isVerifying ? (
                                    <>
                                        <Loader2 className="mr-2 h-5 w-5 animate-spin" />
                                        Verifying...
                                    </>
                                ) : (
                                    <>
                                        <Unlock className="mr-2 h-5 w-5" />
                                        Unlock Terminal
                                    </>
                                )}
                            </Button>
                        </form>

                        {/* Footer */}
                        <div className="mt-8 pt-6 border-t border-border/50 text-center">
                            <p className="text-xs text-muted-foreground">
                                Protected by end-to-end encryption
                            </p>
                        </div>
                    </div>
                </div>

                {/* Decorative elements */}
                <div className="absolute -top-4 -left-4 w-24 h-24 border border-primary/20 rounded-full" />
                <div className="absolute -bottom-8 -right-8 w-32 h-32 border border-chart-4/20 rounded-full" />
            </div>
        </div>
    );
}
