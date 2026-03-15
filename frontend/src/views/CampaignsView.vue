<script setup>
import { ref, computed, onMounted } from 'vue'
import BaseButton from '../components/BaseButton.vue'
import BaseInput from '../components/BaseInput.vue'
import BaseModal from '../components/BaseModal.vue'
import StatCard from '../components/StatCard.vue'
import {
  EnvelopeIcon,
  PaperAirplaneIcon,
  XCircleIcon,
  CheckCircleIcon,
} from '@heroicons/vue/24/outline'
import { listCampaigns, createCampaign, updateCampaign, deleteCampaign, sendCampaign, getCampaignStats } from '../api/campaigns.js'
import { listSegments } from '../api/segments.js'
import { useToastStore } from '../stores/toast.js'
import { useConfirm } from '../composables/useConfirm.js'

const toast = useToastStore()
const { confirm } = useConfirm()

const campaigns = ref([])
const segments = ref([])
const loading = ref(true)

const showFormModal = ref(false)
const saving = ref(false)
const editTarget = ref(null)
const form = ref({ subject: '', from_name: '', from_email: '', html_body: '', segment_ids: [] })

const showDetailModal = ref(false)
const detailCampaign = ref(null)
const detailStats = ref(null)
const statsLoading = ref(false)

const previewMode = ref(false)

onMounted(async () => {
  try {
    ;[campaigns.value, segments.value] = await Promise.all([listCampaigns(), listSegments()])
  } catch {
    toast.add('error', 'Failed to load campaigns')
  } finally {
    loading.value = false
  }
})

