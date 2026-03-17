<script setup>
import { ref, onMounted, watch } from 'vue'
import { ChevronDownIcon, ChevronRightIcon } from '@heroicons/vue/24/outline'
import BaseButton from '../components/BaseButton.vue'
import BaseInput from '../components/BaseInput.vue'
import BaseModal from '../components/BaseModal.vue'
import {
  listSegments,
  getSegment,
  createSegment,
  updateSegment,
  deleteSegment,
  removeContactFromSegment,
} from '../api/segments.js'
import { useToastStore } from '../stores/toast.js'
import { useConfirm } from '../composables/useConfirm.js'
import { useTenantStore } from '../stores/tenant.js'

const toast = useToastStore()
const { confirm } = useConfirm()
const tenantStore = useTenantStore()

const segments = ref([])
const loading = ref(true)
const expandedId = ref(null)
const expandedContacts = ref([])
const expandLoading = ref(false)

const showCreateModal = ref(false)
const creating = ref(false)
const createForm = ref({ name: '', description: '' })

const showEditModal = ref(false)
const editing = ref(false)
const editForm = ref({ name: '', description: '' })
const editTarget = ref(null)

async function loadData() {
  loading.value = true
  expandedId.value = null
  expandedContacts.value = []
  try {
    segments.value = (await listSegments()) || []
  } catch {
    toast.add('error', 'Failed to load segments')
  } finally {
    loading.value = false
  }
}

onMounted(loadData)
watch(() => tenantStore.currentSlug, () => { if (!loading.value) loadData() })

