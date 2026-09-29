import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";

// Wails v3 官方约定：
//   - 开发端口由 wails3 dev 注入 WAILS_VITE_PORT（默认 9245）
//   - dev 模式下 Go 资产服务器通过 FRONTEND_DEVSERVER_URL 代理到本 Vite 服务器
//   - 产物输出 dist/，由 main.go 的 //go:embed all:frontend/dist 嵌入
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [svelte(), tailwindcss()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    target: "chrome120",
  },
});
