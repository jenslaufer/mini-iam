import apiClient from './client.js'

export const getUsers = () => apiClient.get('/admin/users').then((r) => r.data)
export const getUser = (id) => apiClient.get(`/admin/users/${id}`).then((r) => r.data)
export const updateUser = (id, body) => apiClient.put(`/admin/users/${id}`, body).then((r) => r.data)
export const deleteUser = (id) => apiClient.delete(`/admin/users/${id}`)
