import apiClient from './client.js'

export const changePassword = (currentPassword, newPassword, confirmPassword) =>
  apiClient
    .post('/password', {
      current_password: currentPassword,
      new_password: newPassword,
      confirm_password: confirmPassword,
    })
    .then((r) => r.data)