function formatDate(iso) {
  return new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

async function toggleExpand(segment) {
  if (expandedId.value === segment.id) {
    expandedId.value = null
    expandedContacts.value = []
    return
  }
  expandedId.value = segment.id
  expandedContacts.value = []
  expandLoading.value = true
  try {
    const detail = await getSegment(segment.id)
    expandedContacts.value = detail.contacts || []
  } catch {
    toast.add('error', 'Failed to load segment contacts')
  } finally {
    expandLoading.value = false
  }
}

function openCreate() {
  createForm.value = { name: '', description: '' }
  showCreateModal.value = true
}

async function submitCreate() {
  creating.value = true
  try {
    const seg = await createSegment({ name: createForm.value.name, description: createForm.value.description })
    segments.value.push(seg)
    showCreateModal.value = false
    toast.add('success', 'Segment created')
  } catch (e) {
    toast.add('error', e.response?.data?.detail || 'Failed to create segment')
  } finally {
    creating.value = false
  }
}

function openEdit(segment) {
  editTarget.value = segment
  editForm.value = { name: segment.name, description: segment.description || '' }
  showEditModal.value = true
}

async function submitEdit() {
  editing.value = true
  try {
    const updated = await updateSegment(editTarget.value.id, {
      name: editForm.value.name,
      description: editForm.value.description,
    })
    const idx = segments.value.findIndex((s) => s.id === editTarget.value.id)
    if (idx !== -1) segments.value[idx] = { ...segments.value[idx], ...updated }
    showEditModal.value = false
    toast.add('success', 'Segment updated')
  } catch (e) {
    toast.add('error', e.response?.data?.detail || 'Failed to update segment')
  } finally {
    editing.value = false
  }
}

async function remove(segment) {
  const hasContacts = segment.contact_count > 0
  const ok = await confirm({
    title: `Delete "${segment.name}"?`,
    description: hasContacts
      ? `This segment has ${segment.contact_count} contact(s). Deleting it will remove them from the segment but not delete the contacts.`
      : 'This action cannot be undone.',
  })
  if (!ok) return
  try {
    await deleteSegment(segment.id)
    segments.value = segments.value.filter((s) => s.id !== segment.id)
    if (expandedId.value === segment.id) expandedId.value = null
    toast.add('success', 'Segment deleted')
  } catch {
    toast.add('error', 'Failed to delete segment')
  }
}

async function removeContact(contact) {
  const ok = await confirm({
    title: `Remove ${contact.email} from segment?`,
    description: 'The contact will not be deleted.',
  })
  if (!ok) return
  try {
    await removeContactFromSegment(expandedId.value, contact.id)
    expandedContacts.value = expandedContacts.value.filter((c) => c.id !== contact.id)
    const seg = segments.value.find((s) => s.id === expandedId.value)
    if (seg) seg.contact_count = Math.max(0, (seg.contact_count || 1) - 1)
    toast.add('success', 'Contact removed from segment')
  } catch {
    toast.add('error', 'Failed to remove contact')
  }
}
</script>

<template>
  <div class="space-y-4 max-w-5xl">
    <!-- Toolbar -->
    <div class="flex justify-end">
      <BaseButton @click="openCreate">+ New Segment</BaseButton>
    </div>

    <!-- Table -->
    <div class="bg-white rounded-xl shadow-sm border border-slate-200 overflow-hidden">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b border-slate-200 bg-slate-50">
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Name</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Description</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Contacts</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Created</th>
            <th class="px-4 py-3"></th>
          </tr>
        </thead>
        <tbody>
          <!-- Skeleton -->
          <template v-if="loading">
            <tr v-for="i in 3" :key="i" class="border-b border-slate-100">
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-32"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-48"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-10"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-20"></div></td>
              <td class="px-4 py-3"></td>
            </tr>
          </template>

          <!-- Empty -->
          <tr v-else-if="segments.length === 0">
            <td colspan="5" class="px-4 py-12 text-center text-slate-400 text-sm">
              No segments yet
            </td>
          </tr>

          <template v-else v-for="segment in segments" :key="segment.id">
            <!-- Segment row -->
            <tr
              class="border-b border-slate-100 hover:bg-slate-50 transition-colors cursor-pointer"
              @click="toggleExpand(segment)"
            >
              <td class="px-4 py-3">
                <div class="flex items-center gap-2">
                  <component
                    :is="expandedId === segment.id ? ChevronDownIcon : ChevronRightIcon"
                    class="w-4 h-4 text-slate-400 shrink-0"
                  />
                  <span class="text-slate-900 font-medium">{{ segment.name }}</span>
                </div>
              </td>
              <td class="px-4 py-3 text-slate-500">{{ segment.description || '—' }}</td>
              <td class="px-4 py-3 text-slate-700">{{ segment.contact_count ?? 0 }}</td>
              <td class="px-4 py-3 text-slate-500">{{ formatDate(segment.created_at) }}</td>
              <td class="px-4 py-3">
                <div class="flex items-center gap-2 justify-end" @click.stop>
                  <button
                    @click="openEdit(segment)"
                    class="px-3 py-1.5 rounded-lg border border-slate-200 text-xs text-slate-600 hover:bg-slate-50 transition-colors"
                  >
                    Edit
                  </button>
                  <button
                    @click="remove(segment)"
                    class="px-3 py-1.5 rounded-lg border border-red-200 text-xs text-red-600 hover:bg-red-50 transition-colors"
                  >
                    Delete
                  </button>
                </div>
              </td>
            </tr>

            <!-- Expanded contacts -->
            <tr v-if="expandedId === segment.id" class="border-b border-slate-100 bg-slate-50">
              <td colspan="5" class="px-8 py-3">
                <div v-if="expandLoading" class="space-y-2 py-2">
                  <div v-for="i in 3" :key="i" class="h-3 bg-slate-200 rounded animate-pulse w-48"></div>
                </div>
                <div v-else-if="expandedContacts.length === 0" class="text-slate-400 text-xs py-2">
                  No contacts in this segment
                </div>
                <table v-else class="w-full text-xs">
                  <thead>
                    <tr>
                      <th class="text-left py-1.5 pr-4 text-slate-400 font-medium">Email</th>
                      <th class="text-left py-1.5 pr-4 text-slate-400 font-medium">Name</th>
                      <th class="py-1.5"></th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr
                      v-for="contact in expandedContacts"
                      :key="contact.id"
                      class="border-t border-slate-100"
                    >
                      <td class="py-2 pr-4 text-slate-700">{{ contact.email }}</td>
                      <td class="py-2 pr-4 text-slate-500">{{ contact.name || '—' }}</td>
                      <td class="py-2 text-right">
                        <button
                          @click="removeContact(contact)"
                          class="text-red-500 hover:text-red-700 transition-colors"
                          title="Remove from segment"
                        >
                          Remove
                        </button>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>

    <!-- Create Modal -->
    <BaseModal v-model:show="showCreateModal" title="New Segment">
      <form @submit.prevent="submitCreate" class="space-y-4">
        <BaseInput label="Name" v-model="createForm.name" placeholder="Newsletter subscribers" required :disabled="creating" />
        <div class="flex flex-col gap-1">
          <label class="text-sm font-medium text-slate-700">Description</label>
          <textarea
            v-model="createForm.description"
            rows="3"
            placeholder="Optional description"
            :disabled="creating"
            class="w-full px-3 py-2 rounded-lg border border-slate-200 text-sm text-slate-900 placeholder-slate-400 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 resize-none"
          ></textarea>
        </div>
        <div class="flex justify-end gap-3 pt-2">
          <BaseButton variant="ghost" type="button" @click="showCreateModal = false" :disabled="creating">Cancel</BaseButton>
          <BaseButton type="submit" :loading="creating">Create Segment</BaseButton>
        </div>
      </form>
    </BaseModal>

    <!-- Edit Modal -->
    <BaseModal v-model:show="showEditModal" title="Edit Segment">
      <form @submit.prevent="submitEdit" class="space-y-4">
        <BaseInput label="Name" v-model="editForm.name" placeholder="Newsletter subscribers" required :disabled="editing" />
        <div class="flex flex-col gap-1">
          <label class="text-sm font-medium text-slate-700">Description</label>
          <textarea
            v-model="editForm.description"
            rows="3"
            placeholder="Optional description"
            :disabled="editing"
            class="w-full px-3 py-2 rounded-lg border border-slate-200 text-sm text-slate-900 placeholder-slate-400 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 resize-none"
          ></textarea>
        </div>
        <div class="flex justify-end gap-3 pt-2">
          <BaseButton variant="ghost" type="button" @click="showEditModal = false" :disabled="editing">Cancel</BaseButton>
          <BaseButton type="submit" :loading="editing">Save Changes</BaseButton>
        </div>
      </form>
    </BaseModal>
  </div>
</template>
