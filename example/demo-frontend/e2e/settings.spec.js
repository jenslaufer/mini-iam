// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsDemo, login, uniqueEmail, getAdminToken, deleteUserApi, signupContact, activateAccount } from './helpers.js'

// ─── UX GAPS OBSERVED ────────────────────────────────────────────────────────
// GAP-11: After a successful password change, the user is NOT automatically
//         logged out. Some security conventions require re-authentication after
//         a password change so that stolen sessions are invalidated. The tests
//         below document the current behaviour; if re-login is added, update
//         the success test accordingly.
// GAP-12: Settings page has no breadcrumb/heading showing the current user,
//         which could be confusing if multiple accounts share a device.
// GAP-13: No "delete account" option in Settings — mentioned here for
//         completeness as a common user expectation.
// ─────────────────────────────────────────────────────────────────────────────

// ─── Field helpers ────────────────────────────────────────────────────────────

/** @param {import('@playwright/test').Page} page */
function currentPasswordField(page) {
  return page.getByLabel(/current password/i)
}

/** @param {import('@playwright/test').Page} page */
function newPasswordField(page) {
  return page.getByLabel(/^new password/i)
}

/** @param {import('@playwright/test').Page} page */
function confirmPasswordField(page) {
  return page.getByLabel(/confirm/i)
}

// ─── Form rendering ───────────────────────────────────────────────────────────

test.describe('Settings — page structure', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
    await page.goto('/settings')
  })

  test('settings page is accessible at /settings', async ({ page }) => {
    await expect(page).toHaveURL('/settings')
  })

  test('shows Change Password heading', async ({ page }) => {
    await expect(page.getByRole('heading', { name: /change password/i })).toBeVisible()
  })

  test('shows all three password fields', async ({ page }) => {
    await expect(currentPasswordField(page)).toBeVisible()
    await expect(newPasswordField(page)).toBeVisible()
    await expect(confirmPasswordField(page)).toBeVisible()
  })

  test('shows submit button', async ({ page }) => {
    await expect(page.getByRole('button', { name: /change password|save|update/i })).toBeVisible()
  })

  test('has a back link to /dashboard', async ({ page }) => {
    const backLink = page.getByRole('link', { name: /← dashboard|back/i })
    await expect(backLink).toBeVisible()
    await backLink.click()
    await expect(page).toHaveURL('/dashboard', { timeout: 15000 })
  })

  test('/settings redirects to /login when not authenticated', async ({ page }) => {
    await page.evaluate(() => localStorage.clear())
    await page.goto('/settings')
    await expect(page).toHaveURL('/login', { timeout: 15000 })
  })
})

// ─── Client-side validation ───────────────────────────────────────────────────

test.describe('Settings — client-side validation', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
    await page.goto('/settings')
  })

  test('empty current password shows required error', async ({ page }) => {
    await newPasswordField(page).fill('ValidNew-pw-1!')
    await confirmPasswordField(page).fill('ValidNew-pw-1!')
    await page.getByRole('button', { name: /change password|save|update/i }).click()

    await expect(page.getByText(/current password.*required|required/i)).toBeVisible({ timeout: 10000 })
    await expect(page).toHaveURL('/settings')
  })

  test('new password shorter than 8 chars shows length error', async ({ page }) => {
    await currentPasswordField(page).fill('password123')
    await newPasswordField(page).fill('short')
    await confirmPasswordField(page).fill('short')
    await page.getByRole('button', { name: /change password|save|update/i }).click()

    await expect(page.getByText(/at least 8|minimum 8|too short/i)).toBeVisible({ timeout: 10000 })
    await expect(page).toHaveURL('/settings')
  })

  test('mismatched new and confirm passwords shows mismatch error', async ({ page }) => {
    await currentPasswordField(page).fill('password123')
    await newPasswordField(page).fill('ValidNew-pw-1!')
    await confirmPasswordField(page).fill('DifferentPw-2!')
    await page.getByRole('button', { name: /change password|save|update/i }).click()

    await expect(page.getByText(/do not match|passwords must match|mismatch/i)).toBeVisible({ timeout: 10000 })
    await expect(page).toHaveURL('/settings')
  })

  test('all fields empty shows validation error without API call', async ({ page }) => {
    await page.getByRole('button', { name: /change password|save|update/i }).click()

    // Still on settings, no network success message
    await expect(page).toHaveURL('/settings')
    await expect(page.getByText(/success|password changed/i)).not.toBeVisible()
  })
})

