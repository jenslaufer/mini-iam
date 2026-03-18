<script setup>
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import {
  Squares2X2Icon,
  UsersIcon,
  CpuChipIcon,
  ArrowRightStartOnRectangleIcon,
  Cog6ToothIcon,
  EnvelopeIcon,
  TagIcon,
  MegaphoneIcon,
  BuildingOfficeIcon,
} from '@heroicons/vue/24/outline'
import { useAuthStore } from '../stores/auth.js'
import { useTenantStore } from '../stores/tenant.js'
import { useRouter } from 'vue-router'

defineProps({ mobile: Boolean })
defineEmits(['close'])

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const tenantStore = useTenantStore()

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

const platformItems = [
  { label: 'Tenants', to: '/tenants', icon: BuildingOfficeIcon },
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

    <!-- Tenant selector (platform admin only) -->
    <div v-if="tenantStore.isPlatformAdmin" class="px-3 pb-2">
      <select
        :value="tenantStore.currentSlug"
        @change="tenantStore.select($event.target.value)"
        class="w-full bg-slate-800 text-white text-sm rounded-lg border border-slate-700 px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
      >
        <option v-for="t in tenantStore.tenants" :key="t.slug" :value="t.slug">
          {{ t.name }}
        </option>
      </select>
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

      <!-- Platform section (platform admin only) -->
      <template v-if="tenantStore.isPlatformAdmin">
        <div class="pt-4 pb-1 px-3">
          <p class="text-xs font-semibold text-slate-500 uppercase tracking-wider">Platform</p>
        </div>
        <RouterLink
          v-for="item in platformItems"
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
      </template>
    </nav>

    <!-- Logout -->
    <div class="px-3 py-4 shrink-0">
      <RouterLink
        to="/settings"
        @click="$emit('close')"
        :class="[
          'w-full flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors mb-1',
          isActive('/settings')
            ? 'bg-slate-800 text-white'
            : 'text-slate-400 hover:bg-slate-800 hover:text-white',
        ]"
      >
        <Cog6ToothIcon class="w-5 h-5 shrink-0" />
        Settings
      </RouterLink>
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
