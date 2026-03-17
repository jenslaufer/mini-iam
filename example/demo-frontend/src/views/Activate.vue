<template>
  <div class="max-w-sm mx-auto py-16">
    <div class="text-center mb-6">
      <h1 class="text-2xl font-bold text-gray-800">Set Your Password</h1>
      <p class="text-gray-500 text-sm mt-1">Complete your account setup.</p>
    </div>

    <form v-if="!activated" @submit.prevent="submit" class="bg-white rounded-lg shadow p-6 space-y-4">
      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Password</label>
        <input v-model="password" type="password" required minlength="8" placeholder="Min. 8 characters"
          class="w-full border rounded px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
      </div>
      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Confirm password</label>
        <input v-model="confirm" type="password" required minlength="8" placeholder="Repeat password"
          class="w-full border rounded px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
      </div>
      <p v-if="error" class="text-red-600 text-sm">{{ error }}</p>
      <button class="w-full bg-blue-600 text-white rounded py-2 text-sm font-medium hover:bg-blue-700">
        Activate Account
      </button>
    </form>

    <div v-else class="bg-white rounded-lg shadow p-6 text-center">
      <div class="text-green-600 text-4xl mb-3">✓</div>
      <h2 class="text-lg font-semibold text-gray-800 mb-2">Account activated!</h2>
      <p class="text-gray-500 text-sm mb-4">You can now login with your email and password.</p>
      <router-link to="/login" class="text-blue-600 hover:underline text-sm font-medium">Go to Login</router-link>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../stores/auth.js'

const route = useRoute()
const password = ref('')
const confirm = ref('')
const error = ref('')
const activated = ref(false)

async function submit() {
  error.value = ''
  if (password.value !== confirm.value) {
    error.value = 'Passwords do not match'
    return
  }
  try {
    await api(`/activate/${route.params.token}`, {
      method: 'POST',
      body: { password: password.value },
    })
    activated.value = true
  } catch (e) { error.value = e.message }
}
</script>
