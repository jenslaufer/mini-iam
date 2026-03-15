<script setup>
import { ExclamationTriangleIcon } from '@heroicons/vue/24/outline'
import { useConfirm } from '../composables/useConfirm.js'
import BaseButton from './BaseButton.vue'

const { visible, title, description, onConfirm, onCancel } = useConfirm()
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div
        v-if="visible"
        class="fixed inset-0 z-50 flex items-center justify-center p-4 bg-slate-900/50 backdrop-blur-sm"
      >
        <div class="bg-white rounded-2xl shadow-xl w-full max-w-sm p-6">
          <div class="flex gap-4 items-start mb-4">
            <div class="p-2 rounded-full bg-red-100 shrink-0">
              <ExclamationTriangleIcon class="w-6 h-6 text-red-600" />
            </div>
            <div>
              <h3 class="text-base font-semibold text-slate-900">{{ title }}</h3>
              <p v-if="description" class="text-sm text-slate-500 mt-1">{{ description }}</p>
            </div>
          </div>
          <div class="flex justify-end gap-3">
            <BaseButton variant="ghost" @click="onCancel">Cancel</BaseButton>
            <BaseButton variant="danger" @click="onConfirm">Delete</BaseButton>
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
</style>
