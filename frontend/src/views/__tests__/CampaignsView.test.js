import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import CampaignsView from '../CampaignsView.vue'

vi.mock('../../api/campaigns.js', () => ({
  listCampaigns: vi.fn(),
  getCampaign: vi.fn(),
  getCampaignStats: vi.fn(),
  createCampaign: vi.fn(),
  updateCampaign: vi.fn(),
  deleteCampaign: vi.fn(),
  sendCampaign: vi.fn(),
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

import {
  listCampaigns,
  createCampaign,
  deleteCampaign,
  sendCampaign,
  getCampaignStats,
} from '../../api/campaigns.js'
import { listSegments } from '../../api/segments.js'

const SEGMENTS = [
  { id: 'seg-1', name: 'Newsletter', contact_count: 100, created_at: '2024-01-01T00:00:00Z' },
]

const CAMPAIGNS = [
  {
    id: 'camp-1',
    subject: 'Welcome Email',
    from_name: 'Acme Corp',
    from_email: 'hello@acme.com',
    status: 'draft',
    segment_ids: ['seg-1'],
    total: null,
    opened: null,
    sent_at: null,
    html_body: '<p>Hello!</p>',
    created_at: '2024-01-01T00:00:00Z',
  },
  {
    id: 'camp-2',
    subject: 'Monthly Update',
    from_name: 'Acme Corp',
    from_email: 'hello@acme.com',
    status: 'sending',
    segment_ids: ['seg-1'],
    total: 100,
    opened: 0,
    sent_at: null,
    html_body: '<p>Update!</p>',
    created_at: '2024-02-01T00:00:00Z',
  },
  {
    id: 'camp-3',
    subject: 'Product Launch',
    from_name: 'Acme Corp',
    from_email: 'hello@acme.com',
    status: 'sent',
    segment_ids: ['seg-1'],
    total: 100,
    opened: 42,
    sent_at: '2024-03-01T00:00:00Z',
    html_body: '<p>Launch!</p>',
    created_at: '2024-03-01T00:00:00Z',
  },
  {
    id: 'camp-4',
    subject: 'Failed Campaign',
    from_name: 'Acme Corp',
    from_email: 'hello@acme.com',
    status: 'failed',
    segment_ids: [],
    total: 0,
    opened: 0,
    sent_at: null,
    html_body: '<p>Oops!</p>',
    created_at: '2024-04-01T00:00:00Z',
  },
]

const attachTo = document.createElement('div')
document.body.appendChild(attachTo)

function mountCampaigns() {
  return mount(CampaignsView, {
    attachTo,
    global: {
      plugins: [createTestingPinia({ createSpy: vi.fn })],
    },
  })
}

describe('CampaignsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listCampaigns.mockResolvedValue(CAMPAIGNS)
    listSegments.mockResolvedValue(SEGMENTS)
    getCampaignStats.mockResolvedValue({ total: 100, sent: 100, opened: 42, failed: 0 })
    mockConfirm.mockResolvedValue(true)
  })

  it('renders campaign list from API', async () => {
    const wrapper = mountCampaigns()
    await flushPromises()

    expect(wrapper.text()).toContain('Welcome Email')
    expect(wrapper.text()).toContain('Monthly Update')
    expect(wrapper.text()).toContain('Product Launch')
    expect(wrapper.text()).toContain('Failed Campaign')
    wrapper.unmount()
  })

  it('shows correct status badges', async () => {
    const wrapper = mountCampaigns()
    await flushPromises()

    expect(wrapper.text()).toContain('Draft')
    expect(wrapper.text()).toContain('Sending')
    expect(wrapper.text()).toContain('Sent')
    expect(wrapper.text()).toContain('Failed')

    // Draft badge uses slate classes
    const draftBadge = wrapper
      .findAll('span')
      .find((s) => s.text() === 'Draft' && s.classes().some((c) => c.includes('slate')))
    expect(draftBadge).toBeTruthy()

    // Sending badge uses amber classes
    const sendingBadge = wrapper
      .findAll('span')
      .find((s) => s.text() === 'Sending' && s.classes().some((c) => c.includes('amber')))
    expect(sendingBadge).toBeTruthy()

    // Sent badge uses green classes
    const sentBadge = wrapper
      .findAll('span')
      .find((s) => s.text() === 'Sent' && s.classes().some((c) => c.includes('green')))
    expect(sentBadge).toBeTruthy()

    // Failed badge uses red classes
    const failedBadge = wrapper
      .findAll('span')
      .find((s) => s.text() === 'Failed' && s.classes().some((c) => c.includes('red')))
    expect(failedBadge).toBeTruthy()

    wrapper.unmount()
  })

  it('"New Campaign" button opens modal', async () => {
    const wrapper = mountCampaigns()
    await flushPromises()

    const newBtn = wrapper.findAll('button').find((b) => b.text().includes('New Campaign'))
    await newBtn.trigger('click')

    expect(document.body.textContent).toContain('New Campaign')
    wrapper.unmount()
  })

  it('creating campaign calls API with segment_ids', async () => {
    const created = {
      id: 'camp-new',
      subject: 'Brand New',
      from_name: 'Acme',
      from_email: 'hi@acme.com',
      status: 'draft',
      segment_ids: ['seg-1'],
      total: null,
      opened: null,
      sent_at: null,
      html_body: '<p>New!</p>',
      created_at: '2024-05-01T00:00:00Z',
    }
    createCampaign.mockResolvedValue(created)

    const wrapper = mountCampaigns()
    await flushPromises()

    const newBtn = wrapper.findAll('button').find((b) => b.text().includes('New Campaign'))
    await newBtn.trigger('click')

    const form = document.querySelector('form')
    form.dispatchEvent(new Event('submit'))
    await flushPromises()

    expect(createCampaign).toHaveBeenCalledWith(
      expect.objectContaining({ segment_ids: expect.any(Array) }),
    )
    expect(wrapper.text()).toContain('Brand New')
    wrapper.unmount()
  })

  it('delete draft campaign with confirm dialog', async () => {
    deleteCampaign.mockResolvedValue()

    const wrapper = mountCampaigns()
    await flushPromises()

    const deleteButtons = wrapper.findAll('button').filter((b) => b.text().trim() === 'Delete')
    await deleteButtons[0].trigger('click')
    await flushPromises()

    expect(mockConfirm).toHaveBeenCalledWith(
      expect.objectContaining({ title: expect.stringContaining('Welcome Email') }),
    )
    expect(deleteCampaign).toHaveBeenCalledWith('camp-1')
    expect(wrapper.text()).not.toContain('Welcome Email')
    wrapper.unmount()
  })

  it('cannot delete sent campaign (delete button not rendered)', async () => {
    // Only draft campaigns render the Delete button
    // Use a list with only a sent campaign to verify
    listCampaigns.mockResolvedValue([CAMPAIGNS[2]]) // 'sent' status

    const wrapper = mountCampaigns()
    await flushPromises()

    const deleteButtons = wrapper.findAll('button').filter((b) => b.text().trim() === 'Delete')
    expect(deleteButtons.length).toBe(0)
    wrapper.unmount()
  })

  it('send campaign shows confirm dialog', async () => {
    sendCampaign.mockResolvedValue({})

    const wrapper = mountCampaigns()
    await flushPromises()

    const sendButtons = wrapper.findAll('button').filter((b) => b.text().trim() === 'Send')
    await sendButtons[0].trigger('click')
    await flushPromises()

    expect(mockConfirm).toHaveBeenCalledWith(
      expect.objectContaining({ title: expect.stringContaining('Welcome Email') }),
    )
    expect(sendCampaign).toHaveBeenCalledWith('camp-1')
    wrapper.unmount()
  })

  it('shows skeleton loading state', () => {
    listCampaigns.mockReturnValue(new Promise(() => {}))
    const wrapper = mountCampaigns()

    expect(wrapper.find('.animate-pulse').exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows empty state when no campaigns', async () => {
    listCampaigns.mockResolvedValue([])

    const wrapper = mountCampaigns()
    await flushPromises()

    expect(wrapper.text()).toContain('No campaigns yet')
    wrapper.unmount()
  })
})
