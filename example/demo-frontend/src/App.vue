<template>
  <div class="min-h-screen">
    <nav class="bg-white shadow">
      <div class="max-w-4xl mx-auto px-4 py-3 flex items-center justify-between">
        <router-link to="/" class="font-bold text-lg text-gray-800">Launch Kit Demo</router-link>
        <div class="flex gap-4 text-sm">
          <template v-if="auth.token">
            <router-link to="/dashboard" class="text-blue-600 hover:underline">Dashboard</router-link>
            <button @click="logout" class="text-gray-500 hover:underline">Logout</button>
          </template>
          <template v-else>
            <router-link to="/login" class="text-blue-600 hover:underline">Login</router-link>
            <router-link to="/register" class="text-blue-600 hover:underline">Register</router-link>
          </template>
        </div>
      </div>
    </nav>
    <main class="max-w-4xl mx-auto px-4 py-8">
      <router-view />
    </main>
  </div>
</template>

<script setup>
import { auth } from './stores/auth.js'
import { useRouter } from 'vue-router'

const router = useRouter()

function logout() {
  auth.logout()
  router.push('/login')
}
</script>
