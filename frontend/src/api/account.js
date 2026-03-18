import apiClient from './client.js'

export const changePassword = (currentPassword, newPassword) =>
  apiClient
    .post('/password', {
      current_password: currentPassword,
      new_password: newPassword,
    })
    .then((r) => r.data)
