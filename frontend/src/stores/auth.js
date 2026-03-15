import { defineStore } from 'pinia'
import { ref } from 'vue'
import { loginApi } from '../api/auth.js'

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

  async function login(email, password) {
    const data = await loginApi(email, password)
    const payload = decodeJwt(data.access_token)
    if (payload?.role !== 'admin') {
      throw new Error('Access denied: admin role required')
    }
    token.value = data.access_token
    adminEmail.value = email
    localStorage.setItem('access_token', data.access_token)
    localStorage.setItem('admin_email', email)
  }

  function logout() {
    token.value = ''
    adminEmail.value = ''
    localStorage.removeItem('access_token')
    localStorage.removeItem('admin_email')
  }

  return { token, adminEmail, login, logout }
})
