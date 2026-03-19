import apiClient from './client.js'

export const getTenants = () => apiClient.get('/admin/tenants').then((r) => r.data)
export const getTenant = (id) => apiClient.get(`/admin/tenants/${id}`).then((r) => r.data)
export const updateTenant = (id, data) => apiClient.put(`/admin/tenants/${id}`, data).then((r) => r.data)
export const deleteTenant = (id) => apiClient.delete(`/admin/tenants/${id}`)
export const exportTenant = (id) => apiClient.get(`/admin/tenants/${id}/export`).then((r) => r.data)
export const importTenant = (data) => apiClient.post('/admin/tenants/import', data).then((r) => r.data)
