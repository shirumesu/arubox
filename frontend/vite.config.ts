import { fileURLToPath, URL } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";
import { defineConfig } from "vite";

export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
    // The app icon is imported from build/icons.
    fs: { allow: [".."] },
  },
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
      "@bindings": fileURLToPath(new URL("./bindings/github.com/shirumesu/arubox/internal", import.meta.url)),
    },
  },
  build: {
    // One page per window, so each loads only its own code.
    rolldownOptions: {
      input: {
        dashboard: fileURLToPath(new URL("./index.html", import.meta.url)),
        translator: fileURLToPath(new URL("./translator.html", import.meta.url)),
        capture: fileURLToPath(new URL("./capture.html", import.meta.url)),
        launcher: fileURLToPath(new URL("./launcher.html", import.meta.url)),
        screenshot: fileURLToPath(new URL("./screenshot.html", import.meta.url)),
        pin: fileURLToPath(new URL("./pin.html", import.meta.url)),
      },
    },
  },
  plugins: [react(), tailwindcss(), wails("./bindings")],
});
