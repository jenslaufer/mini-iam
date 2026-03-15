<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { Bars3Icon } from '@heroicons/vue/24/outline'
import { useAuthStore } from '../stores/auth.js'

defineEmits(['toggle-sidebar'])

const route = useRoute()
const auth = useAuthStore()

const pageTitle = computed(() => {
  const map = { '/dashboard': 'Dashboard', '/users': 'Users', '/clients': 'Clients' }
  return map[route.path] || 'Admin'
})

const avatarLetter = computed(() =>
  auth.adminEmail ? auth.adminEmail[0].toUpperCase() : 'A',
)
</script>

<template>
  <header class="h-16 bg-white border-b border-slate-200 flex items-center px-6 gap-4 shrink-0">
    <button
      @click="$emit('toggle-sidebar')"
      class="lg:hidden text-slate-500 hover:text-slate-700 transition-colors"
      aria-label="Toggle sidebar"
    >
      <Bars3Icon class="w-6 h-6" />
    </button>

    <h1 class="text-base font-semibold text-slate-900 flex-1">{{ pageTitle }}</h1>

    <div class="flex items-center gap-3">
      <span class="text-sm text-slate-500 hidden sm:block">{{ auth.adminEmail }}</span>
      <div
        class="w-8 h-8 rounded-full bg-blue-600 text-white text-sm font-semibold flex items-center justify-center shrink-0"
      >
        {{ avatarLetter }}
      </div>
    </div>
  </header>
</template>
