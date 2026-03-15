import { ref } from 'vue'

const visible = ref(false)
const title = ref('')
const description = ref('')
let resolvePromise = null

export function useConfirm() {
  function confirm(opts) {
    title.value = opts.title || 'Confirm'
    description.value = opts.description || ''
    visible.value = true
    return new Promise((resolve) => {
      resolvePromise = resolve
    })
  }

  function onConfirm() {
    visible.value = false
    resolvePromise?.(true)
  }

  function onCancel() {
    visible.value = false
    resolvePromise?.(false)
  }

  return { visible, title, description, confirm, onConfirm, onCancel }
}
