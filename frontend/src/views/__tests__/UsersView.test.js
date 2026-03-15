import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import UsersView from '../UsersView.vue'

vi.mock('../../api/users.js', () => ({
  getUsers: vi.fn(),
  updateUser: vi.fn(),
  deleteUser: vi.fn(),
}))

const mockConfirm = vi.fn().mockResolvedValue(true)

vi.mock('../../composables/useConfirm.js', () => ({
  useConfirm: () => ({
    confirm: mockConfirm,
  }),
}))

import { getUsers, updateUser, deleteUser } from '../../api/users.js'

const USERS = [
  {
    id: '1',
    email: 'alice@example.com',
    name: 'Alice',
    role: 'admin',
    created_at: '2024-01-01T00:00:00Z',
  },
  {
    id: '2',
    email: 'bob@example.com',
    name: 'Bob',
    role: 'user',
    created_at: '2024-02-01T00:00:00Z',
  },
  {
    id: '3',
    email: 'carol@example.com',
    name: 'Carol',
    role: 'user',
    created_at: '2024-03-01T00:00:00Z',
  },
]

function mountUsers() {
  return mount(UsersView, {
    global: {
      plugins: [createTestingPinia({ createSpy: vi.fn })],
    },
  })
}

describe('UsersView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getUsers.mockResolvedValue(USERS)
    mockConfirm.mockResolvedValue(true)
  })

  it('renders user list from API', async () => {
    const wrapper = mountUsers()
    await flushPromises()

    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('bob@example.com')
    expect(wrapper.text()).toContain('carol@example.com')
  })

  it('search filters users by email', async () => {
    const wrapper = mountUsers()
    await flushPromises()

    const searchInput = wrapper.find('input[type="search"]')
    await searchInput.setValue('alice')

    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).not.toContain('bob@example.com')
  })

  it('search filters users by name', async () => {
    const wrapper = mountUsers()
    await flushPromises()

    const searchInput = wrapper.find('input[type="search"]')
    await searchInput.setValue('Carol')

    expect(wrapper.text()).toContain('carol@example.com')
    expect(wrapper.text()).not.toContain('alice@example.com')
  })

  it('search is case-insensitive', async () => {
    const wrapper = mountUsers()
    await flushPromises()

    await wrapper.find('input[type="search"]').setValue('BOB')
    expect(wrapper.text()).toContain('bob@example.com')
  })

  it('role dropdown changes user role via API', async () => {
    updateUser.mockResolvedValue({ ...USERS[1], role: 'admin' })

    const wrapper = mountUsers()
    await flushPromises()

    // Click the Role button for the second user (Bob)
    const roleButtons = wrapper.findAll('button').filter((b) => b.text().trim() === 'Role')
    await roleButtons[1].trigger('click')

    // Click 'admin' option in dropdown
    const adminOption = wrapper
      .findAll('button')
      .find((b) => b.text().trim() === 'admin' && b.classes().includes('text-slate-700'))
    await adminOption.trigger('click')
    await flushPromises()

    expect(updateUser).toHaveBeenCalledWith('2', { role: 'admin' })
  })

  it('delete button shows confirm dialog', async () => {
    deleteUser.mockResolvedValue()

    const wrapper = mountUsers()
    await flushPromises()

    const deleteButtons = wrapper.findAll('button').filter((b) => b.text().trim() === 'Delete')
    await deleteButtons[0].trigger('click')
    await flushPromises()

    expect(mockConfirm).toHaveBeenCalledWith(
      expect.objectContaining({ title: expect.stringContaining('alice@example.com') }),
    )
  })

  it('deletes user from list after confirmation', async () => {
    deleteUser.mockResolvedValue()

    const wrapper = mountUsers()
    await flushPromises()

    const deleteButtons = wrapper.findAll('button').filter((b) => b.text().trim() === 'Delete')
    await deleteButtons[0].trigger('click')
    await flushPromises()

    expect(wrapper.text()).not.toContain('alice@example.com')
    expect(wrapper.text()).toContain('bob@example.com')
  })

  it('shows skeleton loading state before data arrives', () => {
    getUsers.mockReturnValue(new Promise(() => {}))
    const wrapper = mountUsers()

    // Skeleton rows have animate-pulse divs
    expect(wrapper.find('.animate-pulse').exists()).toBe(true)
  })
})
