import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useToastStore } from '../toast.js'

describe('toast store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('add() creates toast with unique id', () => {
    const store = useToastStore()
    store.add('success', 'First')
    store.add('success', 'Second')

    expect(store.toasts).toHaveLength(2)
    expect(store.toasts[0].id).not.toBe(store.toasts[1].id)
    expect(store.toasts[0].message).toBe('First')
    expect(store.toasts[1].message).toBe('Second')
  })

  it('success type has correct type field', () => {
    const store = useToastStore()
    store.add('success', 'Done')

    expect(store.toasts[0].type).toBe('success')
  })

  it('error type has correct type field', () => {
    const store = useToastStore()
    store.add('error', 'Oops')

    expect(store.toasts[0].type).toBe('error')
  })

  it('success toast auto-dismisses after 4000ms', () => {
    const store = useToastStore()
    store.add('success', 'Done')
    expect(store.toasts).toHaveLength(1)

    vi.advanceTimersByTime(4000)
    expect(store.toasts).toHaveLength(0)
  })

  it('error toast auto-dismisses after 6000ms', () => {
    const store = useToastStore()
    store.add('error', 'Oops')
    expect(store.toasts).toHaveLength(1)

    vi.advanceTimersByTime(5999)
    expect(store.toasts).toHaveLength(1)

    vi.advanceTimersByTime(1)
    expect(store.toasts).toHaveLength(0)
  })

  it('remove() deletes specific toast', () => {
    const store = useToastStore()
    store.add('success', 'First')
    store.add('success', 'Second')

    const idToRemove = store.toasts[0].id
    store.remove(idToRemove)

    expect(store.toasts).toHaveLength(1)
    expect(store.toasts[0].message).toBe('Second')
  })

  it('remove() ignores unknown id', () => {
    const store = useToastStore()
    store.add('success', 'Hello')
    store.remove(9999)

    expect(store.toasts).toHaveLength(1)
  })
})
