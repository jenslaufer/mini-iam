<script setup>
import { ref, onMounted } from 'vue'
import { ClipboardDocumentIcon, XMarkIcon } from '@heroicons/vue/24/outline'
import BaseButton from '../components/BaseButton.vue'
import BaseInput from '../components/BaseInput.vue'
import BaseModal from '../components/BaseModal.vue'
import { getClients, createClient, deleteClient } from '../api/clients.js'
import { useToastStore } from '../stores/toast.js'
import { useConfirm } from '../composables/useConfirm.js'

const toast = useToastStore()
const { confirm } = useConfirm()

const clients = ref([])
const loading = ref(true)
const showModal = ref(false)
const creating = ref(false)
const newSecret = ref(null)

const form = ref({ name: '', redirectUris: '' })

onMounted(async () => {
  try {
    clients.value = await getClients()
  } catch {
    toast.add('error', 'Failed to load clients')
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

function openModal() {
  form.value = { name: '', redirectUris: '' }
  showModal.value = true
}

async function submitCreate() {
  creating.value = true
  try {
    const redirect_uris = form.value.redirectUris
      .split('\n')
      .map((u) => u.trim())
      .filter(Boolean)
    const data = await createClient({ name: form.value.name, redirect_uris })
    clients.value.push(data)
    showModal.value = false
    newSecret.value = { id: data.client_id, secret: data.client_secret }
    toast.add('success', 'Client created')
  } catch (e) {
    toast.add('error', e.response?.data?.error_description || 'Failed to create client')
  } finally {
    creating.value = false
  }
}

async function remove(client) {
  const ok = await confirm({
    title: `Delete "${client.name}"?`,
    description: 'This action cannot be undone.',
  })
  if (!ok) return
  try {
    await deleteClient(client.client_id)
    clients.value = clients.value.filter((c) => c.client_id !== client.client_id)
    toast.add('success', 'Client deleted')
  } catch {
    toast.add('error', 'Failed to delete client')
  }
}

async function copySecret() {
  if (!newSecret.value) return
  await navigator.clipboard.writeText(newSecret.value.secret)
  toast.add('success', 'Secret copied to clipboard')
}

function truncate(str, n = 16) {
  return str.length > n ? str.slice(0, n) + '…' : str
}
</script>

<template>
  <div class="space-y-4 max-w-5xl">
    <!-- Header -->
    <div class="flex justify-end">
      <BaseButton @click="openModal">+ New Client</BaseButton>
    </div>

    <!-- Secret alert -->
    <div
      v-if="newSecret"
      class="flex items-start gap-3 p-4 rounded-xl border border-amber-300 bg-amber-50 text-amber-800 text-sm"
    >
      <div class="flex-1">
        <p class="font-semibold mb-1">Client secret — copy now, never shown again</p>
        <code class="font-mono text-xs break-all">{{ newSecret.secret }}</code>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        <button
          @click="copySecret"
          class="p-1.5 rounded-lg hover:bg-amber-100 transition-colors"
          title="Copy to clipboard"
        >
          <ClipboardDocumentIcon class="w-5 h-5" />
        </button>
        <button
          @click="newSecret = null"
          class="p-1.5 rounded-lg hover:bg-amber-100 transition-colors"
          title="Dismiss"
        >
          <XMarkIcon class="w-5 h-5" />
        </button>
      </div>
    </div>

    <!-- Table -->
    <div class="bg-white rounded-xl shadow-sm border border-slate-200 overflow-hidden">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b border-slate-200 bg-slate-50">
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Client ID</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Name</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Redirect URIs</th>
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
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-48"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-20"></div></td>
              <td class="px-4 py-3"></td>
            </tr>
          </template>

          <!-- Empty -->
          <tr v-else-if="clients.length === 0">
            <td colspan="5" class="px-4 py-12 text-center text-slate-400 text-sm">
              No clients registered yet
            </td>
          </tr>

          <!-- Rows -->
          <tr
            v-else
            v-for="client in clients"
            :key="client.client_id"
            class="border-b border-slate-100 last:border-0 hover:bg-slate-50 transition-colors"
          >
            <td class="px-4 py-3">
              <code class="font-mono text-xs bg-slate-100 px-2 py-1 rounded text-slate-700">
                {{ truncate(client.client_id) }}
              </code>
            </td>
            <td class="px-4 py-3 text-slate-900 font-medium">{{ client.name }}</td>
            <td class="px-4 py-3 text-slate-500 text-xs">
              <span v-for="(uri, i) in client.redirect_uris" :key="i" class="block">{{ uri }}</span>
            </td>
            <td class="px-4 py-3 text-slate-500">{{ formatDate(client.created_at) }}</td>
            <td class="px-4 py-3 text-right">
              <button
                @click="remove(client)"
                class="px-3 py-1.5 rounded-lg border border-red-200 text-xs text-red-600 hover:bg-red-50 transition-colors"
              >
                Delete
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Create modal -->
    <BaseModal v-model:show="showModal" title="New OAuth2 Client">
      <form @submit.prevent="submitCreate" class="space-y-4">
        <BaseInput
          label="Application Name"
          v-model="form.name"
          placeholder="My App"
          required
          :disabled="creating"
        />
        <div class="flex flex-col gap-1">
          <label class="text-sm font-medium text-slate-700">
            Redirect URIs <span class="text-red-500 ml-0.5">*</span>
          </label>
          <textarea
            v-model="form.redirectUris"
            rows="3"
            placeholder="https://app.example.com/callback&#10;http://localhost:3000/callback"
            required
            :disabled="creating"
            class="w-full px-3 py-2 rounded-lg border border-slate-200 text-sm text-slate-900 placeholder-slate-400 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 resize-none"
          ></textarea>
          <p class="text-xs text-slate-400">One URI per line</p>
        </div>
        <div class="flex justify-end gap-3 pt-2">
          <BaseButton variant="ghost" type="button" @click="showModal = false" :disabled="creating">
            Cancel
          </BaseButton>
          <BaseButton type="submit" :loading="creating">Create Client</BaseButton>
        </div>
      </form>
    </BaseModal>
  </div>
</template>
