import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth.js'
import AppLayout from '../components/AppLayout.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/dashboard' },
    {
      path: '/login',
      component: () => import('../views/LoginView.vue'),
    },
    {
      path: '/',
      component: AppLayout,
      children: [
        { path: 'dashboard', component: () => import('../views/DashboardView.vue') },
        { path: 'users', component: () => import('../views/UsersView.vue') },
        { path: 'clients', component: () => import('../views/ClientsView.vue') },
      ],
    },
  ],
})

router.beforeEach((to) => {
  const auth = useAuthStore()
  if (to.path !== '/login' && !auth.token) {
    return '/login'
  }
  if (to.path === '/login' && auth.token) {
    return '/dashboard'
  }
})

export default router
