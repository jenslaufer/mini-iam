<script setup>
import { ref } from 'vue'
import BaseInput from '../components/BaseInput.vue'
import BaseButton from '../components/BaseButton.vue'
import { changePassword } from '../api/account.js'
import { useToastStore } from '../stores/toast.js'

const toast = useToastStore()

const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const loading = ref(false)
const errors = ref({})

function validate() {
  const e = {}
  if (!currentPassword.value) e.currentPassword = 'Current password is required'
  if (newPassword.value.length < 8) e.newPassword = 'Password must be at least 8 characters'
  if (newPassword.value !== confirmPassword.value) e.confirmPassword = 'Passwords do not match'
  errors.value = e
  return Object.keys(e).length === 0
}

async function submit() {
  if (!validate()) return
  loading.value = true
  errors.value = {}
  try {
    await changePassword(currentPassword.value, newPassword.value)
    toast.add('success', 'Password changed successfully')
    currentPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
  } catch (err) {
    const msg = err.response?.data?.error_description || 'Failed to change password'
    toast.add('error', msg)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="max-w-lg">
    <h1 class="text-2xl font-semibold text-slate-900 mb-6">Settings</h1>

    <div class="bg-white rounded-xl border border-slate-200 p-6">
      <h2 class="text-lg font-medium text-slate-900 mb-4">Change Password</h2>

      <form class="space-y-4" novalidate @submit.prevent="submit">
        <BaseInput
          label="Current Password"
          type="password"
          v-model="currentPassword"
          :error="errors.currentPassword"
          required
          :disabled="loading"
        />
        <BaseInput
          label="New Password"
          type="password"
          v-model="newPassword"
          :error="errors.newPassword"
          required
          :disabled="loading"
        />
        <BaseInput
          label="Confirm New Password"
          type="password"
          v-model="confirmPassword"
          :error="errors.confirmPassword"
          required
          :disabled="loading"
        />
        <div class="pt-2">
          <BaseButton type="submit" :loading="loading" :disabled="loading">
            Change Password
          </BaseButton>
        </div>
      </form>
    </div>
  </div>
</template>
