<template>
  <div class="max-w-sm mx-auto py-16">
    <div class="text-center mb-6">
      <h1 class="text-2xl font-bold text-gray-800">Set Your Password</h1>
      <p class="text-gray-500 text-sm mt-1">Complete your account setup.</p>
    </div>

    <div v-if="tokenError" class="bg-white rounded-lg shadow p-6 text-center">
      <div class="text-red-500 text-4xl mb-3">!</div>
      <h2 class="text-lg font-semibold text-gray-800 mb-2">Invalid or expired link</h2>
      <p class="text-gray-500 text-sm mb-4">This activation link is not valid. Please request a new invite.</p>
      <router-link to="/" class="text-blue-600 hover:underline text-sm font-medium">Back to Home</router-link>
    </div>

    <form v-else-if="!activated" @submit.prevent="submit" class="bg-white rounded-lg shadow p-6 space-y-4">
      <div>
        <label for="password" class="block text-sm font-medium text-gray-700 mb-1">Password</label>
        <input id="password" v-model="password" type="password" required placeholder="Min. 8 characters"
          class="w-full border rounded px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
      </div>
      <div>
        <label for="confirm-password" class="block text-sm font-medium text-gray-700 mb-1">Confirm password</label>
        <input id="confirm-password" v-model="confirm" type="password" required placeholder="Repeat password"
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
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../stores/auth.js'

const route = useRoute()
const password = ref('')
const confirm = ref('')
const error = ref('')
const activated = ref(false)
const tokenError = ref(false)

onMounted(async () => {
  // Validate token format — reject obviously bad tokens
  const token = route.params.token
  if (!token || token.length < 10) {
    tokenError.value = true
    return
  }
  // Optionally validate against backend (HEAD request or similar)
  // For now we accept any plausible-looking token and let the backend reject on submit
})

async function submit() {
  error.value = ''
  if (password.value.length < 8) {
    error.value = 'Password must be at least 8 characters'
    return
  }
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
