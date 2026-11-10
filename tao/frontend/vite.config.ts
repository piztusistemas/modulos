import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  define: {
    // The Gemini API key is wired through the Go backend / config screen in a
    // later phase. Define it here so build-time references don't fail.
    'process.env.GEMINI_API_KEY': JSON.stringify(''),
  },
})
