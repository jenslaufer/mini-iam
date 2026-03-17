<script setup>
import { ref, onMounted } from 'vue'
import BaseButton from '../components/BaseButton.vue'
import BaseModal from '../components/BaseModal.vue'
import { getTenants, deleteTenant, exportTenant, importTenant } from '../api/tenants.js'
import { useToastStore } from '../stores/toast.js'
import { useConfirm } from '../composables/useConfirm.js'
import { useTenantStore } from '../stores/tenant.js'

const toast = useToastStore()
const { confirm } = useConfirm()
const tenantStore = useTenantStore()

const tenants = ref([])
const loading = ref(true)

const showImportModal = ref(false)
const importing = ref(false)
const importFile = ref(null)

const importedClients = ref(null)

onMounted(async () => {
  try {
    tenants.value = (await getTenants()) || []
  } catch {
    toast.add('error', 'Failed to load tenants')
  } finally {
    loading.value = false
  }
})

function formatDate(iso) {
  return new Date(iso).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  })
}

function openImportModal() {
  importFile.value = null
  showImportModal.value = true
}

function onFileChange(e) {
  importFile.value = e.target.files[0] || null
}

async function submitImport() {
  if (!importFile.value) return
  importing.value = true
  try {
    const text = await importFile.value.text()
    const data = JSON.parse(text)
    const result = await importTenant(data)
    tenants.value = await getTenants()
    await tenantStore.loadTenants()
    showImportModal.value = false
    toast.add('success', `Tenant "${result.slug}" imported`)
    if (result.clients?.some((c) => c.client_secret)) {
      importedClients.value = result.clients.filter((c) => c.client_secret)
    }
  } catch (e) {
    toast.add('error', e.response?.data?.detail || 'Failed to import tenant')
  } finally {
    importing.value = false
  }
}

async function doExport(tenant) {
  try {
    const data = await exportTenant(tenant.id)
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${tenant.slug}.json`
    a.click()
    URL.revokeObjectURL(url)
  } catch {
    toast.add('error', 'Failed to export tenant')
  }
}

async function remove(tenant) {
  const ok = await confirm({
    title: `Delete "${tenant.name}"?`,
    description: 'This action cannot be undone.',
  })
  if (!ok) return
  try {
    await deleteTenant(tenant.id)
    tenants.value = tenants.value.filter((t) => t.id !== tenant.id)
    await tenantStore.loadTenants()
    toast.add('success', 'Tenant deleted')
  } catch {
    toast.add('error', 'Failed to delete tenant')
  }
}
</script>

<template>
  <div class="space-y-4 max-w-5xl">
    <!-- Header -->
    <div class="flex justify-end">
      <BaseButton @click="openImportModal">Import Tenant</BaseButton>
    </div>

    <!-- Table -->
    <div class="bg-white rounded-xl shadow-sm border border-slate-200 overflow-hidden">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b border-slate-200 bg-slate-50">
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Name</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Slug</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Created</th>
            <th class="px-4 py-3"></th>
          </tr>
        </thead>
        <tbody>
          <!-- Skeleton -->
          <template v-if="loading">
            <tr v-for="i in 3" :key="i" class="border-b border-slate-100">
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-32"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-24"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-20"></div></td>
              <td class="px-4 py-3"></td>
            </tr>
          </template>

          <!-- Empty -->
          <tr v-else-if="tenants.length === 0">
            <td colspan="4" class="px-4 py-12 text-center text-slate-400 text-sm">
              No tenants yet
            </td>
          </tr>

          <!-- Rows -->
          <tr
            v-else
            v-for="tenant in tenants"
            :key="tenant.id"
            class="border-b border-slate-100 last:border-0 hover:bg-slate-50 transition-colors"
          >
            <td class="px-4 py-3 text-slate-900 font-medium">{{ tenant.name }}</td>
            <td class="px-4 py-3">
              <code class="font-mono text-xs bg-slate-100 px-2 py-1 rounded text-slate-700">{{ tenant.slug }}</code>
            </td>
            <td class="px-4 py-3 text-slate-500">{{ formatDate(tenant.created_at) }}</td>
            <td class="px-4 py-3 text-right">
              <div class="flex items-center justify-end gap-2">
                <button
                  @click="doExport(tenant)"
                  class="px-3 py-1.5 rounded-lg border border-slate-200 text-xs text-slate-600 hover:bg-slate-50 transition-colors"
                >
                  Export
                </button>
                <button
                  @click="remove(tenant)"
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

    <!-- Import modal -->
    <BaseModal v-model:show="showImportModal" title="Import Tenant">
      <form @submit.prevent="submitImport" class="space-y-4">
        <div class="flex flex-col gap-1">
          <label class="text-sm font-medium text-slate-700">
            Tenant file <span class="text-red-500 ml-0.5">*</span>
          </label>
          <input
            type="file"
            accept=".json"
            required
            :disabled="importing"
            @change="onFileChange"
            class="w-full text-sm text-slate-700 file:mr-3 file:py-1.5 file:px-3 file:rounded-lg file:border file:border-slate-200 file:text-xs file:font-medium file:text-slate-600 file:bg-white hover:file:bg-slate-50 file:transition-colors"
          />
          <p class="text-xs text-slate-400">JSON export from another LaunchKit instance</p>
        </div>
        <div class="flex justify-end gap-3 pt-2">
          <BaseButton variant="ghost" type="button" @click="showImportModal = false" :disabled="importing">
            Cancel
          </BaseButton>
          <BaseButton type="submit" :loading="importing">Import</BaseButton>
        </div>
      </form>
    </BaseModal>

    <!-- Client secrets modal -->
    <BaseModal v-model:show="importedClients" title="Client Secrets">
      <div class="space-y-4">
        <div class="flex items-start gap-3 p-3 rounded-lg bg-amber-50 border border-amber-300 text-amber-800 text-sm">
          <p class="font-semibold">Save these — they won't be shown again</p>
        </div>
        <div
          v-for="client in importedClients"
          :key="client.client_id"
          class="space-y-1"
        >
          <p class="text-xs font-medium text-slate-500">{{ client.name || client.client_id }}</p>
          <div class="flex flex-col gap-0.5">
            <p class="text-xs text-slate-400">Client ID</p>
            <code class="font-mono text-xs bg-slate-100 px-2 py-1 rounded text-slate-700 break-all">{{ client.client_id }}</code>
          </div>
          <div class="flex flex-col gap-0.5">
            <p class="text-xs text-slate-400">Client Secret</p>
            <code class="font-mono text-xs bg-slate-100 px-2 py-1 rounded text-slate-700 break-all">{{ client.client_secret }}</code>
          </div>
        </div>
        <div class="flex justify-end pt-2">
          <BaseButton @click="importedClients = null">Done</BaseButton>
        </div>
      </div>
    </BaseModal>
  </div>
</template>
