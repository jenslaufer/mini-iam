<template>
  <div class="max-w-sm mx-auto">
    <h2 class="text-2xl font-bold mb-6">Register</h2>
    <form @submit.prevent="submit" novalidate class="space-y-4">
      <input v-model="name" placeholder="Name" required class="w-full border rounded px-3 py-2 text-sm" />
      <input v-model="email" type="email" placeholder="Email" required class="w-full border rounded px-3 py-2 text-sm" />
      <input v-model="password" type="password" placeholder="Password" class="w-full border rounded px-3 py-2 text-sm" />
      <p v-if="error" class="text-red-600 text-sm">{{ error }}</p>
      <button class="w-full bg-blue-600 text-white rounded py-2 text-sm font-medium hover:bg-blue-700">Register</button>
    </form>
    <p class="mt-4 text-sm text-gray-500">Have an account? <router-link to="/login" class="text-blue-600 hover:underline">Login</router-link></p>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { auth, iam, endpoints } from '../stores/auth.js'

const router = useRouter()
const name = ref('')
const email = ref('')
const password = ref('')
const error = ref('')

const clientId = import.meta.env.VITE_OIDC_CLIENT_ID || ''

async function submit() {
  if (password.value.length < 8) {
    error.value = 'Password must be at least 8 characters'
    return
  }
  try {
    const regBody = { email: email.value, password: password.value, name: name.value }
    if (clientId) regBody.client_id = clientId
    await iam(endpoints.register, { method: 'POST', body: regBody })
    const loginBody = { email: email.value, password: password.value }
    if (clientId) loginBody.client_id = clientId
    const tokens = await iam(endpoints.login, { method: 'POST', body: loginBody })
    auth.setToken(tokens.access_token)
    router.push('/dashboard')
  } catch (e) { error.value = e.message }
}
</script>
