import axios from 'axios'

const base = import.meta.env.VITE_API_URL || '/api'

export async function loginApi(email, password) {
  const { data } = await axios.post(`${base}/login`, { email, password })
  return data
}
