import axios from 'axios'
import router from '../router/index.js'

const apiClient = axios.create({
  baseURL: import.meta.env.VITE_API_URL || '/auth',
})

apiClient.interceptors.request.use(async (config) => {
  const { useAuthStore } = await import('../stores/auth.js')
  const { useTenantStore } = await import('../stores/tenant.js')
  const auth = useAuthStore()
  if (auth.token) {
    config.headers.Authorization = `Bearer ${auth.token}`
  }
  const tenantStore = useTenantStore()
  if (tenantStore.currentSlug) {
    config.headers['X-Tenant'] = tenantStore.currentSlug
  }
  return config
})

let logoutInProgress = false

apiClient.interceptors.response.use(
  (res) => res,
  async (err) => {
    if (err.response?.status === 401 && !logoutInProgress) {
      logoutInProgress = true
      const { useAuthStore } = await import('../stores/auth.js')
      const auth = useAuthStore()
      auth.logout()
      router.push('/login').finally(() => {
        logoutInProgress = false
      })
    }
    return Promise.reject(err)
  },
)

export default apiClient
