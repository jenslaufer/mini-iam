import { reactive } from 'vue'
import { router } from '../router/index.js'

const OIDC_ISSUER_URL = (import.meta.env.VITE_OIDC_ISSUER_URL || '/auth').replace(/\/$/, '')
const API_URL = (import.meta.env.VITE_API_URL || '/api').replace(/\/$/, '')

// OIDC endpoints — populated from discovery, with direct fallbacks
let endpoints = {
  register: `${OIDC_ISSUER_URL}/register`,
  login: `${OIDC_ISSUER_URL}/login`,
  userinfo: `${OIDC_ISSUER_URL}/userinfo`,
  revocation: `${OIDC_ISSUER_URL}/revoke`,
}

// Fetch OIDC Discovery and override endpoints if available
fetch(`${OIDC_ISSUER_URL}/.well-known/openid-configuration`)
  .then(r => r.json())
  .then(config => {
    if (config.userinfo_endpoint) endpoints.userinfo = config.userinfo_endpoint
    if (config.revocation_endpoint) endpoints.revocation = config.revocation_endpoint
  })
  .catch(() => {}) // discovery optional — fallbacks work

export { endpoints }

export const auth = reactive({
  token: localStorage.getItem('token'),
  user: null,

  setToken(token) {
    this.token = token
    localStorage.setItem('token', token)
  },

  logout() {
    this.token = null
    this.user = null
    localStorage.removeItem('token')
    router.push('/login')
  },
})

/** Call IAM endpoints (register, login). */
export async function iam(url, options = {}) {
  const headers = { ...options.headers }
  if (auth.token) headers.Authorization = `Bearer ${auth.token}`
  if (options.body && typeof options.body === 'object') {
    headers['Content-Type'] = 'application/json'
    options.body = JSON.stringify(options.body)
  }
  const res = await fetch(url, { ...options, headers })
  if (!res.ok) {
    const data = await res.json().catch(() => ({}))
    throw new Error(data.error_description || data.detail || data.error || `Auth request failed (${res.status})`)
  }
  return res.json()
}

/** Call business API endpoints (notes, dashboard). */
export async function api(path, options = {}) {
  const headers = { ...options.headers }
  if (auth.token) headers.Authorization = `Bearer ${auth.token}`
  if (options.body && typeof options.body === 'object') {
    headers['Content-Type'] = 'application/json'
    options.body = JSON.stringify(options.body)
  }
  const res = await fetch(`${API_URL}${path}`, { ...options, headers })
  if (res.status === 401) {
    auth.logout()
    throw new Error('Unauthorized')
  }
  if (!res.ok) {
    const data = await res.json().catch(() => ({}))
    throw new Error(data.detail || `Request failed (${res.status})`)
  }
  return res.json()
}
