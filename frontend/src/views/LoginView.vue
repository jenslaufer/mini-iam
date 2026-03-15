<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth.js'
import BaseInput from '../components/BaseInput.vue'
import BaseButton from '../components/BaseButton.vue'

const router = useRouter()
const auth = useAuthStore()

const email = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)

async function submit() {
  error.value = ''
  loading.value = true
  try {
    await auth.login(email.value, password.value)
    router.push('/dashboard')
  } catch (e) {
    error.value =
      e.response?.data?.error_description ||
      e.message ||
      'Login failed'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen bg-slate-50 flex items-center justify-center p-4">
    <div class="bg-white rounded-xl shadow-sm border border-slate-200 w-full max-w-sm p-8">
      <!-- Brand -->
      <div class="flex flex-col items-center mb-8">
        <div class="w-10 h-10 bg-blue-600 rounded-lg mb-3"></div>
        <h1 class="text-xl font-bold text-slate-900 tracking-tight">mini-iam</h1>
        <p class="text-sm text-slate-500 mt-1">Identity &amp; Access Management</p>
      </div>

      <!-- Error -->
      <div
        v-if="error"
        class="mb-4 px-4 py-3 rounded-lg bg-red-50 border border-red-200 text-red-700 text-sm"
      >
        {{ error }}
      </div>

      <!-- Form -->
      <form @submit.prevent="submit" class="space-y-4">
        <BaseInput
          label="Email"
          type="email"
          v-model="email"
          placeholder="admin@example.com"
          required
          :disabled="loading"
        />
        <BaseInput
          label="Password"
          type="password"
          v-model="password"
          placeholder="••••••••"
          required
          :disabled="loading"
        />
        <BaseButton type="submit" :loading="loading" class="w-full justify-center">
          Sign in
        </BaseButton>
      </form>
    </div>
  </div>
</template>
