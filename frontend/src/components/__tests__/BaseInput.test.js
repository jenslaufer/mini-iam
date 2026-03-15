import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import BaseInput from '../BaseInput.vue'

describe('BaseInput', () => {
  it('renders label text', () => {
    const wrapper = mount(BaseInput, { props: { label: 'Email' } })
    expect(wrapper.find('label').text()).toContain('Email')
  })

  it('does not render label element when label prop is absent', () => {
    const wrapper = mount(BaseInput)
    expect(wrapper.find('label').exists()).toBe(false)
  })

  it('emits update:modelValue on input', async () => {
    const wrapper = mount(BaseInput, { props: { modelValue: '' } })
    const input = wrapper.find('input')
    await input.setValue('hello')
    expect(wrapper.emitted('update:modelValue')).toBeTruthy()
    expect(wrapper.emitted('update:modelValue')[0]).toEqual(['hello'])
  })

  it('reflects modelValue in input value', () => {
    const wrapper = mount(BaseInput, { props: { modelValue: 'test@example.com' } })
    expect(wrapper.find('input').element.value).toBe('test@example.com')
  })

  it('shows error message when error prop is set', () => {
    const wrapper = mount(BaseInput, { props: { error: 'Required field' } })
    expect(wrapper.find('p').text()).toBe('Required field')
  })

  it('applies error border class when error prop is set', () => {
    const wrapper = mount(BaseInput, { props: { error: 'Bad input' } })
    expect(wrapper.find('input').classes()).toContain('border-red-400')
  })

  it('uses normal border class when no error', () => {
    const wrapper = mount(BaseInput)
    expect(wrapper.find('input').classes()).toContain('border-slate-200')
  })

  it('disables the input when disabled prop is true', () => {
    const wrapper = mount(BaseInput, { props: { disabled: true } })
    expect(wrapper.find('input').attributes('disabled')).toBeDefined()
  })

  it('renders required asterisk when required prop is true', () => {
    const wrapper = mount(BaseInput, { props: { label: 'Email', required: true } })
    expect(wrapper.find('span.text-red-500').exists()).toBe(true)
  })
})
