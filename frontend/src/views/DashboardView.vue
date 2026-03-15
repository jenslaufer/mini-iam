<script setup>
import { ref, computed, onMounted } from 'vue'
import { RouterLink } from 'vue-router'
import {
  UsersIcon,
  ShieldCheckIcon,
  CpuChipIcon,
  ArrowRightIcon,
} from '@heroicons/vue/24/outline'
import StatCard from '../components/StatCard.vue'
import { getUsers } from '../api/users.js'
import { getClients } from '../api/clients.js'

const users = ref([])
const clients = ref([])
const loading = ref(true)

onMounted(async () => {
  try {
    ;[users.value, clients.value] = await Promise.all([getUsers(), getClients()])
  } finally {
    loading.value = false
  }
})

const totalUsers = computed(() => users.value.length)
const totalAdmins = computed(() => users.value.filter((u) => u.role === 'admin').length)
const totalClients = computed(() => clients.value.length)
</script>

<template>
  <div class="space-y-6 max-w-4xl">
    <!-- Stats -->
    <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
      <StatCard label="Total Users" :value="loading ? '—' : totalUsers" icon-class="bg-blue-100">
        <template #icon>
          <UsersIcon class="w-6 h-6 text-blue-600" />
        </template>
      </StatCard>
      <StatCard label="Admins" :value="loading ? '—' : totalAdmins" icon-class="bg-violet-100">
        <template #icon>
          <ShieldCheckIcon class="w-6 h-6 text-violet-600" />
        </template>
      </StatCard>
      <StatCard label="OAuth2 Clients" :value="loading ? '—' : totalClients" icon-class="bg-emerald-100">
        <template #icon>
          <CpuChipIcon class="w-6 h-6 text-emerald-600" />
        </template>
      </StatCard>
    </div>

    <!-- Quick links -->
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
      <RouterLink
        to="/users"
        class="bg-white rounded-xl shadow-sm border border-slate-200 p-6 flex items-center justify-between group hover:border-blue-300 transition-colors"
      >
        <div>
          <p class="font-semibold text-slate-900 mb-1">Manage Users</p>
          <p class="text-sm text-slate-500">View, edit roles, and remove users</p>
        </div>
        <ArrowRightIcon class="w-5 h-5 text-slate-400 group-hover:text-blue-600 transition-colors" />
      </RouterLink>

      <RouterLink
        to="/clients"
        class="bg-white rounded-xl shadow-sm border border-slate-200 p-6 flex items-center justify-between group hover:border-blue-300 transition-colors"
      >
        <div>
          <p class="font-semibold text-slate-900 mb-1">OAuth2 Clients</p>
          <p class="text-sm text-slate-500">Register and manage OIDC applications</p>
        </div>
        <ArrowRightIcon class="w-5 h-5 text-slate-400 group-hover:text-blue-600 transition-colors" />
      </RouterLink>
    </div>
  </div>
</template>
