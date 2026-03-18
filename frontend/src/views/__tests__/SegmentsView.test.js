import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import SegmentsView from '../SegmentsView.vue'

vi.mock('../../api/segments.js', () => ({
  listSegments: vi.fn(),
  getSegment: vi.fn(),
  createSegment: vi.fn(),
  updateSegment: vi.fn(),
  deleteSegment: vi.fn(),
  addContactToSegment: vi.fn(),
  removeContactFromSegment: vi.fn(),
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

import {
  listSegments,
  getSegment,
  createSegment,
  updateSegment,
  deleteSegment,
} from '../../api/segments.js'

const SEGMENTS = [
  {
    id: 'seg-1',
    name: 'Newsletter',
    description: 'Monthly newsletter subscribers',
    contact_count: 42,
    created_at: '2024-01-01T00:00:00Z',
  },
  {
    id: 'seg-2',
    name: 'VIP',
    description: 'High-value customers',
    contact_count: 7,
    created_at: '2024-02-01T00:00:00Z',
  },
]

const attachTo = document.createElement('div')
document.body.appendChild(attachTo)

function mountSegments() {
  return mount(SegmentsView, {
    attachTo,
    global: {
      plugins: [createTestingPinia({ createSpy: vi.fn })],
    },
  })
}

describe('SegmentsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listSegments.mockResolvedValue(SEGMENTS)
    getSegment.mockResolvedValue({ ...SEGMENTS[0], contacts: [] })
    mockConfirm.mockResolvedValue(true)
  })

  it('renders segment list from API', async () => {
    const wrapper = mountSegments()
    await flushPromises()

    expect(wrapper.text()).toContain('Newsletter')
    expect(wrapper.text()).toContain('VIP')
    wrapper.unmount()
  })

  it('"New Segment" button opens modal', async () => {
    const wrapper = mountSegments()
    await flushPromises()

    const newBtn = wrapper.findAll('button').find((b) => b.text().includes('New Segment'))
    await newBtn.trigger('click')

    expect(document.body.textContent).toContain('New Segment')
    wrapper.unmount()
  })

  it('creating segment calls API and refreshes list', async () => {
    const created = {
      id: 'seg-3',
      name: 'Early Adopters',
      description: '',
      contact_count: 0,
      created_at: '2024-03-01T00:00:00Z',
    }
    createSegment.mockResolvedValue(created)

    const wrapper = mountSegments()
    await flushPromises()

    const newBtn = wrapper.findAll('button').find((b) => b.text().includes('New Segment'))
    await newBtn.trigger('click')

    const form = document.querySelector('form')
    form.dispatchEvent(new Event('submit'))
    await flushPromises()

    expect(createSegment).toHaveBeenCalled()
    expect(wrapper.text()).toContain('Early Adopters')
    wrapper.unmount()
  })

  it('edit segment updates via API', async () => {
    const updated = { ...SEGMENTS[0], name: 'Newsletter Updated' }
    updateSegment.mockResolvedValue(updated)

    const wrapper = mountSegments()
    await flushPromises()

    const editButtons = wrapper.findAll('button').filter((b) => b.text().trim() === 'Edit')
    await editButtons[0].trigger('click')

    const form = document.querySelector('form')
    form.dispatchEvent(new Event('submit'))
    await flushPromises()

    expect(updateSegment).toHaveBeenCalledWith('seg-1', expect.any(Object))
    wrapper.unmount()
  })

  it('delete segment with confirm dialog', async () => {
    deleteSegment.mockResolvedValue()

    const wrapper = mountSegments()
    await flushPromises()

    const deleteButtons = wrapper.findAll('button').filter((b) => b.text().trim() === 'Delete')
    await deleteButtons[0].trigger('click')
    await flushPromises()

    expect(mockConfirm).toHaveBeenCalledWith(
      expect.objectContaining({ title: expect.stringContaining('Newsletter') }),
    )
    expect(wrapper.text()).not.toContain('Newsletter')
    expect(wrapper.text()).toContain('VIP')
    wrapper.unmount()
  })

  it('shows contact count for each segment', async () => {
    const wrapper = mountSegments()
    await flushPromises()

    expect(wrapper.text()).toContain('42')
    expect(wrapper.text()).toContain('7')
    wrapper.unmount()
  })

  it('shows skeleton loading state', () => {
    listSegments.mockReturnValue(new Promise(() => {}))
    const wrapper = mountSegments()

    expect(wrapper.find('.animate-pulse').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows empty state when no segments', async () => {
    listSegments.mockResolvedValue([])

    const wrapper = mountSegments()
    await flushPromises()

    expect(wrapper.text()).toContain('No segments yet')
    wrapper.unmount()
  })
})
