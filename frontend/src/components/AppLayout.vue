<script setup>
import { ref } from 'vue'
import AppSidebar from './AppSidebar.vue'
import AppTopBar from './AppTopBar.vue'
import ToastContainer from './ToastContainer.vue'
import ConfirmDialog from './ConfirmDialog.vue'

const sidebarOpen = ref(false)
</script>

<template>
  <div class="flex h-screen bg-slate-50 overflow-hidden">
    <!-- Desktop sidebar -->
    <div class="hidden lg:flex lg:shrink-0">
      <AppSidebar />
    </div>

    <!-- Mobile sidebar overlay -->
    <Transition name="overlay">
      <div
        v-if="sidebarOpen"
        class="fixed inset-0 z-40 lg:hidden"
        @click="sidebarOpen = false"
      >
        <div class="absolute inset-0 bg-slate-900/50" />
        <div class="relative flex h-full w-60">
          <AppSidebar mobile @close="sidebarOpen = false" />
        </div>
      </div>
    </Transition>

    <!-- Main area -->
    <div class="flex flex-col flex-1 min-w-0">
      <AppTopBar @toggle-sidebar="sidebarOpen = !sidebarOpen" />
      <main class="flex-1 overflow-y-auto p-6">
        <RouterView />
      </main>
    </div>

    <ToastContainer />
    <ConfirmDialog />
  </div>
</template>

<style scoped>
.overlay-enter-active,
.overlay-leave-active {
  transition: opacity 0.2s ease;
}
.overlay-enter-from,
.overlay-leave-to {
  opacity: 0;
}
</style>
