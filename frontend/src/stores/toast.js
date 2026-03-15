import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useToastStore = defineStore('toast', () => {
  const toasts = ref([])
  let nextId = 0

  function add(type, message) {
    const id = nextId++
    toasts.value.push({ id, type, message })
    const delay = type === 'error' ? 6000 : 4000
    setTimeout(() => remove(id), delay)
  }

  function remove(id) {
    toasts.value = toasts.value.filter((t) => t.id !== id)
  }

  return { toasts, add, remove }
})
