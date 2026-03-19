import apiClient from './client.js'

export const listContacts = () => apiClient.get('/admin/contacts').then((r) => r.data)
export const createContact = (body) => apiClient.post('/admin/contacts', body).then((r) => r.data)
export const updateContact = (id, body) => apiClient.put(`/admin/contacts/${id}`, body).then((r) => r.data)
export const deleteContact = (id) => apiClient.delete(`/admin/contacts/${id}`)
export const importContacts = (body) => apiClient.post('/admin/contacts/import', body).then((r) => r.data)
