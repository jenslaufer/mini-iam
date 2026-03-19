<script setup>
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import BaseButton from '../components/BaseButton.vue'
import BaseInput from '../components/BaseInput.vue'
import { getTenant, updateTenant } from '../api/tenants.js'
import { useToastStore } from '../stores/toast.js'

const route = useRoute()
const toast = useToastStore()

const loading = ref(true)
const saving = ref(false)
const tenant = ref(null)

const form = ref({
  name: '',
  registration_enabled: false,
  smtp_host: '',
  smtp_port: '',
  smtp_user: '',
  smtp_password: '',
  smtp_from: '',
  smtp_from_name: '',
  smtp_rate_ms: '',
})

const passwordChanged = ref(false)
const hasPassword = ref(false)

onMounted(async () => {
  try {
    const data = await getTenant(route.params.id)
    tenant.value = data
    form.value = {
      name: data.name,
      registration_enabled: data.registration_enabled,
      smtp_host: data.smtp?.smtp_host || '',
      smtp_port: data.smtp?.smtp_port || '',
      smtp_user: data.smtp?.smtp_user || '',
      smtp_password: '',
      smtp_from: data.smtp?.smtp_from || '',
      smtp_from_name: data.smtp?.smtp_from_name || '',
      smtp_rate_ms: data.smtp?.smtp_rate_ms ? String(data.smtp.smtp_rate_ms) : '',
    }
    // If any SMTP field is set, the password is configured
    hasPassword.value = !!(data.smtp?.smtp_host || data.smtp?.smtp_user)
  } catch {
    toast.add('error', 'Failed to load tenant')
  } finally {
    loading.value = false
  }
})

function onPasswordInput(val) {
  form.value.smtp_password = val
  passwordChanged.value = true
}

async function save() {
  saving.value = true
  try {
    const body = {
      name: form.value.name,
      registration_enabled: form.value.registration_enabled,
      smtp: {
        host: form.value.smtp_host,
        port: form.value.smtp_port,
        user: form.value.smtp_user,
        from: form.value.smtp_from,
        from_name: form.value.smtp_from_name,
        rate_ms: parseInt(form.value.smtp_rate_ms) || 0,
      },
    }
    // Only send password if user actually changed it
    if (passwordChanged.value) {
      body.smtp.password = form.value.smtp_password
    }

    const updated = await updateTenant(route.params.id, body)
    tenant.value = updated
    hasPassword.value = !!(updated.smtp?.smtp_host || updated.smtp?.smtp_user)
    passwordChanged.value = false
    form.value.smtp_password = ''
    toast.add('success', 'Tenant saved')
  } catch (e) {
    toast.add('error', e.response?.data?.error_description || 'Failed to save')
  } finally {
    saving.value = false
  }
}

function formatDate(iso) {
  return new Date(iso).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}
</script>

<template>
  <div class="space-y-6 max-w-2xl">
    <!-- Back link -->
    <router-link to="/tenants" class="inline-flex items-center gap-1 text-sm text-slate-500 hover:text-slate-700 transition-colors">
      &larr; Back to Tenants
    </router-link>

    <!-- Loading skeleton -->
    <div v-if="loading" class="space-y-4">
      <div class="h-8 bg-slate-200 rounded animate-pulse w-48"></div>
      <div class="h-4 bg-slate-200 rounded animate-pulse w-32"></div>
      <div class="h-10 bg-slate-200 rounded animate-pulse w-full"></div>
    </div>

    <template v-else-if="tenant">
      <!-- General Settings -->
      <div class="bg-white rounded-xl shadow-sm border border-slate-200 p-6 space-y-4">
        <h2 class="text-sm font-medium text-slate-900 uppercase tracking-wide">General Settings</h2>

        <BaseInput label="Name" v-model="form.name" placeholder="Tenant name" />

        <div class="flex flex-col gap-1">
          <span class="text-sm font-medium text-slate-700">Slug</span>
          <code class="font-mono text-sm bg-slate-100 px-3 py-2 rounded-lg text-slate-700">{{ tenant.slug }}</code>
        </div>

        <div class="flex items-center gap-3">
          <label class="relative inline-flex items-center cursor-pointer">
            <input
              type="checkbox"
              v-model="form.registration_enabled"
              class="sr-only peer"
              role="checkbox"
              :aria-label="'Registration enabled'"
            />
            <div class="w-9 h-5 bg-slate-200 peer-focus:ring-2 peer-focus:ring-blue-500 rounded-full peer peer-checked:bg-blue-600 transition-colors after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:after:translate-x-full"></div>
          </label>
          <span class="text-sm text-slate-700">Registration enabled</span>
        </div>

        <div class="flex flex-col gap-1">
          <span class="text-sm font-medium text-slate-700">Created</span>
          <span class="text-sm text-slate-500">{{ formatDate(tenant.created_at) }}</span>
        </div>
      </div>

      <!-- SMTP Configuration -->
      <div class="bg-white rounded-xl shadow-sm border border-slate-200 p-6 space-y-4">
        <h2 class="text-sm font-medium text-slate-900 uppercase tracking-wide">SMTP Configuration</h2>

        <div class="grid grid-cols-2 gap-4">
          <BaseInput label="Host" v-model="form.smtp_host" placeholder="smtp.example.com" />
          <BaseInput label="Port" v-model="form.smtp_port" placeholder="587" />
        </div>

        <BaseInput label="User" v-model="form.smtp_user" placeholder="user@example.com" />

        <BaseInput
          label="Password"
          type="password"
          :model-value="form.smtp_password"
          :placeholder="hasPassword ? '••••••••' : ''"
          @update:model-value="onPasswordInput"
        />

        <div class="grid grid-cols-2 gap-4">
          <BaseInput label="From" v-model="form.smtp_from" placeholder="noreply@example.com" />
          <BaseInput label="From Name" v-model="form.smtp_from_name" placeholder="My App" />
        </div>

        <BaseInput label="Rate (ms)" v-model="form.smtp_rate_ms" placeholder="100" />
      </div>

      <!-- Save -->
      <div class="flex justify-end">
        <BaseButton @click="save" :loading="saving">Save</BaseButton>
      </div>
    </template>
  </div>
</template>
