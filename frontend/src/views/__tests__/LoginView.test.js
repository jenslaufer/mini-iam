import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import LoginView from '../LoginView.vue'

const mockPush = vi.fn()

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: mockPush }),
  useRoute: () => ({}),
  RouterLink: { template: '<a><slot /></a>' },
  createRouter: () => ({ beforeEach: vi.fn() }),
  createWebHistory: vi.fn(),
}))

function mountLogin(loginImpl) {
  return mount(LoginView, {
    global: {
      plugins: [
        createTestingPinia({
          createSpy: vi.fn,
          initialState: { auth: { token: '', adminEmail: '' } },
        }),
      ],
    },
  })
}

describe('LoginView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders email and password inputs', () => {
    const wrapper = mountLogin()
    const inputs = wrapper.findAll('input')
    const types = inputs.map((i) => i.attributes('type'))
    expect(types).toContain('email')
    expect(types).toContain('password')
  })

  it('calls auth.login on form submit', async () => {
    const wrapper = mountLogin()
    const { useAuthStore } = await import('../../stores/auth.js')
    const auth = useAuthStore()
    auth.login.mockResolvedValue()

    await wrapper.find('input[type="email"]').setValue('admin@example.com')
    await wrapper.find('input[type="password"]').setValue('secret')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(auth.login).toHaveBeenCalledWith('admin@example.com', 'secret', '')
  })

  it('redirects to /dashboard on successful login', async () => {
    const wrapper = mountLogin()
    const { useAuthStore } = await import('../../stores/auth.js')
    const auth = useAuthStore()
    auth.login.mockResolvedValue()

    await wrapper.find('input[type="email"]').setValue('admin@example.com')
    await wrapper.find('input[type="password"]').setValue('secret')
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(mockPush).toHaveBeenCalledWith('/dashboard')
  })

  it('shows error message on login failure', async () => {
    const wrapper = mountLogin()
    const { useAuthStore } = await import('../../stores/auth.js')
    const auth = useAuthStore()
    auth.login.mockRejectedValue(new Error('Access denied: admin role required'))

    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.text()).toContain('Access denied: admin role required')
  })

  it('shows API error_description when present', async () => {
    const wrapper = mountLogin()
    const { useAuthStore } = await import('../../stores/auth.js')
    const auth = useAuthStore()
    const err = { response: { data: { error_description: 'Invalid credentials' } } }
    auth.login.mockRejectedValue(err)

    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(wrapper.text()).toContain('Invalid credentials')
  })

  it('disables inputs during loading', async () => {
    const wrapper = mountLogin()
    const { useAuthStore } = await import('../../stores/auth.js')
    const auth = useAuthStore()

    // Never resolves — stays in loading state
    auth.login.mockReturnValue(new Promise(() => {}))

    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    const inputs = wrapper.findAll('input')
    inputs.forEach((input) => {
      expect(input.attributes('disabled')).toBeDefined()
    })
  })
})
