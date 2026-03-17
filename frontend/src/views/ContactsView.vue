<script setup>
import { ref, computed, onMounted } from 'vue'
import { MagnifyingGlassIcon } from '@heroicons/vue/24/outline'
import BaseButton from '../components/BaseButton.vue'
import BaseInput from '../components/BaseInput.vue'
import BaseModal from '../components/BaseModal.vue'
import { listContacts, createContact, deleteContact, importContacts } from '../api/contacts.js'
import { listSegments } from '../api/segments.js'
import { useToastStore } from '../stores/toast.js'
import { useConfirm } from '../composables/useConfirm.js'

const toast = useToastStore()
const { confirm } = useConfirm()

const contacts = ref([])
const segments = ref([])
const loading = ref(true)
const search = ref('')

const showAddModal = ref(false)
const adding = ref(false)
const addForm = ref({ email: '', name: '' })

const showImportModal = ref(false)
const importing = ref(false)
const importText = ref('')
const importSegmentId = ref('')

onMounted(async () => {
  try {
    const [c, s] = await Promise.all([listContacts(), listSegments()])
    contacts.value = c || []
    segments.value = s || []
  } catch {
    toast.add('error', 'Failed to load contacts')
  } finally {
    loading.value = false
  }
})

const filtered = computed(() => {
  const q = search.value.toLowerCase()
  if (!q) return contacts.value
  return contacts.value.filter(
    (c) => c.email.toLowerCase().includes(q) || c.name?.toLowerCase().includes(q),
  )
})

