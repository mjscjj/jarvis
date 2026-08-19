import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  base: "./",
  plugins: [react()],
  server: {
    port: 4176,
    strictPort: true,
  },
  preview: {
    port: 4177,
    strictPort: true,
  },
});
