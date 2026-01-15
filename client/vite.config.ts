import { defineConfig, loadEnv, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";
import fs from "fs";
import type { OutputOptions } from "rollup";

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  // Load env file based on mode
  const env = loadEnv(mode, process.cwd(), "");

  // SEO configuration with fallbacks
  const seoConfig = {
    siteUrl: env.VITE_SITE_URL || "https://trade.lynchz.dev",
    siteName: env.VITE_SITE_NAME || "Passive Income Ahh",
    siteDescription:
      env.VITE_SITE_DESCRIPTION ||
      "AI-powered cryptocurrency trading platform using OpenRouter and Binance Futures. Automate your trading with multi-AI debate consensus, backtesting, and real-time portfolio management.",
    twitterHandle: env.VITE_TWITTER_HANDLE || "",
    fbAppId: env.VITE_FB_APP_ID || "",
  };

  return {
    plugins: [
      react(),
      tailwindcss(),
      // Custom HTML transform plugin for SEO meta tags
      {
        name: "html-transform",
        transformIndexHtml(html) {
          return html
            .replace(/%VITE_SITE_URL%/g, seoConfig.siteUrl)
            .replace(/%VITE_SITE_NAME%/g, seoConfig.siteName)
            .replace(/%VITE_SITE_DESCRIPTION%/g, seoConfig.siteDescription)
            .replace(/%VITE_TWITTER_HANDLE%/g, seoConfig.twitterHandle)
            .replace(/%VITE_FB_APP_ID%/g, seoConfig.fbAppId);
        },
        // Transform robots.txt and sitemap.xml after build
        writeBundle(options: OutputOptions) {
          const outDir = options.dir || "dist";
          const filesToTransform = ["robots.txt", "sitemap.xml"];

          for (const file of filesToTransform) {
            const filePath = path.join(outDir, file);
            if (fs.existsSync(filePath)) {
              let content = fs.readFileSync(filePath, "utf-8");
              content = content.replace(/%VITE_SITE_URL%/g, seoConfig.siteUrl);
              fs.writeFileSync(filePath, content);
            }
          }
        },
      } as Plugin,
    ],
    resolve: {
      alias: {
        "@": path.resolve(__dirname, "./src"),
      },
    },
    build: {
      chunkSizeWarningLimit: 1000,
      rollupOptions: {
        output: {
          manualChunks: {
            "vendor-react": ["react", "react-dom", "react-router-dom"],
            "vendor-ui": ["framer-motion", "lucide-react", "sonner"],
            "vendor-radix": [
              "@radix-ui/react-accordion",
              "@radix-ui/react-avatar",
              "@radix-ui/react-checkbox",
              "@radix-ui/react-collapsible",
              "@radix-ui/react-dialog",
              "@radix-ui/react-dropdown-menu",
              "@radix-ui/react-label",
              "@radix-ui/react-progress",
              "@radix-ui/react-scroll-area",
              "@radix-ui/react-select",
              "@radix-ui/react-separator",
              "@radix-ui/react-slider",
              "@radix-ui/react-slot",
              "@radix-ui/react-switch",
              "@radix-ui/react-tabs",
              "@radix-ui/react-tooltip",
            ],
            "vendor-charts": ["recharts", "d3-force"],
            "vendor-utils": [
              "axios",
              "@tanstack/react-query",
              "clsx",
              "tailwind-merge",
            ],
          },
        },
      },
    },
  };
});
