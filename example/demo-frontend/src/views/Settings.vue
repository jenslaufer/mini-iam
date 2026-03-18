<template>
  <div>
    <div class="flex justify-between items-center mb-6">
      <h2 class="text-2xl font-bold">Settings</h2>
      <router-link to="/dashboard" class="text-blue-600 text-sm hover:underline">← Dashboard</router-link>
    </div>

    <div class="bg-white rounded-lg shadow p-6 max-w-md">
      <h3 class="text-lg font-semibold mb-4">Change Password</h3>

      <form @submit.prevent="submit" novalidate class="space-y-4">
        <div>
          <label for="current-password" class="block text-sm font-medium text-gray-700 mb-1">Current Password</label>
          <input id="current-password" v-model="current" type="password" required
            class="w-full border rounded px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
          <p v-if="errors.current" class="text-red-600 text-xs mt-1">{{ errors.current }}</p>
        </div>
        <div>
          <label for="new-password" class="block text-sm font-medium text-gray-700 mb-1">New Password</label>
          <input id="new-password" v-model="newPw" type="password" required
            class="w-full border rounded px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
          <p v-if="errors.newPw" class="text-red-600 text-xs mt-1">{{ errors.newPw }}</p>
        </div>
        <div>
          <label for="confirm-password" class="block text-sm font-medium text-gray-700 mb-1">Confirm New Password</label>
          <input id="confirm-password" v-model="confirm" type="password" required
            class="w-full border rounded px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
          <p v-if="errors.confirm" class="text-red-600 text-xs mt-1">{{ errors.confirm }}</p>
        </div>

        <p v-if="success" class="text-green-600 text-sm">Password changed successfully.</p>
        <p v-if="apiError" class="text-red-600 text-sm">{{ apiError }}</p>

        <button :disabled="loading"
          class="w-full bg-blue-600 text-white rounded py-2 text-sm font-medium hover:bg-blue-700 disabled:opacity-50">
          {{ loading ? 'Changing…' : 'Change Password' }}
        </button>
      </form>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { auth, iam, endpoints } from '../stores/auth.js'

const current = ref('')
const newPw = ref('')
const confirm = ref('')
const errors = ref({})
const loading = ref(false)
const success = ref(false)
const apiError = ref('')

function validate() {
  const e = {}
  if (!current.value) e.current = 'Current password is required'
  if (newPw.value.length < 8) e.newPw = 'Password must be at least 8 characters'
  if (newPw.value !== confirm.value) e.confirm = 'Passwords do not match'
  errors.value = e
  return Object.keys(e).length === 0
}

async function submit() {
  success.value = false
  apiError.value = ''
  if (!validate()) return

  loading.value = true
  try {
    const OIDC_ISSUER_URL = (import.meta.env.VITE_OIDC_ISSUER_URL || '/auth').replace(/\/$/, '')
    await iam(`${OIDC_ISSUER_URL}/password`, {
      method: 'POST',
      body: { current_password: current.value, new_password: newPw.value },
    })
    success.value = true
    current.value = ''
    newPw.value = ''
    confirm.value = ''
  } catch (e) {
    apiError.value = e.message
  } finally {
    loading.value = false
  }
}
</script>
