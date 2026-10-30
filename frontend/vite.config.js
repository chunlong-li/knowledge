import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 相对基路径：既可在根路径独立部署，也可被族谱系统反代到 /knowledge/ 子路径
export default defineConfig({
  base: './',
  plugins: [vue()],
  server: {
    port: 5190,
    proxy: {
      '/api': { target: 'http://localhost:8090', changeOrigin: true },
      '/upload': { target: 'http://localhost:8090', changeOrigin: true }
    }
  },
  build: { outDir: 'dist' }
})
