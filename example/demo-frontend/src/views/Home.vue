<template>
  <div class="text-center py-16">
    <h1 class="text-3xl font-bold text-gray-800 mb-4">Launch Kit Demo</h1>
    <p class="text-gray-600 mb-8">Vue 3 frontend + FastAPI backend. Auth via Launch Kit (swappable IAM).</p>
    <div v-if="stats" class="inline-block bg-white rounded-lg shadow px-8 py-5 text-left text-sm space-y-1">
      <p><span class="font-medium">Active users:</span> {{ stats.users }}</p>
      <p><span class="font-medium">Total notes:</span> {{ stats.notes }}</p>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../stores/auth.js'

const stats = ref(null)
onMounted(async () => { stats.value = await api('/stats') })
</script>
