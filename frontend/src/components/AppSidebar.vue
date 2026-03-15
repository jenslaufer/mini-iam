<script setup>
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import {
  Squares2X2Icon,
  UsersIcon,
  CpuChipIcon,
  ArrowRightStartOnRectangleIcon,
  EnvelopeIcon,
  TagIcon,
  MegaphoneIcon,
} from '@heroicons/vue/24/outline'
import { useAuthStore } from '../stores/auth.js'
import { useRouter } from 'vue-router'

defineProps({ mobile: Boolean })
defineEmits(['close'])

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const navItems = [
  { label: 'Dashboard', to: '/dashboard', icon: Squares2X2Icon },
  { label: 'Users', to: '/users', icon: UsersIcon },
  { label: 'Clients', to: '/clients', icon: CpuChipIcon },
]

const marketingItems = [
  { label: 'Contacts', to: '/contacts', icon: EnvelopeIcon },
  { label: 'Segments', to: '/segments', icon: TagIcon },
  { label: 'Campaigns', to: '/campaigns', icon: MegaphoneIcon },
]

function logout() {
  auth.logout()
  router.push('/login')
}

function isActive(path) {
  return route.path === path
}
</script>

<template>
  <aside class="flex flex-col w-60 bg-slate-900 min-h-0 h-full">
    <!-- Brand -->
    <div class="flex items-center gap-2 px-5 py-5 shrink-0">
      <div class="w-6 h-6 bg-blue-600 rounded-sm shrink-0"></div>
      <span class="text-white font-semibold text-base tracking-tight">launch-kit</span>
    </div>

    <!-- Nav -->
    <nav class="flex-1 px-3 py-2 space-y-0.5 overflow-y-auto">
      <RouterLink
        v-for="item in navItems"
        :key="item.to"
        :to="item.to"
        @click="$emit('close')"
        :class="[
          'flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
          isActive(item.to)
            ? 'bg-slate-800 text-white'
            : 'text-slate-300 hover:bg-slate-800 hover:text-white',
        ]"
      >
        <component :is="item.icon" class="w-5 h-5 shrink-0" />
        {{ item.label }}
      </RouterLink>

      <!-- Marketing section -->
      <div class="pt-4 pb-1 px-3">
        <p class="text-xs font-semibold text-slate-500 uppercase tracking-wider">Marketing</p>
      </div>
      <RouterLink
        v-for="item in marketingItems"
        :key="item.to"
        :to="item.to"
        @click="$emit('close')"
        :class="[
          'flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
          isActive(item.to)
            ? 'bg-slate-800 text-white'
            : 'text-slate-300 hover:bg-slate-800 hover:text-white',
        ]"
      >
        <component :is="item.icon" class="w-5 h-5 shrink-0" />
        {{ item.label }}
      </RouterLink>
    </nav>

    <!-- Logout -->
    <div class="px-3 py-4 shrink-0">
      <button
        @click="logout"
        class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium text-slate-400 hover:bg-slate-800 hover:text-white transition-colors"
      >
        <ArrowRightStartOnRectangleIcon class="w-5 h-5 shrink-0" />
        Sign out
      </button>
    </div>
  </aside>
</template>
