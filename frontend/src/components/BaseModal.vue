<script setup>
import { onMounted, onUnmounted } from 'vue'
import { XMarkIcon } from '@heroicons/vue/24/outline'

const props = defineProps({
  show: Boolean,
  title: String,
})
const emit = defineEmits(['update:show'])

function close() {
  emit('update:show', false)
}

function onKeydown(e) {
  if (e.key === 'Escape' && props.show) close()
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div
        v-if="show"
        class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/50 backdrop-blur-sm"
        @click.self="close"
      >
        <div class="bg-white rounded-2xl shadow-xl w-full max-w-md">
          <div class="flex items-center justify-between px-6 py-4 border-b border-slate-200">
            <h2 class="text-base font-semibold text-slate-900">{{ title }}</h2>
            <button
              @click="close"
              class="text-slate-400 hover:text-slate-600 transition-colors"
              aria-label="Close"
            >
              <XMarkIcon class="w-5 h-5" />
            </button>
          </div>
          <div class="px-6 py-4">
            <slot />
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.modal-enter-active,
.modal-leave-active {
  transition: opacity 0.2s ease;
}
.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}
.modal-enter-active .bg-white,
.modal-leave-active .bg-white {
  transition: transform 0.2s ease;
}
.modal-enter-from .bg-white {
  transform: scale(0.95);
}
</style>
