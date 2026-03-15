<script setup>
import { ArrowPathIcon } from '@heroicons/vue/24/outline'

const props = defineProps({
  variant: { type: String, default: 'primary' },
  loading: Boolean,
  disabled: Boolean,
  type: { type: String, default: 'button' },
})

const variantClasses = {
  primary: 'bg-blue-600 hover:bg-blue-700 text-white border-transparent',
  danger: 'bg-red-600 hover:bg-red-700 text-white border-transparent',
  ghost: 'bg-transparent hover:bg-slate-100 text-slate-700 border border-slate-200',
}
</script>

<template>
  <button
    :type="type"
    :disabled="disabled || loading"
    :class="[
      'inline-flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500',
      variantClasses[variant],
      (disabled || loading) && 'opacity-75 pointer-events-none',
    ]"
  >
    <ArrowPathIcon v-if="loading" class="w-4 h-4 animate-spin" />
    <slot />
  </button>
</template>
