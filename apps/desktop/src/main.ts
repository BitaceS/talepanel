import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import './style.css'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/connect' },
    { path: '/connect', component: () => import('./views/Connect.vue') },
    { path: '/dashboard', component: () => import('./views/Dashboard.vue') },
    { path: '/servers', component: () => import('./views/Servers.vue') },
    { path: '/servers/:id', component: () => import('./views/ServerDetail.vue') },
  ],
})

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.mount('#app')
