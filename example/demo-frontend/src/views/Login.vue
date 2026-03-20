<template>
  <div class="max-w-sm mx-auto">
    <h2 class="text-2xl font-bold mb-6">Login</h2>
    <form @submit.prevent="submit" class="space-y-4">
      <input v-model="email" type="email" placeholder="Email" required class="w-full border rounded px-3 py-2 text-sm" />
      <input v-model="password" type="password" placeholder="Password" required class="w-full border rounded px-3 py-2 text-sm" />
      <p v-if="error" class="text-red-600 text-sm">{{ error }}</p>
      <button class="w-full bg-blue-600 text-white rounded py-2 text-sm font-medium hover:bg-blue-700">Login</button>
    </form>
    <p class="mt-4 text-sm text-gray-500">No account? <router-link to="/register" class="text-blue-600 hover:underline">Register</router-link></p>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { auth, iam, endpoints } from '../stores/auth.js'

const router = useRouter()
const email = ref('')
const password = ref('')
const error = ref('')

const clientId = import.meta.env.VITE_OIDC_CLIENT_ID || ''

async function submit() {
  try {
    const body = { email: email.value, password: password.value }
    if (clientId) body.client_id = clientId
    const data = await iam(endpoints.login, { method: 'POST', body })
    auth.setToken(data.access_token)
    router.push('/dashboard')
  } catch (e) { error.value = e.message }
}
</script>
