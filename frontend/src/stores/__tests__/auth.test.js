import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from '../auth.js'

vi.mock('../../api/auth.js', () => ({
  loginApi: vi.fn(),
}))

// Prevent the api/client.js module from trying to import router at module load
vi.mock('../../api/client.js', () => ({
  default: {},
}))

vi.mock('../../router/index.js', () => ({
  default: { push: vi.fn() },
}))

import { loginApi } from '../../api/auth.js'

function makeJwt(payload) {
  const encoded = btoa(JSON.stringify(payload))
  return `header.${encoded}.signature`
}

describe('auth store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    vi.clearAllMocks()
  })

  it('login stores token and decodes admin email from JWT', async () => {
    const token = makeJwt({ role: 'admin', sub: 'admin@example.com' })
    loginApi.mockResolvedValue({ access_token: token })

    const auth = useAuthStore()
    await auth.login('admin@example.com', 'secret')

    expect(auth.token).toBe(token)
    expect(auth.adminEmail).toBe('admin@example.com')
    expect(localStorage.getItem('access_token')).toBe(token)
    expect(localStorage.getItem('admin_email')).toBe('admin@example.com')
  })

  it('login rejects non-admin users', async () => {
    const token = makeJwt({ role: 'user', sub: 'user@example.com' })
    loginApi.mockResolvedValue({ access_token: token })

    const auth = useAuthStore()
    await expect(auth.login('user@example.com', 'secret')).rejects.toThrow(
      'Access denied: admin role required',
    )
    expect(auth.token).toBe('')
  })

  it('logout clears token and email', async () => {
    const token = makeJwt({ role: 'admin' })
    loginApi.mockResolvedValue({ access_token: token })

    const auth = useAuthStore()
    await auth.login('admin@example.com', 'secret')
    auth.logout()

    expect(auth.token).toBe('')
    expect(auth.adminEmail).toBe('')
    expect(localStorage.getItem('access_token')).toBeNull()
    expect(localStorage.getItem('admin_email')).toBeNull()
  })

  it('token persists to localStorage', async () => {
    const token = makeJwt({ role: 'admin' })
    loginApi.mockResolvedValue({ access_token: token })

    const auth = useAuthStore()
    await auth.login('admin@example.com', 'secret')

    expect(localStorage.getItem('access_token')).toBe(token)
  })

  it('isAuthenticated reflects token presence', async () => {
    const token = makeJwt({ role: 'admin' })
    loginApi.mockResolvedValue({ access_token: token })

    const auth = useAuthStore()
    expect(auth.token).toBe('')

    await auth.login('admin@example.com', 'secret')
    expect(auth.token).toBeTruthy()

    auth.logout()
    expect(auth.token).toBe('')
  })

  it('hydrates token from localStorage on store init', () => {
    localStorage.setItem('access_token', 'stored-token')
    localStorage.setItem('admin_email', 'admin@example.com')

    const auth = useAuthStore()
    expect(auth.token).toBe('stored-token')
    expect(auth.adminEmail).toBe('admin@example.com')
  })
})
