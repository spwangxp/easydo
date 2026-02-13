import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import * as ElementPlusIconsVue from '@element-plus/icons-vue'
import App from './App.vue'
import router from './router'
import './assets/styles/main.scss'

const app = createApp(App)

// Load version information dynamically from the generated version.js file
const loadVersion = () => {
  const script = document.createElement('script')
  script.src = '/assets/version.js'
  script.onload = () => {
    if (window.__VERSION__) {
      console.log('[INFO] ' + window.__VERSION__.toString())
      console.log('[INFO] EasyDo Frontend Version Details:', JSON.stringify(window.__VERSION__, null, 2))
    }
  }
  script.onerror = () => {
    console.log('[INFO] EasyDo Frontend v1.0.0 (version info not available)')
  }
  document.head.appendChild(script)
}

// Initialize version loading
if (typeof document !== 'undefined') {
  loadVersion()
}

// 注册所有 Element Plus 图标
for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(key, component)
}

app.use(createPinia())
app.use(router)
app.use(ElementPlus)

app.mount('#app')
