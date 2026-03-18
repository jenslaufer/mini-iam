// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsDemo } from './helpers.js'

// ─── UX GAPS OBSERVED ────────────────────────────────────────────────────────
// GAP-5: After logout, navigating back with the browser Back button might
//        still show protected pages if the router does not re-check auth on
//        every render. Tests below verify the redirect guard holds after logout.
// GAP-6: The login form does not specify whether the error is "wrong password"
//        or "account not found" — both cases should show a generic error to
//        avoid user enumeration.
// ─────────────────────────────────────────────────────────────────────────────

test.describe('Login page', () => {
  test('shows login form at /login', async ({ page }) => {
    await page.goto('/login')
    await expect(page).toHaveURL('/login')
    await expect(page.getByPlaceholder(/email/i)).toBeVisible()
    await expect(page.getByPlaceholder(/password/i)).toBeVisible()
    await expect(page.getByRole('button', { name: /sign in|log in|login/i })).toBeVisible()
  })

  test('successful login redirects to /dashboard', async ({ page }) => {
    const jsErrors = []
    page.on('pageerror', (err) => jsErrors.push(err.message))

    await loginAsDemo(page)

    await expect(page).toHaveURL('/dashboard')
    // Dashboard must actually render content, not stay blank
    await expect(page.getByText(/dashboard|welcome|notes/i).first()).toBeVisible({ timeout: 10000 })

    expect(jsErrors).toEqual([])
  })

  test('wrong password shows error and stays on /login', async ({ page }) => {
    await page.goto('/login')
    await page.getByPlaceholder(/email/i).fill('demo@example.com')
    await page.getByPlaceholder(/password/i).fill('definitely-wrong-password')
    await page.getByRole('button', { name: /sign in|log in|login/i }).click()

    await expect(page.getByText(/invalid|incorrect|wrong|error/i)).toBeVisible({ timeout: 10000 })
    await expect(page).toHaveURL('/login')
  })

  test('non-existent email shows error and stays on /login', async ({ page }) => {
    await page.goto('/login')
    await page.getByPlaceholder(/email/i).fill('nobody-exists@example.com')
    await page.getByPlaceholder(/password/i).fill('somepassword')
    await page.getByRole('button', { name: /sign in|log in|login/i }).click()

    await expect(page.getByText(/invalid|incorrect|not found|error/i)).toBeVisible({ timeout: 10000 })
    await expect(page).toHaveURL('/login')
  })

  test('empty email and password shows validation error', async ({ page }) => {
    await page.goto('/login')
    await page.getByRole('button', { name: /sign in|log in|login/i }).click()

    // Either HTML5 native validation prevents submission, or a custom error appears.
    // Either way the user must not reach /dashboard.
    await expect(page).toHaveURL('/login')
  })

  test('logout clears session and /dashboard redirects to /login', async ({ page }) => {
    await loginAsDemo(page)
    await expect(page).toHaveURL('/dashboard')

    // Find and click the logout link or button
    await page.getByRole('button', { name: /logout|sign out|log out/i }).first().click()
    await expect(page).toHaveURL('/login', { timeout: 10000 })

    // Attempting to visit a protected page must redirect back to login
    await page.goto('/dashboard')
    await expect(page).toHaveURL('/login', { timeout: 15000 })
  })

  test('after logout, /settings also redirects to /login', async ({ page }) => {
    await loginAsDemo(page)
    await page.getByRole('button', { name: /logout|sign out|log out/i }).first().click()
    await expect(page).toHaveURL('/login', { timeout: 10000 })

    await page.goto('/settings')
    await expect(page).toHaveURL('/login', { timeout: 15000 })
  })

  test('visiting /dashboard without session redirects to /login', async ({ page }) => {
    // Ensure no session exists
    await page.goto('/login')
    await page.evaluate(() => localStorage.clear())

    await page.goto('/dashboard')
    await expect(page).toHaveURL('/login', { timeout: 15000 })
  })

  test('link to register page is present (if registration is enabled)', async ({ page }) => {
    await page.goto('/login')
    // GAP-7: If self-registration is disabled, there may be no register link.
    //        This test is informational — it passes whether the link exists or not.
    const registerLink = page.getByRole('link', { name: /register|sign up|create account/i })
    const hasLink = await registerLink.isVisible().catch(() => false)
    // Log presence for awareness; no assertion failure either way.
    test.info().annotations.push({
      type: 'register-link',
      description: hasLink ? 'Register link found on login page' : 'No register link on login page',
    })
  })
})
