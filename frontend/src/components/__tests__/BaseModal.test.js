import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import BaseModal from '../BaseModal.vue'

// Teleport renders to body — we attach to a real element so Teleport works
const attachTo = document.createElement('div')
document.body.appendChild(attachTo)

describe('BaseModal', () => {
  it('renders content when show=true', () => {
    const wrapper = mount(BaseModal, {
      props: { show: true, title: 'My Modal' },
      slots: { default: '<p>Modal body</p>' },
      attachTo,
    })
    expect(document.body.textContent).toContain('My Modal')
    expect(document.body.textContent).toContain('Modal body')
    wrapper.unmount()
  })

  it('does not render content when show=false', () => {
    const wrapper = mount(BaseModal, {
      props: { show: false, title: 'Hidden' },
      slots: { default: '<p>Hidden body</p>' },
      attachTo,
    })
    expect(document.body.textContent).not.toContain('Hidden body')
    wrapper.unmount()
  })

  it('emits update:show=false when close button is clicked', async () => {
    const wrapper = mount(BaseModal, {
      props: { show: true, title: 'Closeable' },
      attachTo,
    })
    // The close button has aria-label="Close"
    const closeBtn = document.querySelector('[aria-label="Close"]')
    await closeBtn.click()
    expect(wrapper.emitted('update:show')).toBeTruthy()
    expect(wrapper.emitted('update:show')[0]).toEqual([false])
    wrapper.unmount()
  })

  it('emits update:show=false on Escape key', async () => {
    const wrapper = mount(BaseModal, {
      props: { show: true, title: 'Escape test' },
      attachTo,
    })
    const event = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
    window.dispatchEvent(event)
    expect(wrapper.emitted('update:show')).toBeTruthy()
    expect(wrapper.emitted('update:show')[0]).toEqual([false])
    wrapper.unmount()
  })

  it('does not emit on Escape key when show=false', async () => {
    const wrapper = mount(BaseModal, {
      props: { show: false, title: 'Closed modal' },
      attachTo,
    })
    const event = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
    window.dispatchEvent(event)
    expect(wrapper.emitted('update:show')).toBeFalsy()
    wrapper.unmount()
  })
})
