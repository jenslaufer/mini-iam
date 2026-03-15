import apiClient from './client.js'

export const listSegments = () => apiClient.get('/admin/segments').then((r) => r.data)
export const getSegment = (id) => apiClient.get(`/admin/segments/${id}`).then((r) => r.data)
export const createSegment = (body) => apiClient.post('/admin/segments', body).then((r) => r.data)
export const updateSegment = (id, body) => apiClient.put(`/admin/segments/${id}`, body).then((r) => r.data)
export const deleteSegment = (id) => apiClient.delete(`/admin/segments/${id}`)
export const addContactToSegment = (id, contact_id) =>
  apiClient.post(`/admin/segments/${id}/contacts`, { contact_id }).then((r) => r.data)
export const removeContactFromSegment = (id, contact_id) =>
  apiClient.delete(`/admin/segments/${id}/contacts/${contact_id}`)
