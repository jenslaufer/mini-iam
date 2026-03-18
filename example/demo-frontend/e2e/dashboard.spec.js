// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsDemo, DEMO_EMAIL } from './helpers.js'

// ─── UX GAPS OBSERVED ────────────────────────────────────────────────────────
// GAP-8:  There is no confirmation dialog when deleting a note. A single click
//         permanently destroys user data. Consider adding a confirmation step.
// GAP-9:  Notes have no edit functionality described. Users can only add or
//         delete — no way to fix a typo without deleting and re-creating.
// GAP-10: The "notes count" stat is not described as updating in real-time.
//         After adding/deleting a note the counter might be stale until refresh.
// ─────────────────────────────────────────────────────────────────────────────

test.describe('Dashboard', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
  })

  test('shows user info on the dashboard', async ({ page }) => {
    await expect(page).toHaveURL('/dashboard')
    // Some representation of the logged-in user must be visible
    await expect(page.getByText(DEMO_EMAIL).first()).toBeVisible({ timeout: 10000 })
  })

  test('shows platform stats section', async ({ page }) => {
    // A stats section (notes count, platform stats, etc.) must render
    await expect(page.getByText(/notes|stats|count/i).first()).toBeVisible({ timeout: 10000 })
  })

  test('has navigation link to Settings', async ({ page }) => {
    const settingsLink = page.getByRole('link', { name: /settings/i })
    await expect(settingsLink).toBeVisible()
    await settingsLink.click()
    await expect(page).toHaveURL('/settings', { timeout: 15000 })
  })

  test('has logout link that ends the session', async ({ page }) => {
    const logoutLink = page.getByRole('button', { name: /logout|sign out|log out/i }).first()
    await expect(logoutLink).toBeVisible()
    await logoutLink.click()
    await expect(page).toHaveURL('/login', { timeout: 15000 })
  })
})

test.describe('Dashboard — notes CRUD', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsDemo(page)
  })

  test('add note form has title field', async ({ page }) => {
    // At minimum a title input and submit button must be present
    await expect(page.getByPlaceholder(/title/i)).toBeVisible({ timeout: 10000 })
    await expect(page.getByRole('button', { name: /add|create|save/i }).first()).toBeVisible()
  })

  test('adds a note with title only', async ({ page }) => {
    const noteTitle = `Note ${Date.now()}`

    await page.getByPlaceholder(/title/i).fill(noteTitle)
    await page.getByRole('button', { name: /add|create|save/i }).first().click()

    // The note must appear in the list
    await expect(page.getByText(noteTitle)).toBeVisible({ timeout: 10000 })

    // Cleanup — delete the note we just created
    const noteRow = page.locator('li, tr, article, [data-testid="note"]', { hasText: noteTitle })
    const deleteBtn = noteRow.getByRole('button', { name: /delete|remove/i })
    if (await deleteBtn.isVisible().catch(() => false)) {
      await deleteBtn.click()
      await expect(page.getByText(noteTitle)).not.toBeVisible({ timeout: 10000 })
    }
  })

  test('adds a note with title and body', async ({ page }) => {
    const noteTitle = `Full Note ${Date.now()}`
    const noteBody = 'This is the body of the note.'

    await page.getByPlaceholder(/title/i).fill(noteTitle)

    // Body is optional — fill only if the field exists
    const bodyField = page.getByPlaceholder(/body|content|description/i)
    if (await bodyField.isVisible().catch(() => false)) {
      await bodyField.fill(noteBody)
    }

    await page.getByRole('button', { name: /add|create|save/i }).first().click()

    await expect(page.getByText(noteTitle)).toBeVisible({ timeout: 10000 })

    // Cleanup
    const noteRow = page.locator('li, tr, article, [data-testid="note"]', { hasText: noteTitle })
    const deleteBtn = noteRow.getByRole('button', { name: /delete|remove/i })
    if (await deleteBtn.isVisible().catch(() => false)) {
      await deleteBtn.click()
    }
  })

  test('deletes a note', async ({ page }) => {
    const noteTitle = `Delete Me ${Date.now()}`

    // Create a note first
    await page.getByPlaceholder(/title/i).fill(noteTitle)
    await page.getByRole('button', { name: /add|create|save/i }).first().click()
    await expect(page.getByText(noteTitle)).toBeVisible({ timeout: 10000 })

    // Delete it — GAP-8: no confirmation dialog expected
    const noteRow = page.locator('li, tr, article, [data-testid="note"]', { hasText: noteTitle })
    await noteRow.getByRole('button', { name: /delete|remove/i }).click()

    await expect(page.getByText(noteTitle)).not.toBeVisible({ timeout: 10000 })
  })

  test('add note with empty title is rejected', async ({ page }) => {
    // Count notes before
    const before = await page.locator('li, tr, article, [data-testid="note"]').count()

    await page.getByRole('button', { name: /add|create|save/i }).first().click()

    // Note count must not increase (empty title should be rejected)
    // Allow a brief moment for any async update
    await page.waitForTimeout(500)
    const after = await page.locator('li, tr, article, [data-testid="note"]').count()
    expect(after).toBe(before)
  })
})
