<script setup>
import { ref, computed, onMounted } from 'vue'
import { RouterLink } from 'vue-router'
import {
  UsersIcon,
  ShieldCheckIcon,
  CpuChipIcon,
  ArrowRightIcon,
  EnvelopeIcon,
  TagIcon,
  MegaphoneIcon,
} from '@heroicons/vue/24/outline'
import StatCard from '../components/StatCard.vue'
import { getUsers } from '../api/users.js'
import { getClients } from '../api/clients.js'
import { listContacts } from '../api/contacts.js'
import { listSegments } from '../api/segments.js'
import { listCampaigns } from '../api/campaigns.js'

const users = ref([])
const clients = ref([])
const contacts = ref([])
const segments = ref([])
const campaigns = ref([])
const loading = ref(true)

onMounted(async () => {
  try {
    ;[users.value, clients.value, contacts.value, segments.value, campaigns.value] = await Promise.all([
      getUsers(),
      getClients(),
      listContacts(),
      listSegments(),
      listCampaigns(),
    ])
  } finally {
    loading.value = false
  }
})

const totalUsers = computed(() => users.value.length)
const totalAdmins = computed(() => users.value.filter((u) => u.role === 'admin').length)
const totalClients = computed(() => clients.value.length)
const totalContacts = computed(() => contacts.value.length)
const totalSegments = computed(() => segments.value.length)
const totalCampaigns = computed(() => campaigns.value.length)
</script>

<template>
  <div class="space-y-6 max-w-4xl">
    <!-- IAM Stats -->
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

    <!-- Marketing Stats -->
    <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
      <StatCard label="Contacts" :value="loading ? '—' : totalContacts" icon-class="bg-indigo-100">
        <template #icon>
          <EnvelopeIcon class="w-6 h-6 text-indigo-600" />
        </template>
      </StatCard>
      <StatCard label="Segments" :value="loading ? '—' : totalSegments" icon-class="bg-amber-100">
        <template #icon>
          <TagIcon class="w-6 h-6 text-amber-600" />
        </template>
      </StatCard>
      <StatCard label="Campaigns" :value="loading ? '—' : totalCampaigns" icon-class="bg-rose-100">
        <template #icon>
          <MegaphoneIcon class="w-6 h-6 text-rose-600" />
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

      <RouterLink
        to="/contacts"
        class="bg-white rounded-xl shadow-sm border border-slate-200 p-6 flex items-center justify-between group hover:border-indigo-300 transition-colors"
      >
        <div>
          <p class="font-semibold text-slate-900 mb-1">Contacts</p>
          <p class="text-sm text-slate-500">Manage subscribers and import lists</p>
        </div>
        <ArrowRightIcon class="w-5 h-5 text-slate-400 group-hover:text-indigo-600 transition-colors" />
      </RouterLink>

      <RouterLink
        to="/campaigns"
        class="bg-white rounded-xl shadow-sm border border-slate-200 p-6 flex items-center justify-between group hover:border-rose-300 transition-colors"
      >
        <div>
          <p class="font-semibold text-slate-900 mb-1">Campaigns</p>
          <p class="text-sm text-slate-500">Create and send email campaigns</p>
        </div>
        <ArrowRightIcon class="w-5 h-5 text-slate-400 group-hover:text-rose-600 transition-colors" />
      </RouterLink>
    </div>
  </div>
</template>
