import { defineConfig } from "vite";

export default defineConfig({
  server: {
    proxy: {
      "/feed": { target: "http://localhost:8080", changeOrigin: true },
    },
  },
});
