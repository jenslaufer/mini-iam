import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth.js'
import AppLayout from '../components/AppLayout.vue'

let restored = false

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
        { path: 'contacts', component: () => import('../views/ContactsView.vue') },
        { path: 'segments', component: () => import('../views/SegmentsView.vue') },
        { path: 'campaigns', component: () => import('../views/CampaignsView.vue') },
        { path: 'tenants', component: () => import('../views/TenantsView.vue') },
        { path: 'settings', component: () => import('../views/SettingsView.vue') },
      ],
    },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (to.path !== '/login' && !auth.token) {
    return '/login'
  }
  if (to.path === '/login' && auth.token) {
    return '/dashboard'
  }
  if (auth.token && !restored) {
    restored = true
    await auth.restore()
  }
})

export default router
