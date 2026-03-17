import axios from 'axios'

const base = import.meta.env.VITE_API_URL || '/auth'

export async function loginApi(email, password, tenantSlug = '') {
  const path = tenantSlug ? `${base}/t/${tenantSlug}/login` : `${base}/login`
  const { data } = await axios.post(path, { email, password })
  return data
}
