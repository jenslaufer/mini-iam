import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import ClientsView from '../ClientsView.vue'

vi.mock('../../api/clients.js', () => ({
  getClients: vi.fn(),
  createClient: vi.fn(),
  deleteClient: vi.fn(),
}))

const mockConfirm = vi.fn().mockResolvedValue(true)

vi.mock('../../composables/useConfirm.js', () => ({
  useConfirm: () => ({
    confirm: mockConfirm,
  }),
}))

import { getClients, createClient, deleteClient } from '../../api/clients.js'

const CLIENTS = [
  {
    client_id: 'client-abc-123',
    name: 'My App',
    redirect_uris: ['https://myapp.com/callback'],
    created_at: '2024-01-01T00:00:00Z',
  },
  {
    client_id: 'client-def-456',
    name: 'Other App',
    redirect_uris: ['https://other.com/callback'],
    created_at: '2024-02-01T00:00:00Z',
  },
]

const CREATED_CLIENT = {
  client_id: 'new-client-id',
  name: 'New App',
  client_secret: 'super-secret-value',
  redirect_uris: ['https://newapp.com/callback'],
  created_at: '2024-03-01T00:00:00Z',
}

const attachTo = document.createElement('div')
document.body.appendChild(attachTo)

function mountClients() {
  return mount(ClientsView, {
    attachTo,
    global: {
      plugins: [createTestingPinia({ createSpy: vi.fn })],
    },
  })
}

describe('ClientsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getClients.mockResolvedValue(CLIENTS)
    mockConfirm.mockResolvedValue(true)
    // Reset clipboard mock
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
      writable: true,
      configurable: true,
    })
  })

  it('renders client list from API', async () => {
    const wrapper = mountClients()
    await flushPromises()

    expect(wrapper.text()).toContain('My App')
    expect(wrapper.text()).toContain('Other App')
    wrapper.unmount()
  })

  it('"New Client" button opens modal', async () => {
    const wrapper = mountClients()
    await flushPromises()

    const newClientBtn = wrapper.findAll('button').find((b) => b.text().includes('New Client'))
    await newClientBtn.trigger('click')

    // Modal title should appear in body
    expect(document.body.textContent).toContain('New OAuth2 Client')
    wrapper.unmount()
  })

  it('creating client shows secret alert', async () => {
    createClient.mockResolvedValue(CREATED_CLIENT)

    const wrapper = mountClients()
    await flushPromises()

    // Open modal
    const newClientBtn = wrapper.findAll('button').find((b) => b.text().includes('New Client'))
    await newClientBtn.trigger('click')

    // Fill form — find inputs in modal
    const modalInputs = document.querySelectorAll('input')
    const nameInput = Array.from(modalInputs).find(
      (el) => el.placeholder === 'My App' || el.getAttribute('placeholder') === 'My App',
    )
    if (nameInput) {
      nameInput.value = 'New App'
      nameInput.dispatchEvent(new Event('input'))
    }

    const textarea = document.querySelector('textarea')
    if (textarea) {
      textarea.value = 'https://newapp.com/callback'
      textarea.dispatchEvent(new Event('input'))
    }

    // Submit form
    const form = document.querySelector('form')
    form.dispatchEvent(new Event('submit'))
    await flushPromises()

    // Secret alert should now be visible
    expect(wrapper.text()).toContain('super-secret-value')
    wrapper.unmount()
  })

  it('copy button copies secret to clipboard', async () => {
    createClient.mockResolvedValue(CREATED_CLIENT)

    const wrapper = mountClients()
    await flushPromises()

    // Trigger create flow to show secret
    const newClientBtn = wrapper.findAll('button').find((b) => b.text().includes('New Client'))
    await newClientBtn.trigger('click')

    const form = document.querySelector('form')
    form.dispatchEvent(new Event('submit'))
    await flushPromises()

    // Find copy button by its title attribute
    const copyBtn = wrapper.find('button[title="Copy to clipboard"]')
    await copyBtn.trigger('click')
    await flushPromises()

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('super-secret-value')
    wrapper.unmount()
  })

  it('delete button shows confirm dialog', async () => {
    deleteClient.mockResolvedValue()

    const wrapper = mountClients()
    await flushPromises()

    const deleteButtons = wrapper.findAll('button').filter((b) => b.text().trim() === 'Delete')
    await deleteButtons[0].trigger('click')
    await flushPromises()

    expect(mockConfirm).toHaveBeenCalledWith(
      expect.objectContaining({ title: expect.stringContaining('My App') }),
    )
    wrapper.unmount()
  })

  it('removes client from list after confirmed delete', async () => {
    deleteClient.mockResolvedValue()

    const wrapper = mountClients()
    await flushPromises()

    const deleteButtons = wrapper.findAll('button').filter((b) => b.text().trim() === 'Delete')
    await deleteButtons[0].trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('My App')
    expect(wrapper.text()).toContain('Other App')
    wrapper.unmount()
  })

  it('shows skeleton loading state before data arrives', () => {
    getClients.mockReturnValue(new Promise(() => {}))
    const wrapper = mountClients()

    expect(wrapper.find('.animate-pulse').exists()).toBe(true)
    wrapper.unmount()
  })
})
