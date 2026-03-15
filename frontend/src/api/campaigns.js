import apiClient from './client.js'

export const listCampaigns = () => apiClient.get('/admin/campaigns').then((r) => r.data)
export const getCampaign = (id) => apiClient.get(`/admin/campaigns/${id}`).then((r) => r.data)
export const getCampaignStats = (id) => apiClient.get(`/admin/campaigns/${id}/stats`).then((r) => r.data)
export const createCampaign = (body) => apiClient.post('/admin/campaigns', body).then((r) => r.data)
export const updateCampaign = (id, body) => apiClient.put(`/admin/campaigns/${id}`, body).then((r) => r.data)
export const deleteCampaign = (id) => apiClient.delete(`/admin/campaigns/${id}`)
export const sendCampaign = (id) => apiClient.post(`/admin/campaigns/${id}/send`).then((r) => r.data)
