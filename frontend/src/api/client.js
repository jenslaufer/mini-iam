import axios from 'axios'
import { useAuthStore } from '../stores/auth.js'
import router from '../router/index.js'

const apiClient = axios.create({
  baseURL: import.meta.env.VITE_API_URL || '/auth',
})

apiClient.interceptors.request.use((config) => {
  const auth = useAuthStore()
  if (auth.token) {
    config.headers.Authorization = `Bearer ${auth.token}`
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
