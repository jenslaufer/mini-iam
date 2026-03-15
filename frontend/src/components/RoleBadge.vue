<script setup>
defineProps({ role: String })
</script>

<template>
  <span
    :class="[
      'inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium',
      role === 'admin'
        ? 'bg-blue-100 text-blue-700'
        : 'bg-slate-100 text-slate-600',
    ]"
  >
    {{ role }}
  </span>
</template>
