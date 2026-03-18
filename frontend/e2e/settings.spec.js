// @ts-check
import { test, expect } from '@playwright/test'
import {
  loginAsAdmin,
  registerUser,
  getAdminToken,
  deleteUserApi,
  changePasswordApi,
  promoteToAdmin,
} from './helpers.js'

const BASE_URL = 'http://localhost:3000'

/**
 * Log in as an arbitrary admin user and wait for the dashboard.
 * Unlike loginAsAdmin this accepts arbitrary credentials, which lets tests
 * log in as freshly-created admin users without touching the seeded account.
 * @param {import('@playwright/test').Page} page
 * @param {string} email
 * @param {string} password
 */
async function loginAs(page, email, password) {
  await page.goto('/login')
  await page.getByPlaceholder('Leave empty for platform admin').fill('')
  await page.getByPlaceholder('admin@example.com').fill(email)
  await page.getByPlaceholder('••••••••').fill(password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL('/dashboard', { timeout: 15000 })
}

function currentPasswordField(page) {
  return page.getByRole('textbox', { name: /^Current Password/ })
}

function newPasswordField(page) {
  return page.getByRole('textbox', { name: /^New Password/ })
}

function confirmPasswordField(page) {
  return page.getByRole('textbox', { name: /^Confirm New Password/ })
}

test.describe('Settings — password change form', () => {
  test('shows password change form with all fields and submit button', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/settings')

    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Change Password' })).toBeVisible()
    await expect(currentPasswordField(page)).toBeVisible()
    await expect(newPasswordField(page)).toBeVisible()
    await expect(confirmPasswordField(page)).toBeVisible()
    await expect(page.getByRole('button', { name: 'Change Password' })).toBeVisible()
  })

  test('settings link in sidebar navigates to /settings', async ({ page }) => {
    await loginAsAdmin(page)
    await page.locator('aside').getByText('Settings').click()
    await expect(page).toHaveURL('/settings')
  })
})

test.describe('Settings — successful password change', () => {
  let userId
  const email = `pwd-change-${Date.now()}@example.com`
  const initialPassword = 'Initial-pw-1'
  const newPassword = 'New-pw-2-updated'

  test.beforeAll(async () => {
    const adminToken = await getAdminToken(BASE_URL)
    const user = await registerUser(BASE_URL, email, initialPassword, 'Pwd Change User')
    userId = user.id
    await promoteToAdmin(BASE_URL, adminToken, userId)
  })

  test.afterAll(async () => {
    if (userId) {
      const adminToken = await getAdminToken(BASE_URL)
      await deleteUserApi(BASE_URL, adminToken, userId)
    }
  })

  test('changes password via UI, shows success toast, and new credentials work', async ({ page }) => {
    await loginAs(page, email, initialPassword)
    await page.goto('/settings')

    // Fill the form
    await currentPasswordField(page).fill(initialPassword)
    await newPasswordField(page).fill(newPassword)
    await confirmPasswordField(page).fill(newPassword)
    await page.getByRole('button', { name: 'Change Password' }).click()

    // Success toast
    await expect(page.getByText('Password changed successfully')).toBeVisible({ timeout: 10000 })

    // Fields reset after success
    await expect(currentPasswordField(page)).toHaveValue('')
    await expect(newPasswordField(page)).toHaveValue('')
    await expect(confirmPasswordField(page)).toHaveValue('')

    // Logout and verify old credentials no longer work
    await page.getByRole('button', { name: 'Sign out' }).click()
    await expect(page).toHaveURL('/login')

    await page.getByPlaceholder('admin@example.com').fill(email)
    await page.getByPlaceholder('••••••••').fill(initialPassword)
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page.locator('.bg-red-50')).toBeVisible()
    await expect(page).toHaveURL('/login')

    // New credentials work
    await page.getByPlaceholder('admin@example.com').fill(email)
    await page.getByPlaceholder('••••••••').fill(newPassword)
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page).toHaveURL('/dashboard', { timeout: 15000 })
  })
})

test.describe('Settings — error handling', () => {
  test('wrong current password shows error toast', async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/settings')

    await currentPasswordField(page).fill('definitelywrong99')
    await newPasswordField(page).fill('ValidNew-pw-1')
    await confirmPasswordField(page).fill('ValidNew-pw-1')
    await page.getByRole('button', { name: 'Change Password' }).click()

    await expect(page.getByText(/current password is incorrect/i)).toBeVisible({ timeout: 10000 })
    await expect(page).toHaveURL('/settings')
  })
})

test.describe('Settings — client-side validation', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page)
    await page.goto('/settings')
  })

  test('mismatched passwords show error under confirm field', async ({ page }) => {
    await currentPasswordField(page).fill('changeme')
    await newPasswordField(page).fill('ValidNew-pw-1')
    await confirmPasswordField(page).fill('DifferentPw-2')
    await page.getByRole('button', { name: 'Change Password' }).click()

    await expect(page.getByText('Passwords do not match')).toBeVisible()
    // No API call — URL stays on /settings
    await expect(page).toHaveURL('/settings')
  })

  test('password shorter than 8 chars shows error under new password field', async ({ page }) => {
    await currentPasswordField(page).fill('changeme')
    await newPasswordField(page).fill('short')
    await confirmPasswordField(page).fill('short')
    await page.getByRole('button', { name: 'Change Password' }).click()

    await expect(page.getByText('Password must be at least 8 characters')).toBeVisible()
    await expect(page).toHaveURL('/settings')
  })

  test('empty current password shows required error', async ({ page }) => {
    await newPasswordField(page).fill('ValidNew-pw-1')
    await confirmPasswordField(page).fill('ValidNew-pw-1')
    await page.getByRole('button', { name: 'Change Password' }).click()

    await expect(page.getByText('Current password is required')).toBeVisible()
    await expect(page).toHaveURL('/settings')
  })
})

test.describe('Settings — API boundary', () => {
  test('password exceeding 72 characters is rejected by the API with 400', async () => {
    const token = await getAdminToken(BASE_URL)
    const overlong = 'A'.repeat(73)

    const res = await changePasswordApi(BASE_URL, token, 'changeme', overlong)

    expect(res.status).toBe(400)
  })
})
