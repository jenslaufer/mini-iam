import apiClient from './client.js'

export const getClients = () => apiClient.get('/admin/clients').then((r) => r.data)
export const createClient = (body) => apiClient.post('/clients', body).then((r) => r.data)
export const updateClient = (id, body) => apiClient.put(`/admin/clients/${id}`, body).then((r) => r.data)
export const deleteClient = (id) => apiClient.delete(`/admin/clients/${id}`)
