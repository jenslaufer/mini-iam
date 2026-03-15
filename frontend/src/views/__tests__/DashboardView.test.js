import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import DashboardView from '../DashboardView.vue'

vi.mock('../../api/users.js', () => ({
  getUsers: vi.fn(),
}))

vi.mock('../../api/clients.js', () => ({
  getClients: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  useRoute: () => ({}),
  RouterLink: { template: '<a><slot /></a>' },
}))

import { getUsers } from '../../api/users.js'
import { getClients } from '../../api/clients.js'

const USERS = [
  { id: '1', email: 'admin@example.com', role: 'admin', created_at: '2024-01-01' },
  { id: '2', email: 'user@example.com', role: 'user', created_at: '2024-01-02' },
  { id: '3', email: 'another@example.com', role: 'user', created_at: '2024-01-03' },
]

const CLIENTS = [
  { client_id: 'c1', name: 'App One', redirect_uris: [], created_at: '2024-01-01' },
  { client_id: 'c2', name: 'App Two', redirect_uris: [], created_at: '2024-01-02' },
]

function mountDashboard() {
  return mount(DashboardView, {
    global: {
      plugins: [createTestingPinia({ createSpy: vi.fn })],
    },
  })
}

describe('DashboardView', () => {
  beforeEach(() => {
    getUsers.mockResolvedValue(USERS)
    getClients.mockResolvedValue(CLIENTS)
  })

  it('shows correct total user count', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.text()).toContain('3')
  })

  it('shows correct admin count', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    // 1 admin in the user list
    expect(wrapper.text()).toContain('1')
  })

  it('shows correct client count', async () => {
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.text()).toContain('2')
  })

  it('shows loading placeholder before data arrives', () => {
    // Don't flush promises — check immediate render
    getUsers.mockReturnValue(new Promise(() => {}))
    getClients.mockReturnValue(new Promise(() => {}))

    const wrapper = mountDashboard()
    // The loading state shows '—' for each stat card value
    const dashes = wrapper.text().match(/—/g)
    expect(dashes).not.toBeNull()
    expect(dashes.length).toBeGreaterThanOrEqual(3)
  })
})
