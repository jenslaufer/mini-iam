import axios from 'axios'
import { useAuthStore } from '../stores/auth.js'
import { useTenantStore } from '../stores/tenant.js'
import router from '../router/index.js'

const apiClient = axios.create({
  baseURL: import.meta.env.VITE_API_URL || '/auth',
})

apiClient.interceptors.request.use((config) => {
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
  (err) => {
    if (err.response?.status === 401 && !logoutInProgress) {
      logoutInProgress = true
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
