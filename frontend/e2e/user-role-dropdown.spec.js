// @ts-check
import { test, expect } from '@playwright/test'
import { loginAsAdmin, getAdminToken, registerUser, deleteUserApi } from './helpers.js'

test.describe('User role dropdown on last row', () => {
  let adminToken
  const testUsers = []

  test.beforeEach(async ({ page, baseURL }) => {
    await loginAsAdmin(page)
    adminToken = await getAdminToken(baseURL)
    for (let i = 0; i < 3; i++) {
      const user = await registerUser(baseURL, `overflow-${i}-${Date.now()}@example.com`, 'testpass1', `User ${i}`)
      testUsers.push(user)
    }
    await page.goto('/users')
    await page.waitForSelector('table tbody tr')
  })

  test.afterEach(async ({ baseURL }) => {
    for (const user of testUsers) {
      await deleteUserApi(baseURL, adminToken, user.id).catch(() => {})
    }
    testUsers.length = 0
  })

  test('role dropdown on last user is not clipped by table container', async ({ page }) => {
    const lastRow = page.locator('tbody tr').last()
    await lastRow.scrollIntoViewIfNeeded()
    await lastRow.getByRole('button', { name: 'Role' }).click()

    const dropdown = lastRow.locator('.absolute')
    await expect(dropdown).toBeVisible({ timeout: 3000 })

    // Check that the dropdown is not visually clipped by any ancestor with overflow:hidden.
    // An element is clipped when its bounding rect extends beyond an overflow:hidden ancestor's rect.
    const isClipped = await dropdown.evaluate((el) => {
      const rect = el.getBoundingClientRect()
      let parent = el.parentElement
      while (parent) {
        const style = getComputedStyle(parent)
        if (style.overflow === 'hidden' || style.overflowY === 'hidden') {
          const parentRect = parent.getBoundingClientRect()
          if (rect.bottom > parentRect.bottom + 1) {
            return true
          }
        }
        parent = parent.parentElement
      }
      return false
    })

    expect(isClipped, 'Dropdown should not be clipped by overflow-hidden ancestor').toBe(false)
  })
})
