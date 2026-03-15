import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createTestingPinia } from '@pinia/testing'
import ToastItem from '../ToastItem.vue'

function mountToast(toastProps) {
  return mount(ToastItem, {
    props: { toast: toastProps },
    global: { plugins: [createTestingPinia()] },
  })
}

describe('ToastItem', () => {
  it('renders the message', () => {
    const wrapper = mountToast({ id: 1, type: 'success', message: 'All good!' })
    expect(wrapper.text()).toContain('All good!')
  })

  it('applies green classes for success variant', () => {
    const wrapper = mountToast({ id: 1, type: 'success', message: 'Done' })
    const classes = wrapper.find('div').classes()
    expect(classes).toContain('bg-green-50')
    expect(classes).toContain('text-green-800')
  })

  it('applies red classes for error variant', () => {
    const wrapper = mountToast({ id: 2, type: 'error', message: 'Oops' })
    const classes = wrapper.find('div').classes()
    expect(classes).toContain('bg-red-50')
    expect(classes).toContain('text-red-800')
  })

  it('close button calls store.remove with toast id', async () => {
    const wrapper = mountToast({ id: 42, type: 'success', message: 'Bye' })
    const { useToastStore } = await import('../../stores/toast.js')
    const store = useToastStore()

    const closeBtn = wrapper.find('button[aria-label="Dismiss"]')
    await closeBtn.trigger('click')

    expect(store.remove).toHaveBeenCalledWith(42)
  })
})
