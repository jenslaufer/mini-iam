import { createRouter, createWebHistory } from 'vue-router'
import { auth } from '../stores/auth.js'

const routes = [
  { path: '/', component: () => import('../views/Home.vue') },
  { path: '/register', component: () => import('../views/Register.vue') },
  { path: '/login', component: () => import('../views/Login.vue') },
  { path: '/activate/:token', component: () => import('../views/Activate.vue') },
  {
    path: '/dashboard',
    component: () => import('../views/Dashboard.vue'),
    meta: { requiresAuth: true },
  },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach((to) => {
  if (to.meta.requiresAuth && !auth.token) return '/login'
})
