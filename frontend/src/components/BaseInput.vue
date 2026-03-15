<script setup>
defineProps({
  label: String,
  modelValue: String,
  type: { type: String, default: 'text' },
  placeholder: String,
  error: String,
  required: Boolean,
  disabled: Boolean,
})

defineEmits(['update:modelValue'])
</script>

<template>
  <div class="flex flex-col gap-1">
    <label v-if="label" class="text-sm font-medium text-slate-700">
      {{ label }}
      <span v-if="required" class="text-red-500 ml-0.5">*</span>
    </label>
    <input
      :type="type"
      :value="modelValue"
      :placeholder="placeholder"
      :required="required"
      :disabled="disabled"
      :class="[
        'w-full px-3 py-2 rounded-lg border text-sm text-slate-900 placeholder-slate-400 transition-colors focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500',
        error ? 'border-red-400' : 'border-slate-200',
        disabled && 'opacity-60 bg-slate-50 cursor-not-allowed',
      ]"
      @input="$emit('update:modelValue', $event.target.value)"
    />
    <p v-if="error" class="text-xs text-red-600">{{ error }}</p>
  </div>
</template>
