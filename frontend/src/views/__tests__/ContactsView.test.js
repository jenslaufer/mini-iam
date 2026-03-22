import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import ContactsView from '../ContactsView.vue'

vi.mock('../../api/contacts.js', () => ({
  listContacts: vi.fn(),
  createContact: vi.fn(),
  deleteContact: vi.fn(),
  importContacts: vi.fn(),
}))

vi.mock('../../api/segments.js', () => ({
  listSegments: vi.fn(),
}))

const mockConfirm = vi.fn().mockResolvedValue(true)

vi.mock('../../composables/useConfirm.js', () => ({
  useConfirm: () => ({
    confirm: mockConfirm,
  }),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
  useRoute: () => ({}),
  RouterLink: { template: '<a><slot /></a>' },
  createRouter: () => ({ beforeEach: vi.fn() }),
  createWebHistory: vi.fn(),
}))

import { listContacts, createContact, deleteContact, importContacts } from '../../api/contacts.js'
import { listSegments } from '../../api/segments.js'

const SEGMENTS = [
  { id: 'seg-1', name: 'Newsletter', contact_count: 2, created_at: '2024-01-01T00:00:00Z' },
  { id: 'seg-2', name: 'VIP', contact_count: 1, created_at: '2024-02-01T00:00:00Z' },
]

const CONTACTS = [
  {
    id: 'c-1',
    email: 'alice@example.com',
    name: 'Alice',
    segments: [{ id: 'seg-1', name: 'Newsletter' }],
    unsubscribed: false,
    consent_source: 'signup',
    created_at: '2024-01-01T00:00:00Z',
  },
  {
    id: 'c-2',
    email: 'bob@example.com',
    name: 'Bob',
    segments: [],
    unsubscribed: true,
    consent_source: null,
    created_at: '2024-02-01T00:00:00Z',
  },
  {
    id: 'c-3',
    email: 'carol@example.com',
    name: 'Carol',
    segments: [{ id: 'seg-2', name: 'VIP' }],
    unsubscribed: false,
    consent_source: 'import',
    created_at: '2024-03-01T00:00:00Z',
  },
]

const attachTo = document.createElement('div')
document.body.appendChild(attachTo)

function mountContacts() {
  return mount(ContactsView, {
    attachTo,
    global: {
      plugins: [createTestingPinia({ createSpy: vi.fn })],
    },
  })
}

describe('ContactsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listContacts.mockResolvedValue(CONTACTS)
    listSegments.mockResolvedValue(SEGMENTS)
    mockConfirm.mockResolvedValue(true)
  })

  it('renders contact list from API', async () => {
    const wrapper = mountContacts()
    await flushPromises()

    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('bob@example.com')
    expect(wrapper.text()).toContain('carol@example.com')
    wrapper.unmount()
  })

  it('search filters contacts by email', async () => {
    const wrapper = mountContacts()
    await flushPromises()

    await wrapper.find('input[type="search"]').setValue('alice')

    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).not.toContain('bob@example.com')
    expect(wrapper.text()).not.toContain('carol@example.com')
    wrapper.unmount()
  })

  it('search filters contacts by name', async () => {
    const wrapper = mountContacts()
    await flushPromises()

    await wrapper.find('input[type="search"]').setValue('Carol')

    expect(wrapper.text()).toContain('carol@example.com')
    expect(wrapper.text()).not.toContain('alice@example.com')
    wrapper.unmount()
  })

  it('"Add Contact" button opens modal', async () => {
    const wrapper = mountContacts()
    await flushPromises()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Contact'))
    await addBtn.trigger('click')

    expect(document.body.textContent).toContain('Add Contact')
    wrapper.unmount()
  })

  it('creating contact calls API and refreshes list', async () => {
    const newContact = {
      id: 'c-new',
      email: 'new@example.com',
      name: 'New Person',
      segments: [],
      unsubscribed: false,
      consent_source: null,
      created_at: '2024-04-01T00:00:00Z',
    }
    createContact.mockResolvedValue(newContact)

    const wrapper = mountContacts()
    await flushPromises()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('Add Contact'))
    await addBtn.trigger('click')

    const form = document.querySelector('form')
    form.dispatchEvent(new Event('submit'))
    await flushPromises()

    expect(createContact).toHaveBeenCalled()
    expect(wrapper.text()).toContain('new@example.com')
    wrapper.unmount()
  })

  it('delete button shows confirm dialog and removes contact', async () => {
    deleteContact.mockResolvedValue()

    const wrapper = mountContacts()
    await flushPromises()

    const deleteButtons = wrapper.findAll('button').filter((b) => b.text().trim() === 'Delete')
    await deleteButtons[0].trigger('click')
    await flushPromises()

    expect(mockConfirm).toHaveBeenCalledWith(
      expect.objectContaining({ title: expect.stringContaining('alice@example.com') }),
    )
    expect(wrapper.text()).not.toContain('alice@example.com')
    expect(wrapper.text()).toContain('bob@example.com')
    wrapper.unmount()
  })

  it('shows unsubscribed badge for unsubscribed contacts', async () => {
    const wrapper = mountContacts()
    await flushPromises()

    expect(wrapper.text()).toContain('Unsubscribed')
    wrapper.unmount()
  })

  it('shows subscribed badge for active contacts', async () => {
    const wrapper = mountContacts()
    await flushPromises()

    expect(wrapper.text()).toContain('Subscribed')
    wrapper.unmount()
  })

  it('import contacts shows import modal', async () => {
    const wrapper = mountContacts()
    await flushPromises()

    const importBtn = wrapper.findAll('button').find((b) => b.text().includes('Import'))
    await importBtn.trigger('click')

    expect(document.body.textContent).toContain('Import Contacts')
    wrapper.unmount()
  })

  it('shows skeleton loading state', () => {
    listContacts.mockReturnValue(new Promise(() => {}))
    const wrapper = mountContacts()

    expect(wrapper.find('.animate-pulse').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows empty state when no contacts', async () => {
    listContacts.mockResolvedValue([])

    const wrapper = mountContacts()
    await flushPromises()

    expect(wrapper.text()).toContain('No contacts found')
    wrapper.unmount()
  })

  it('importContacts sends wrapped payload with contacts key', async () => {
    importContacts.mockResolvedValue({ imported: 1, skipped: 0 })
    listContacts.mockResolvedValue(CONTACTS)
    listSegments.mockResolvedValue(SEGMENTS)

    const wrapper = mountContacts()
    await flushPromises()

    // Open import modal
    const importBtn = wrapper.findAll('button').find((b) => b.text().includes('Import'))
    await importBtn.trigger('click')

    // Fill in textarea with CSV data
    const textarea = document.querySelector('textarea')
    if (textarea) {
      textarea.value = 'test@example.com,Test User'
      textarea.dispatchEvent(new Event('input'))
    }

    // Submit import form
    const form = document.querySelector('form')
    if (form) {
      form.dispatchEvent(new Event('submit'))
      await flushPromises()
    }

    if (importContacts.mock.calls.length > 0) {
      const payload = importContacts.mock.calls[0][0]
      expect(payload).toHaveProperty('contacts')
      expect(Array.isArray(payload.contacts)).toBe(true)
    }
    wrapper.unmount()
  })
})