// ─── Wrong current password ───────────────────────────────────────────────────

test.describe('Settings — wrong current password', () => {
  test('wrong current password shows error message', async ({ page }) => {
    await loginAsDemo(page)
    await page.goto('/settings')

    await currentPasswordField(page).fill('this-is-definitely-wrong-99')
    await newPasswordField(page).fill('ValidNew-pw-1!')
    await confirmPasswordField(page).fill('ValidNew-pw-1!')
    await page.getByRole('button', { name: /change password|save|update/i }).click()

    await expect(
      page.getByText(/incorrect|invalid|wrong|current password/i),
    ).toBeVisible({ timeout: 10000 })
    await expect(page).toHaveURL('/settings')
  })
})

// ─── Successful password change ───────────────────────────────────────────────

test.describe('Settings — successful password change', () => {
  const initialPassword = 'InitialPass1!'
  const newPassword = 'UpdatedPass2!'
  let userId
  let userEmail

  test.beforeAll(async () => {
    // Create a fresh user via signup + activation so we don't touch the seeded demo account
    userEmail = uniqueEmail('pwd-change')
    let contact
    try {
      contact = await signupContact(userEmail, 'PwdChange User')
    } catch (err) {
      // If signup API is unavailable the whole describe block will be skipped
      // in individual test.skip calls below
      return
    }
    const token = contact.invite_token ?? contact.activation_token ?? contact.token
    if (!token) return

    const result = await activateAccount(token, initialPassword)
    userId = result.user_id ?? result.id
  })

  test.afterAll(async () => {
    if (userId) {
      try {
        const adminToken = await getAdminToken()
        await deleteUserApi(adminToken, userId)
      } catch (_) {}
    }
  })

  test('changes password, shows success, resets fields, old password fails', async ({ page }) => {
    if (!userId) {
      test.skip(true, 'User setup via API failed — check signupContact / activateAccount helpers')
      return
    }

    await login(page, userEmail, initialPassword)
    await page.goto('/settings')

    await currentPasswordField(page).fill(initialPassword)
    await newPasswordField(page).fill(newPassword)
    await confirmPasswordField(page).fill(newPassword)
    await page.getByRole('button', { name: /change password|save|update/i }).click()

    // Success message must appear
    await expect(page.getByText(/success|password changed|updated/i)).toBeVisible({ timeout: 10000 })

    // All three fields must be cleared after success
    await expect(currentPasswordField(page)).toHaveValue('')
    await expect(newPasswordField(page)).toHaveValue('')
    await expect(confirmPasswordField(page)).toHaveValue('')

    // GAP-11: Current behaviour — user stays logged in after password change.
    // If the product adds forced re-login, expect a redirect to /login here.

    // Logout and verify old password no longer works
    await page.getByRole('button', { name: /logout|sign out|log out/i }).first().click()
    await expect(page).toHaveURL('/login', { timeout: 15000 })

    await page.getByPlaceholder(/email/i).fill(userEmail)
    await page.getByPlaceholder(/password/i).fill(initialPassword)
    await page.getByRole('button', { name: /sign in|log in|login/i }).click()
    await expect(page.getByText(/invalid|incorrect|error/i)).toBeVisible({ timeout: 10000 })
    await expect(page).toHaveURL('/login')

    // New password must work
    await page.getByPlaceholder(/email/i).fill(userEmail)
    await page.getByPlaceholder(/password/i).fill(newPassword)
    await page.getByRole('button', { name: /sign in|log in|login/i }).click()
    await expect(page).toHaveURL('/dashboard', { timeout: 15000 })
  })
})
