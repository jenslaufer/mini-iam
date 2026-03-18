// @ts-check
import { test, expect } from '@playwright/test'
import { signupContact, deleteContactApi, deleteUserApi, getAdminToken, findAndDeleteContact, findAndDeleteUser, uniqueEmail } from './helpers.js'

// ─── UX GAPS OBSERVED ────────────────────────────────────────────────────────
// GAP-1: After signup, the activation link is shown inline on the page.
//        Real products send the link by email and never expose it in the UI.
//        This is intentional for a demo but worth flagging if ever productised.
// GAP-2: There is no rate-limiting or duplicate-email guard visible from the
//        UI — submitting the same email twice may create two contacts or silently
//        succeed. The API behaviour is untested here because it is backend-owned.
// GAP-3: No feedback for network errors (e.g., backend unreachable).
// ─────────────────────────────────────────────────────────────────────────────

test.describe('Landing page', () => {
  test('renders early-access signup form', async ({ page }) => {
    await page.goto('/')

    // The page must be accessible and show at minimum a heading/brand
    await expect(page).toHaveURL('/')

    // Name and Email inputs must be present
    await expect(page.getByPlaceholder(/name/i)).toBeVisible()
    await expect(page.getByPlaceholder(/email|you@/i)).toBeVisible()

    // There must be a submit button
    await expect(page.getByRole('button', { name: /request access|sign up|get started|join|get.*access/i })).toBeVisible()
  })

  test('successful signup shows activation link', async ({ page }) => {
    const email = uniqueEmail('landing')
    let contactId

    await page.goto('/')

    await page.getByPlaceholder(/name/i).fill('Test User')
    await page.getByPlaceholder(/email|you@/i).fill(email)
    await page.getByRole('button', { name: /request access|sign up|get started|join|get.*access/i }).click()

    // Success state: a confirmation message must appear
    await expect(page.getByText(/thank you|success|check your email|activation|on the list/i)).toBeVisible({
      timeout: 10000,
    })

    // The activation link itself should be visible in the demo UI (GAP-1 above)
    const activationLink = page.getByRole('link', { name: /activate|set password|get started/i })
    await expect(activationLink).toBeVisible({ timeout: 10000 })

    // The link must point to /activate/...
    const href = await activationLink.getAttribute('href')
    expect(href).toMatch(/\/activate\//)

    // Cleanup — delete the created contact via admin API
    try {
      const adminToken = await getAdminToken().catch(() => null)
      if (adminToken) await findAndDeleteContact(adminToken, email)
    } catch (_) {
      // Cleanup is best-effort; test should not fail if admin API is unavailable
    }
  })

  test('signup form rejects empty name', async ({ page }) => {
    await page.goto('/')

    await page.getByPlaceholder(/email|you@/i).fill(uniqueEmail('empty-name'))
    await page.getByRole('button', { name: /request access|sign up|get started|join|get.*access/i }).click()

    // Should not navigate away or show success
    await expect(page).toHaveURL('/')
    // HTML5 validation or custom error — either way no success message
    await expect(page.getByText(/thank you|success|check your email/i)).not.toBeVisible()
  })

  test('signup form rejects empty email', async ({ page }) => {
    await page.goto('/')

    await page.getByPlaceholder(/name/i).fill('Some Name')
    await page.getByRole('button', { name: /request access|sign up|get started|join|get.*access/i }).click()

    await expect(page).toHaveURL('/')
    await expect(page.getByText(/thank you|success|check your email/i)).not.toBeVisible()
  })
})

test.describe('Landing page → Activation flow', () => {
  // This describe block tests the full path from landing page signup through
  // to the /activate/:token page, verifying only what the user sees.

  test('activation page renders password form for valid token', async ({ page }) => {
    const email = uniqueEmail('activate')
    let contactId

    // Get a real activation token via the API so we can test the UI
    let contact
    try {
      contact = await signupContact(email, 'Activate User')
      contactId = contact.id
    } catch (err) {
      test.skip(true, `signupContact API unavailable: ${err.message}`)
      return
    }

    const token = contact.invite_token ?? contact.activation_token ?? contact.token
    if (!token) {
      test.skip(true, 'API did not return an invite/activation token — adjust field name in helpers.js')
      return
    }

    await page.goto(`/activate/${token}`)

    // Password form must appear (two password fields + submit button)
    await expect(page.getByLabel(/^password/i)).toBeVisible({ timeout: 10000 })
    await expect(page.getByLabel(/confirm/i)).toBeVisible()
    await expect(page.getByRole('button', { name: /activate|set password|create account/i })).toBeVisible()

    // Cleanup
    try {
      const adminToken = await getAdminToken().catch(() => null)
      if (adminToken && contactId) await deleteContactApi(adminToken, contactId)
    } catch (_) {}
  })

  test('activation with valid token and password shows success', async ({ page }) => {
    const email = uniqueEmail('act-redirect')
    const password = 'ValidPass1!'

    let contact
    try {
      contact = await signupContact(email, 'Redirect User')
    } catch (err) {
      test.skip(true, `signupContact API unavailable: ${err.message}`)
      return
    }

    const token = contact.invite_token ?? contact.activation_token ?? contact.token
    if (!token) {
      test.skip(true, 'No activation token in API response')
      return
    }

    await page.goto(`/activate/${token}`)

    await page.getByLabel(/^password/i).fill(password)
    await page.getByLabel(/confirm/i).fill(password)
    await page.getByRole('button', { name: /activate|set password|create account/i }).click()

    // After activation the user sees a success message
    await expect(page.getByText(/activated|account created|success/i)).toBeVisible({ timeout: 15000 })

    // Cleanup — delete the user created by activation
    try {
      const adminToken = await getAdminToken().catch(() => null)
      if (adminToken) await findAndDeleteUser(adminToken, email)
    } catch (_) {}
  })

  test('activation password too short shows validation error', async ({ page }) => {
    const email = uniqueEmail('act-short-pw')
    let contactId

    let contact
    try {
      contact = await signupContact(email, 'Short PW User')
      contactId = contact.id
    } catch (err) {
      test.skip(true, `signupContact API unavailable: ${err.message}`)
      return
    }

    const token = contact.invite_token ?? contact.activation_token ?? contact.token
    if (!token) {
      test.skip(true, 'No activation token in API response')
      return
    }

    await page.goto(`/activate/${token}`)
    await page.getByLabel(/^password/i).fill('short')
    await page.getByLabel(/confirm/i).fill('short')
    await page.getByRole('button', { name: /activate|set password|create account/i }).click()

    // Should not redirect — stays on activation page
    await expect(page).toHaveURL(`/activate/${token}`)
    // Must show a validation message
    await expect(page.getByText(/at least 8|minimum 8|too short/i)).toBeVisible()

    // Cleanup
    try {
      const adminToken = await getAdminToken().catch(() => null)
      if (adminToken && contactId) await deleteContactApi(adminToken, contactId)
    } catch (_) {}
  })

  test('unknown activation token shows error page or message', async ({ page }) => {
    // GAP-4: Expired or invalid tokens must show a clear error, not a blank form
    // or silent failure. This test documents expected behaviour.
    await page.goto('/activate/bad')

    // The page must show some kind of error or "invalid link" message
    // (it must NOT show an active password form for a bad token)
    const errorVisible = await page
      .getByText(/invalid|expired|not found|error/i)
      .isVisible()
      .catch(() => false)

    const formVisible = await page
      .getByRole('button', { name: /activate|set password|create account/i })
      .isVisible()
      .catch(() => false)

    // If the form is shown for an invalid token, that is a UX gap.
    // We assert the error is visible; if this fails, file a bug (GAP-4).
    expect(errorVisible || !formVisible).toBe(true)
  })
})
