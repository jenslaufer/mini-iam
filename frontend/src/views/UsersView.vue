<script setup>
import { ref, computed, onMounted } from 'vue'
import { MagnifyingGlassIcon, ChevronDownIcon } from '@heroicons/vue/24/outline'
import RoleBadge from '../components/RoleBadge.vue'
import BaseInput from '../components/BaseInput.vue'
import { getUsers, updateUser, deleteUser } from '../api/users.js'
import { useToastStore } from '../stores/toast.js'
import { useConfirm } from '../composables/useConfirm.js'

const toast = useToastStore()
const { confirm } = useConfirm()

const users = ref([])
const loading = ref(true)
const search = ref('')
const openDropdown = ref(null)

onMounted(async () => {
  try {
    users.value = (await getUsers()) || []
  } catch {
    toast.add('error', 'Failed to load users')
  } finally {
    loading.value = false
  }
})

const filtered = computed(() => {
  const q = search.value.toLowerCase()
  if (!q) return users.value
  return users.value.filter(
    (u) => u.email.toLowerCase().includes(q) || u.name?.toLowerCase().includes(q),
  )
})

function formatDate(iso) {
  return new Date(iso).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  })
}

function toggleDropdown(id) {
  openDropdown.value = openDropdown.value === id ? null : id
}

async function changeRole(user, role) {
  openDropdown.value = null
  if (user.role === role) return
  try {
    const updated = await updateUser(user.id, { role })
    const idx = users.value.findIndex((u) => u.id === user.id)
    if (idx !== -1) users.value[idx] = updated
    toast.add('success', `Role updated to ${role}`)
  } catch {
    toast.add('error', 'Failed to update role')
  }
}

async function remove(user) {
  const ok = await confirm({
    title: `Delete ${user.email}?`,
    description: 'This action cannot be undone.',
  })
  if (!ok) return
  try {
    await deleteUser(user.id)
    users.value = users.value.filter((u) => u.id !== user.id)
    toast.add('success', 'User deleted')
  } catch {
    toast.add('error', 'Failed to delete user')
  }
}
</script>

<template>
  <div class="space-y-4 max-w-5xl">
    <!-- Search -->
    <div class="relative w-64">
      <MagnifyingGlassIcon class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400" />
      <input
        v-model="search"
        type="search"
        placeholder="Search users..."
        class="w-full pl-9 pr-3 py-2 rounded-lg border border-slate-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
      />
    </div>

    <!-- Table -->
    <div class="bg-white rounded-xl shadow-sm border border-slate-200 overflow-hidden">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b border-slate-200 bg-slate-50">
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Email</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Name</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Role</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Created</th>
            <th class="px-4 py-3"></th>
          </tr>
        </thead>
        <tbody>
          <!-- Skeleton -->
          <template v-if="loading">
            <tr v-for="i in 3" :key="i" class="border-b border-slate-100">
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-40"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-24"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-16"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-20"></div></td>
              <td class="px-4 py-3"></td>
            </tr>
          </template>

          <!-- Empty -->
          <tr v-else-if="filtered.length === 0">
            <td colspan="5" class="px-4 py-12 text-center text-slate-400 text-sm">
              No users found
            </td>
          </tr>

          <!-- Rows -->
          <tr
            v-else
            v-for="user in filtered"
            :key="user.id"
            class="border-b border-slate-100 last:border-0 hover:bg-slate-50 transition-colors"
          >
            <td class="px-4 py-3 text-slate-900 font-medium">{{ user.email }}</td>
            <td class="px-4 py-3 text-slate-600">{{ user.name || '—' }}</td>
            <td class="px-4 py-3"><RoleBadge :role="user.role" /></td>
            <td class="px-4 py-3 text-slate-500">{{ formatDate(user.created_at) }}</td>
            <td class="px-4 py-3">
              <div class="flex items-center gap-2 justify-end">
                <!-- Role dropdown -->
                <div class="relative">
                  <button
                    @click="toggleDropdown(user.id)"
                    class="flex items-center gap-1 px-3 py-1.5 rounded-lg border border-slate-200 text-xs text-slate-700 hover:bg-slate-50 transition-colors"
                  >
                    Role
                    <ChevronDownIcon class="w-3 h-3" />
                  </button>
                  <div
                    v-if="openDropdown === user.id"
                    class="absolute right-0 mt-1 w-28 bg-white border border-slate-200 rounded-lg shadow-md z-10 overflow-hidden"
                  >
                    <button
                      v-for="role in ['user', 'admin']"
                      :key="role"
                      @click="changeRole(user, role)"
                      :class="[
                        'w-full text-left px-3 py-2 text-sm hover:bg-slate-50 transition-colors',
                        user.role === role ? 'font-semibold text-blue-700' : 'text-slate-700',
                      ]"
                    >
                      {{ role }}
                    </button>
                  </div>
                </div>

                <!-- Delete -->
                <button
                  @click="remove(user)"
                  class="px-3 py-1.5 rounded-lg border border-red-200 text-xs text-red-600 hover:bg-red-50 transition-colors"
                >
                  Delete
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
