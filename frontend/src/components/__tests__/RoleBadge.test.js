import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import RoleBadge from '../RoleBadge.vue'

describe('RoleBadge', () => {
  it('renders the role text', () => {
    const wrapper = mount(RoleBadge, { props: { role: 'admin' } })
    expect(wrapper.text()).toBe('admin')
  })

  it('applies blue classes for admin role', () => {
    const wrapper = mount(RoleBadge, { props: { role: 'admin' } })
    const classes = wrapper.find('span').classes()
    expect(classes).toContain('bg-blue-100')
    expect(classes).toContain('text-blue-700')
  })

  it('renders user role text', () => {
    const wrapper = mount(RoleBadge, { props: { role: 'user' } })
    expect(wrapper.text()).toBe('user')
  })

  it('applies slate classes for user role', () => {
    const wrapper = mount(RoleBadge, { props: { role: 'user' } })
    const classes = wrapper.find('span').classes()
    expect(classes).toContain('bg-slate-100')
    expect(classes).toContain('text-slate-600')
  })
})
