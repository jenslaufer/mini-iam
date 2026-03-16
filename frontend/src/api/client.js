import axios from 'axios'
import { useAuthStore } from '../stores/auth.js'
import router from '../router/index.js'

function getTenantSlug() {
  const parts = window.location.hostname.split('.')
  if (parts.length >= 3) {
    return parts[0]
  }
  return ''
}

const apiClient = axios.create({
  baseURL: import.meta.env.VITE_API_URL || '/auth',
})

apiClient.interceptors.request.use((config) => {
  const auth = useAuthStore()
  if (auth.token) {
    config.headers.Authorization = `Bearer ${auth.token}`
  }
  const tenant = getTenantSlug()
  if (tenant) {
    config.headers['X-Tenant'] = tenant
  }
  return config
})

apiClient.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401) {
      const auth = useAuthStore()
      auth.logout()
      router.push('/login')
    }
    return Promise.reject(err)
  },
)

export default apiClient
