import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 管理后台挂在 Go 服务的 /admin/ 下，构建产物直接输出到 server/webassets/dist，
// 由 Go 用 embed 打包进二进制。
export default defineConfig({
  plugins: [vue()],
  base: '/admin/',
  server: {
    port: 5174,
    proxy: {
      '/admin/api': {
        target: 'http://127.0.0.1:8088',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: '../server/webassets/dist',
    emptyOutDir: true,
  },
})
