import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [tailwindcss(), react()],
  server: {
    // Windows 上 localhost 会优先解析到 ::1；部分环境禁止监听 IPv6 回环，
    // 会直接报 EACCES。固定到 IPv4 回环，避免依赖系统的 IPv6 配置。
    host: '127.0.0.1',
    // 固定使用 8080：5173 在部分 Windows 环境里会落在系统保留端口段，
    // 监听时直接 EACCES。strictPort 保证端口被占用时明确失败，而不是悄悄换端口。
    port: 8080,
    strictPort: true,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:3001',
        changeOrigin: true,
      },
      '/v1': {
        target: 'http://127.0.0.1:3001',
        changeOrigin: true,
      },
      '/health': {
        target: 'http://127.0.0.1:3001',
        changeOrigin: true,
      },
    },
  },
})
