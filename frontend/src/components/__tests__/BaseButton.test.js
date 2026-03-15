import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import BaseButton from '../BaseButton.vue'

describe('BaseButton', () => {
  it('renders slot content', () => {
    const wrapper = mount(BaseButton, { slots: { default: 'Click me' } })
    expect(wrapper.text()).toContain('Click me')
  })

  it('emits click event', async () => {
    const wrapper = mount(BaseButton)
    await wrapper.trigger('click')
    expect(wrapper.emitted('click')).toBeTruthy()
  })

  it('shows loading spinner when loading prop is true', () => {
    const wrapper = mount(BaseButton, { props: { loading: true } })
    // ArrowPathIcon renders as an svg; check animate-spin class is present
    expect(wrapper.find('.animate-spin').exists()).toBe(true)
  })

  it('does not show spinner when not loading', () => {
    const wrapper = mount(BaseButton, { props: { loading: false } })
    expect(wrapper.find('.animate-spin').exists()).toBe(false)
  })

  it('disabled state sets disabled attribute', () => {
    const wrapper = mount(BaseButton, { props: { disabled: true } })
    expect(wrapper.find('button').attributes('disabled')).toBeDefined()
  })

  it('loading state sets disabled attribute', () => {
    const wrapper = mount(BaseButton, { props: { loading: true } })
    expect(wrapper.find('button').attributes('disabled')).toBeDefined()
  })

  it('applies primary variant classes by default', () => {
    const wrapper = mount(BaseButton)
    const classes = wrapper.find('button').classes()
    expect(classes).toContain('bg-blue-600')
  })

  it('applies danger variant classes', () => {
    const wrapper = mount(BaseButton, { props: { variant: 'danger' } })
    const classes = wrapper.find('button').classes()
    expect(classes).toContain('bg-red-600')
  })

  it('applies ghost variant classes', () => {
    const wrapper = mount(BaseButton, { props: { variant: 'ghost' } })
    const classes = wrapper.find('button').classes()
    expect(classes).toContain('bg-transparent')
  })
})
