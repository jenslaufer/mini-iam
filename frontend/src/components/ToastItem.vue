<script setup>
import { CheckCircleIcon, XCircleIcon } from '@heroicons/vue/24/outline'
import { useToastStore } from '../stores/toast.js'

const props = defineProps({
  toast: Object,
})

const store = useToastStore()

const styles = {
  success: 'bg-green-50 border-green-200 text-green-800',
  error: 'bg-red-50 border-red-200 text-red-800',
}
</script>

<template>
  <div
    :class="[
      'flex items-start gap-3 px-4 py-3 rounded-xl border shadow-sm text-sm max-w-sm',
      styles[toast.type] || styles.error,
    ]"
  >
    <CheckCircleIcon v-if="toast.type === 'success'" class="w-5 h-5 shrink-0 mt-0.5" />
    <XCircleIcon v-else class="w-5 h-5 shrink-0 mt-0.5" />
    <span class="flex-1">{{ toast.message }}</span>
    <button
      @click="store.remove(toast.id)"
      class="shrink-0 opacity-60 hover:opacity-100 transition-opacity"
      aria-label="Dismiss"
    >
      &times;
    </button>
  </div>
</template>
