import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Detects if the current device is a mobile/touch device
 * Checks for touch capability and small screen size
 */
export function isMobileDevice(): boolean {
  // Check if running in browser environment
  if (typeof window === 'undefined') return false;
  
  // Check for touch capability
  const hasTouchScreen = 'ontouchstart' in window || navigator.maxTouchPoints > 0;
  
  // Check for small screen size (mobile breakpoint: 768px)
  const isSmallScreen = window.innerWidth <= 768;
  
  // Consider it mobile if it has touch AND small screen
  // This prevents tablets with keyboards from being treated as mobile
  return hasTouchScreen && isSmallScreen;
}
