import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { getTenants } from '../api/tenants.js'

export const useTenantStore = defineStore('tenant', () => {
  const tenants = ref([])
  const selectedSlug = ref(localStorage.getItem('selected_tenant') || '')
  const platformTenantId = ref('')
  const isPlatformAdmin = ref(false)

  const currentSlug = computed(() => selectedSlug.value)

  async function loadTenants() {
    try {
      tenants.value = (await getTenants()) || []
    } catch {
      tenants.value = []
    }
  }

  function select(slug) {
    selectedSlug.value = slug
    localStorage.setItem('selected_tenant', slug)
  }

  function reset() {
    tenants.value = []
    selectedSlug.value = ''
    isPlatformAdmin.value = false
    localStorage.removeItem('selected_tenant')
  }

  return { tenants, selectedSlug, platformTenantId, isPlatformAdmin, currentSlug, loadTenants, select, reset }
})
