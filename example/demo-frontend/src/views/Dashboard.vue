<template>
  <div>
    <div class="flex justify-between items-center mb-6">
      <h2 class="text-2xl font-bold">Dashboard</h2>
      <div class="flex gap-3 text-sm">
        <router-link to="/settings" class="text-blue-600 hover:underline">Settings</router-link>
        <button @click="logout" class="text-gray-500 hover:underline">Logout</button>
      </div>
    </div>

    <div v-if="dash" class="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-8">
      <div class="bg-white rounded-lg shadow p-5">
        <p class="text-sm text-gray-500">Logged in as</p>
        <p class="text-lg font-semibold">{{ dash.email }}</p>
      </div>
      <div class="bg-white rounded-lg shadow p-5">
        <p class="text-sm text-gray-500">Your notes</p>
        <p class="text-lg font-semibold">{{ dash.your_notes }}</p>
      </div>
      <div class="bg-white rounded-lg shadow p-5">
        <p class="text-sm text-gray-500">Platform</p>
        <p class="text-lg font-semibold">{{ dash.total_users }} users &middot; {{ dash.total_notes }} notes</p>
      </div>
    </div>

    <form @submit.prevent="addNote" class="flex gap-2 mb-4">
      <input v-model="title" placeholder="Note title" required class="flex-1 border rounded px-3 py-2 text-sm" />
      <input v-model="body" placeholder="Body (optional)" class="flex-1 border rounded px-3 py-2 text-sm" />
      <button class="bg-blue-600 text-white rounded px-4 py-2 text-sm hover:bg-blue-700">Add</button>
    </form>

    <ul class="space-y-2">
      <li v-for="note in notes" :key="note.id" class="bg-white rounded shadow px-4 py-3 flex justify-between items-center">
        <div>
          <span class="font-medium">{{ note.title }}</span>
          <span v-if="note.body" class="text-gray-500 text-sm ml-2">— {{ note.body }}</span>
        </div>
        <button @click="removeNote(note.id)" class="text-red-500 text-sm hover:underline">Delete</button>
      </li>
    </ul>
    <p v-if="notes && !notes.length" class="text-gray-400 text-sm">No notes yet. Add one above.</p>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { api, auth } from '../stores/auth.js'

function logout() { auth.logout() }

const dash = ref(null)
const notes = ref([])
const title = ref('')
const body = ref('')

async function load() {
  ;[dash.value, notes.value] = await Promise.all([api('/dashboard'), api('/notes')])
}

async function addNote() {
  await api('/notes', { method: 'POST', body: { title: title.value, body: body.value } })
  title.value = ''
  body.value = ''
  await load()
}

async function removeNote(id) {
  await api(`/notes/${id}`, { method: 'DELETE' })
  await load()
}

onMounted(load)
</script>
