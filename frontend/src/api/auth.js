import axios from 'axios'

const base = import.meta.env.VITE_API_URL || '/auth'

function getTenantSlug() {
  const parts = window.location.hostname.split('.')
  if (parts.length >= 3) {
    return parts[0]
  }
  return ''
}

export async function loginApi(email, password) {
  const headers = {}
  const tenant = getTenantSlug()
  if (tenant) {
    headers['X-Tenant'] = tenant
  }
  const { data } = await axios.post(`${base}/login`, { email, password }, { headers })
  return data
}
