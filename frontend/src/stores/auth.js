import { defineStore } from 'pinia'
import { ref } from 'vue'
import { loginApi } from '../api/auth.js'
import { useTenantStore } from './tenant.js'

function decodeJwt(token) {
  try {
    return JSON.parse(atob(token.split('.')[1]))
  } catch {
    return null
  }
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref(localStorage.getItem('access_token') || '')
  const adminEmail = ref(localStorage.getItem('admin_email') || '')

  async function login(email, password, tenantSlug = '') {
    const data = await loginApi(email, password, tenantSlug)
    const payload = decodeJwt(data.access_token)
    if (payload?.role !== 'admin') {
      throw new Error('Access denied: admin role required')
    }
    token.value = data.access_token
    adminEmail.value = email
    localStorage.setItem('access_token', data.access_token)
    localStorage.setItem('admin_email', email)

    const tenantStore = useTenantStore()
    tenantStore.platformTenantId = payload.tid || ''

    // Try loading tenants list — only platform admins can access this
    await tenantStore.loadTenants()
    tenantStore.isPlatformAdmin = tenantStore.tenants.length > 0

    if (tenantStore.isPlatformAdmin && !tenantStore.selectedSlug && tenantStore.tenants.length > 0) {
      tenantStore.select(tenantStore.tenants[0].slug)
    } else if (!tenantStore.isPlatformAdmin && tenantSlug) {
      // Tenant admin: lock to their own tenant
      tenantStore.select(tenantSlug)
    }
  }

  function logout() {
    token.value = ''
    adminEmail.value = ''
    localStorage.removeItem('access_token')
    localStorage.removeItem('admin_email')
    const tenantStore = useTenantStore()
    tenantStore.reset()
  }

  async function restore() {
    if (!token.value) return
    const payload = decodeJwt(token.value)
    if (!payload) return
    const tenantStore = useTenantStore()
    tenantStore.platformTenantId = payload.tid || ''
    await tenantStore.loadTenants()
    tenantStore.isPlatformAdmin = tenantStore.tenants.length > 0
  }

  return { token, adminEmail, login, logout, restore }
})
