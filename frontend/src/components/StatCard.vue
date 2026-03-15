<script setup>
defineProps({
  label: String,
  value: [String, Number],
  iconClass: String,
})
</script>

<template>
  <div class="bg-white rounded-xl shadow-sm border border-slate-200 p-6 flex items-center gap-4">
    <div :class="['p-3 rounded-xl', iconClass]">
      <slot name="icon" />
    </div>
    <div>
      <p class="text-sm text-slate-500">{{ label }}</p>
      <p class="text-2xl font-semibold text-slate-900">{{ value }}</p>
    </div>
  </div>
</template>
