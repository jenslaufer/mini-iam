<template>
  <div class="max-w-md mx-auto py-16">
    <div class="text-center mb-10">
      <h1 class="text-3xl font-bold text-gray-800 mb-2">Launch Kit Demo</h1>
      <p class="text-gray-500">Sign up to get early access. We'll send you an invite when your account is ready.</p>
    </div>

    <!-- Subscribe form -->
    <form v-if="!subscribed" @submit.prevent="subscribe" class="bg-white rounded-lg shadow p-6 space-y-4">
      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Name</label>
        <input v-model="name" required placeholder="Your name"
          class="w-full border rounded px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
      </div>
      <div>
        <label class="block text-sm font-medium text-gray-700 mb-1">Email</label>
        <input v-model="email" type="email" required placeholder="you@example.com"
          class="w-full border rounded px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500" />
      </div>
      <p v-if="error" class="text-red-600 text-sm">{{ error }}</p>
      <button class="w-full bg-blue-600 text-white rounded py-2 text-sm font-medium hover:bg-blue-700">
        Get Early Access
      </button>
      <p class="text-xs text-gray-400 text-center">Already have an account? <router-link to="/login" class="text-blue-600 hover:underline">Login</router-link></p>
    </form>

    <!-- Success state -->
    <div v-else class="bg-white rounded-lg shadow p-6 text-center">
      <div class="text-green-600 text-4xl mb-3">✓</div>
      <h2 class="text-lg font-semibold text-gray-800 mb-2">You're on the list!</h2>
      <p class="text-gray-500 text-sm mb-4">We'll email <strong>{{ subscribedEmail }}</strong> with an invite link to set up your account.</p>
      <div v-if="inviteToken" class="bg-blue-50 border border-blue-200 rounded p-3">
        <p class="text-xs text-blue-600 font-medium mb-1">Demo: In production this link comes via email</p>
        <router-link :to="`/activate/${inviteToken}`" class="text-blue-700 text-sm font-medium hover:underline">
          Activate your account now →
        </router-link>
      </div>
    </div>

    <!-- Stats -->
    <div v-if="stats" class="mt-8 text-center text-sm text-gray-400">
      {{ stats.users }} users &middot; {{ stats.notes }} notes
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../stores/auth.js'

const name = ref('')
const email = ref('')
const error = ref('')
const subscribed = ref(false)
const subscribedEmail = ref('')
const inviteToken = ref('')
const stats = ref(null)

onMounted(async () => {
  try { stats.value = await api('/stats') } catch {}
})

async function subscribe() {
  error.value = ''
  try {
    const data = await api('/subscribe', { method: 'POST', body: { email: email.value, name: name.value } })
    subscribedEmail.value = email.value
    inviteToken.value = data.invite_token || ''
    subscribed.value = true
  } catch (e) { error.value = e.message }
}
</script>