function formatDate(iso) {
  if (!iso) return '—'
  return new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

const statusConfig = {
  draft: { label: 'Draft', classes: 'bg-slate-100 text-slate-600' },
  sending: { label: 'Sending', classes: 'bg-amber-100 text-amber-700' },
  sent: { label: 'Sent', classes: 'bg-green-100 text-green-700' },
  failed: { label: 'Failed', classes: 'bg-red-100 text-red-700' },
}

function statusBadge(status) {
  return statusConfig[status] || statusConfig.draft
}

function openCreate() {
  editTarget.value = null
  form.value = { subject: '', from_name: '', from_email: '', html_body: '', segment_ids: [] }
  previewMode.value = false
  showFormModal.value = true
}

function openEdit(campaign) {
  editTarget.value = campaign
  form.value = {
    subject: campaign.subject,
    from_name: campaign.from_name,
    from_email: campaign.from_email,
    html_body: campaign.html_body || '',
    segment_ids: campaign.segment_ids ? [...campaign.segment_ids] : [],
  }
  previewMode.value = false
  showFormModal.value = true
}

function toggleSegment(id) {
  const idx = form.value.segment_ids.indexOf(id)
  if (idx === -1) form.value.segment_ids.push(id)
  else form.value.segment_ids.splice(idx, 1)
}

async function submitForm() {
  saving.value = true
  try {
    const payload = {
      subject: form.value.subject,
      from_name: form.value.from_name,
      from_email: form.value.from_email,
      html_body: form.value.html_body,
      segment_ids: form.value.segment_ids,
    }
    if (editTarget.value) {
      const updated = await updateCampaign(editTarget.value.id, payload)
      const idx = campaigns.value.findIndex((c) => c.id === editTarget.value.id)
      if (idx !== -1) campaigns.value[idx] = { ...campaigns.value[idx], ...updated }
      toast.add('success', 'Campaign updated')
    } else {
      const created = await createCampaign(payload)
      campaigns.value.push(created)
      toast.add('success', 'Campaign created')
    }
    showFormModal.value = false
  } catch (e) {
    toast.add('error', e.response?.data?.detail || 'Failed to save campaign')
  } finally {
    saving.value = false
  }
}

async function send(campaign) {
  const segmentNames = segments.value
    .filter((s) => campaign.segment_ids?.includes(s.id))
    .map((s) => s.name)
    .join(', ')
  const ok = await confirm({
    title: `Send "${campaign.subject}"?`,
    description: `This will send to contacts in: ${segmentNames || 'selected segments'}. This cannot be undone.`,
  })
  if (!ok) return
  try {
    await sendCampaign(campaign.id)
    const idx = campaigns.value.findIndex((c) => c.id === campaign.id)
    if (idx !== -1) campaigns.value[idx] = { ...campaigns.value[idx], status: 'sending' }
    toast.add('success', 'Campaign queued for sending')
  } catch (e) {
    toast.add('error', e.response?.data?.detail || 'Failed to send campaign')
  }
}

async function remove(campaign) {
  const ok = await confirm({
    title: `Delete "${campaign.subject}"?`,
    description: 'This action cannot be undone.',
  })
  if (!ok) return
  try {
    await deleteCampaign(campaign.id)
    campaigns.value = campaigns.value.filter((c) => c.id !== campaign.id)
    toast.add('success', 'Campaign deleted')
  } catch {
    toast.add('error', 'Failed to delete campaign')
  }
}

async function openDetail(campaign) {
  detailCampaign.value = campaign
  detailStats.value = null
  statsLoading.value = true
  showDetailModal.value = true
  try {
    detailStats.value = await getCampaignStats(campaign.id)
  } catch {
    toast.add('error', 'Failed to load campaign stats')
  } finally {
    statsLoading.value = false
  }
}

const openRate = computed(() => {
  if (!detailStats.value?.sent) return '—'
  return ((detailStats.value.opened / detailStats.value.sent) * 100).toFixed(1) + '%'
})
</script>

<template>
  <div class="space-y-4 max-w-5xl">
    <!-- Toolbar -->
    <div class="flex justify-end">
      <BaseButton @click="openCreate">+ New Campaign</BaseButton>
    </div>

    <!-- Table -->
    <div class="bg-white rounded-xl shadow-sm border border-slate-200 overflow-hidden">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b border-slate-200 bg-slate-50">
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Subject</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">From</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Status</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Recipients</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Opened</th>
            <th class="text-left px-4 py-3 text-xs font-medium text-slate-500 uppercase tracking-wide">Sent At</th>
            <th class="px-4 py-3"></th>
          </tr>
        </thead>
        <tbody>
          <!-- Skeleton -->
          <template v-if="loading">
            <tr v-for="i in 3" :key="i" class="border-b border-slate-100">
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-40"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-32"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-16"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-12"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-12"></div></td>
              <td class="px-4 py-3"><div class="h-4 bg-slate-200 rounded animate-pulse w-20"></div></td>
              <td class="px-4 py-3"></td>
            </tr>
          </template>

          <!-- Empty -->
          <tr v-else-if="campaigns.length === 0">
            <td colspan="7" class="px-4 py-12 text-center text-slate-400 text-sm">
              No campaigns yet
            </td>
          </tr>

          <!-- Rows -->
          <tr
            v-else
            v-for="campaign in campaigns"
            :key="campaign.id"
            class="border-b border-slate-100 last:border-0 hover:bg-slate-50 transition-colors"
          >
            <td class="px-4 py-3">
              <button
                @click="openDetail(campaign)"
                class="text-slate-900 font-medium hover:text-blue-600 transition-colors text-left"
              >
                {{ campaign.subject }}
              </button>
            </td>
            <td class="px-4 py-3 text-slate-500 text-xs">
              <span class="block">{{ campaign.from_name }}</span>
              <span class="block text-slate-400">{{ campaign.from_email }}</span>
            </td>
            <td class="px-4 py-3">
              <span :class="['inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium', statusBadge(campaign.status).classes]">
                {{ statusBadge(campaign.status).label }}
              </span>
            </td>
            <td class="px-4 py-3 text-slate-600">{{ campaign.total ?? '—' }}</td>
            <td class="px-4 py-3 text-slate-600">{{ campaign.opened ?? '—' }}</td>
            <td class="px-4 py-3 text-slate-500">{{ formatDate(campaign.sent_at) }}</td>
            <td class="px-4 py-3">
              <div class="flex items-center gap-2 justify-end">
                <button
                  v-if="campaign.status === 'draft'"
                  @click="send(campaign)"
                  class="px-3 py-1.5 rounded-lg border border-blue-200 text-xs text-blue-600 hover:bg-blue-50 transition-colors"
                >
                  Send
                </button>
                <button
                  v-if="campaign.status === 'draft'"
                  @click="openEdit(campaign)"
                  class="px-3 py-1.5 rounded-lg border border-slate-200 text-xs text-slate-600 hover:bg-slate-50 transition-colors"
                >
                  Edit
                </button>
                <button
                  v-if="campaign.status === 'draft'"
                  @click="remove(campaign)"
                  class="px-3 py-1.5 rounded-lg border border-red-200 text-xs text-red-600 hover:bg-red-50 transition-colors"
                >
                  Delete
                </button>
                <button
                  v-if="campaign.status !== 'draft'"
                  @click="openDetail(campaign)"
                  class="px-3 py-1.5 rounded-lg border border-slate-200 text-xs text-slate-600 hover:bg-slate-50 transition-colors"
                >
                  Stats
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Create / Edit Modal -->
    <BaseModal v-model:show="showFormModal" :title="editTarget ? 'Edit Campaign' : 'New Campaign'">
      <form @submit.prevent="submitForm" class="space-y-4">
        <BaseInput
          label="Subject"
          v-model="form.subject"
          placeholder="Welcome to our newsletter"
          required
          :disabled="saving"
        />
        <div class="grid grid-cols-2 gap-3">
          <BaseInput label="From Name" v-model="form.from_name" placeholder="Acme Corp" required :disabled="saving" />
          <BaseInput label="From Email" type="email" v-model="form.from_email" placeholder="hello@acme.com" required :disabled="saving" />
        </div>

        <!-- Segment selection -->
        <div class="flex flex-col gap-1">
          <label class="text-sm font-medium text-slate-700">Segments <span class="text-red-500 ml-0.5">*</span></label>
          <div class="border border-slate-200 rounded-lg p-3 space-y-2 max-h-36 overflow-y-auto">
            <label
              v-for="seg in segments"
              :key="seg.id"
              class="flex items-center gap-2 text-sm text-slate-700 cursor-pointer hover:text-slate-900"
            >
              <input
                type="checkbox"
                :value="seg.id"
                :checked="form.segment_ids.includes(seg.id)"
                @change="toggleSegment(seg.id)"
                :disabled="saving"
                class="rounded border-slate-300 text-blue-600 focus:ring-blue-500"
              />
              <span>{{ seg.name }}</span>
              <span class="text-slate-400 text-xs ml-auto">{{ seg.contact_count ?? 0 }} contacts</span>
            </label>
            <p v-if="segments.length === 0" class="text-slate-400 text-xs">No segments available</p>
          </div>
        </div>

        <!-- HTML body + preview toggle -->
        <div class="flex flex-col gap-1">
          <div class="flex items-center justify-between">
            <label class="text-sm font-medium text-slate-700">HTML Body <span class="text-red-500 ml-0.5">*</span></label>
            <button
              type="button"
              @click="previewMode = !previewMode"
              class="text-xs text-blue-600 hover:text-blue-800 transition-colors"
            >
              {{ previewMode ? 'Edit' : 'Preview' }}
            </button>
          </div>
          <textarea
            v-if="!previewMode"
            v-model="form.html_body"
            rows="8"
            required
            :disabled="saving"
            placeholder="<h1>Hello!</h1><p>Your email content here...</p>"
            class="w-full px-3 py-2 rounded-lg border border-slate-200 text-sm text-slate-900 placeholder-slate-400 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 resize-none font-mono"
          ></textarea>
          <iframe
            v-else
            :srcdoc="form.html_body"
            sandbox="allow-same-origin"
            class="w-full h-48 rounded-lg border border-slate-200 bg-white"
          ></iframe>
        </div>

        <div class="flex justify-end gap-3 pt-2">
          <BaseButton variant="ghost" type="button" @click="showFormModal = false" :disabled="saving">Cancel</BaseButton>
          <BaseButton type="submit" :loading="saving">{{ editTarget ? 'Save Changes' : 'Create Campaign' }}</BaseButton>
        </div>
      </form>
    </BaseModal>

    <!-- Detail / Stats Modal -->
    <BaseModal v-model:show="showDetailModal" :title="detailCampaign?.subject || 'Campaign Stats'">
      <div class="space-y-4">
        <div v-if="statsLoading" class="grid grid-cols-2 gap-3">
          <div v-for="i in 4" :key="i" class="h-20 bg-slate-200 rounded-xl animate-pulse"></div>
        </div>
        <div v-else-if="detailStats" class="grid grid-cols-2 gap-3">
          <StatCard label="Total" :value="detailStats.total ?? 0" icon-class="bg-slate-100">
            <template #icon><EnvelopeIcon class="w-5 h-5 text-slate-500" /></template>
          </StatCard>
          <StatCard label="Sent" :value="detailStats.sent ?? 0" icon-class="bg-green-100">
            <template #icon><CheckCircleIcon class="w-5 h-5 text-green-600" /></template>
          </StatCard>
          <StatCard label="Opened" :value="detailStats.opened ?? 0" icon-class="bg-blue-100">
            <template #icon><EnvelopeIcon class="w-5 h-5 text-blue-600" /></template>
          </StatCard>
          <StatCard label="Failed" :value="detailStats.failed ?? 0" icon-class="bg-red-100">
            <template #icon><XCircleIcon class="w-5 h-5 text-red-600" /></template>
          </StatCard>
        </div>
        <div v-if="detailStats && detailStats.sent > 0" class="text-sm text-slate-500">
          Open rate: <span class="font-semibold text-slate-700">{{ openRate }}</span>
        </div>
        <div class="flex justify-end pt-2">
          <BaseButton variant="ghost" @click="showDetailModal = false">Close</BaseButton>
        </div>
      </div>
    </BaseModal>
  </div>
</template>