function formatDate(iso) {
  return new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

function openAddModal() {
  addForm.value = { email: '', name: '' }
  showAddModal.value = true
}

async function submitAdd() {
  adding.value = true
  try {
    const contact = await createContact({ email: addForm.value.email, name: addForm.value.name || undefined })
    contacts.value.push(contact)
    showAddModal.value = false
    toast.add('success', 'Contact added')
  } catch (e) {
    toast.add('error', e.response?.data?.detail || 'Failed to add contact')
  } finally {
    adding.value = false
  }
}

function openImportModal() {
  importText.value = ''
  importSegmentId.value = ''
  showImportModal.value = true
}

async function submitImport() {
  importing.value = true
  try {
    const lines = importText.value.split('\n').map((l) => l.trim()).filter(Boolean)
    let parsed
    try {
      parsed = JSON.parse(importText.value)
      if (!Array.isArray(parsed)) throw new Error()
    } catch {
      parsed = lines.map((line) => {
        const parts = line.split(',')
        const entry = { email: parts[0].trim() }
        if (parts[1]) entry.name = parts[1].trim()
        if (importSegmentId.value) entry.segments = [importSegmentId.value]
        return entry
      })
    }
    if (importSegmentId.value) {
      parsed = parsed.map((c) => ({ ...c, segments: [...(c.segments || []), importSegmentId.value] }))
    }
    const result = await importContacts({ contacts: parsed })
    const [fresh, segs] = await Promise.all([listContacts(), listSegments()])
    contacts.value = fresh
    segments.value = segs
    showImportModal.value = false
    toast.add('success', `${result.imported} imported, ${result.skipped} skipped`)
  } catch (e) {
    toast.add('error', e.response?.data?.detail || 'Failed to import contacts')
  } finally {
    importing.value = false
  }
}

async function remove(contact) {
  const ok = await confirm({
    title: `Delete ${contact.email}?`,
    description: 'This action cannot be undone.',
  })
  if (!ok) return
  try {
    await deleteContact(contact.id)
    contacts.value = contacts.value.filter((c) => c.id !== contact.id)
    toast.add('success', 'Contact deleted')
  } catch {
    toast.add('error', 'Failed to delete contact')
  }
}
</script>

<template>
  <div class="space-y-4 max-w-5xl">
    <!-- Toolbar -->
    <div class="flex items-center gap-3">
      <div class="relative flex-1 max-w-xs">
        <MagnifyingGlassIcon class="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400" />
        <input
          v-model="search"
          type="search"
          placeholder="Search contacts..."
          class="w-full pl-9 pr-3 py-2 rounded-lg border border-slate-200 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
      </div>
      <div class="ml-auto flex gap-2">
        <BaseButton variant="ghost" @click="openImportModal">Import</BaseButton>
        <BaseButton @click="openAddModal">+ Add Contact</BaseButton>
      </div>
    </div>

    <!-- Table -->
    <div class="bg-white rounded-xl shadow-sm border border-slate-200 overflow-hidden">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b border-slate-200 bg-slate-50">
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Email</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Name</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Segments</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Status</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Consent</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Created</th>
            <th class="px-4 py-3"></th>
          </tr>
        </thead>
        <tbody>
          <!-- Skeleton -->
          <template v-if="loading">
            <tr v-for="i in 4" :key="i" class="border-b border-slate-100">
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-40"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-24"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-32"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-20"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-20"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-20"></div></td>
              <td class="px-4 py-3"></td>
            </tr>
          </template>

          <!-- Empty -->
          <tr v-else-if="filtered.length === 0">
            <td colspan="7" class="px-4 py-12 text-center text-slate-400 text-sm">
              No contacts found
            </td>
          </tr>

          <!-- Rows -->
          <tr
            v-else
            v-for="contact in filtered"
            :key="contact.id"
            class="border-b border-slate-100 last:border-0 hover:bg-slate-50 transition-colors"
          >
            <td class="px-4 py-3 text-slate-900 font-medium">{{ contact.email }}</td>
            <td class="px-4 py-3 text-slate-600">{{ contact.name || '—' }}</td>
            <td class="px-4 py-3">
              <div class="flex flex-wrap gap-1">
                <span
                  v-for="seg in contact.segments"
                  :key="seg.id"
                  class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-indigo-100 text-indigo-700"
                >
                  {{ seg.name }}
                </span>
                <span v-if="!contact.segments?.length" class="text-slate-400 text-xs">—</span>
              </div>
            </td>
            <td class="px-4 py-3">
              <span
                :class="[
                  'inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium',
                  contact.unsubscribed
                    ? 'bg-red-100 text-red-700'
                    : 'bg-green-100 text-green-700',
                ]"
              >
                {{ contact.unsubscribed ? 'Unsubscribed' : 'Subscribed' }}
              </span>
            </td>
            <td class="px-4 py-3 text-slate-500 text-xs">{{ contact.consent_source || '—' }}</td>
            <td class="px-4 py-3 text-slate-500">{{ formatDate(contact.created_at) }}</td>
            <td class="px-4 py-3 text-right">
              <button
                @click="remove(contact)"
                class="px-3 py-1.5 rounded-lg border border-red-200 text-xs text-red-600 hover:bg-red-50 transition-colors"
              >
                Delete
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Add Contact Modal -->
    <BaseModal v-model:show="showAddModal" title="Add Contact">
      <form @submit.prevent="submitAdd" class="space-y-4">
        <BaseInput
          label="Email"
          type="email"
          v-model="addForm.email"
          placeholder="user@example.com"
          required
          :disabled="adding"
        />
        <BaseInput
          label="Name"
          v-model="addForm.name"
          placeholder="Jane Smith"
          :disabled="adding"
        />
        <div class="flex justify-end gap-3 pt-2">
          <BaseButton variant="ghost" type="button" @click="showAddModal = false" :disabled="adding">
            Cancel
          </BaseButton>
          <BaseButton type="submit" :loading="adding">Add Contact</BaseButton>
        </div>
      </form>
    </BaseModal>

    <!-- Import Modal -->
    <BaseModal v-model:show="showImportModal" title="Import Contacts">
      <form @submit.prevent="submitImport" class="space-y-4">
        <div class="flex flex-col gap-1">
          <label class="text-sm font-medium text-slate-700">
            Contacts <span class="text-red-500 ml-0.5">*</span>
          </label>
          <textarea
            v-model="importText"
            rows="6"
            required
            :disabled="importing"
            placeholder="One email per line, or email,name pairs, or JSON array"
            class="w-full px-3 py-2 rounded-lg border border-slate-200 text-sm text-slate-900 placeholder-slate-400 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 resize-none font-mono"
          ></textarea>
          <p class="text-xs text-slate-400">One email per line, or <code>email,name</code> pairs, or JSON array</p>
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-sm font-medium text-slate-700">Add to segment (optional)</label>
          <select
            v-model="importSegmentId"
            :disabled="importing"
            class="w-full px-3 py-2 rounded-lg border border-slate-200 text-sm text-slate-900 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
          >
            <option value="">— None —</option>
            <option v-for="seg in segments" :key="seg.id" :value="seg.id">{{ seg.name }}</option>
          </select>
        </div>
        <div class="flex justify-end gap-3 pt-2">
          <BaseButton variant="ghost" type="button" @click="showImportModal = false" :disabled="importing">
            Cancel
          </BaseButton>
          <BaseButton type="submit" :loading="importing">Import</BaseButton>
        </div>
      </form>
    </BaseModal>
  </div>
</template>
