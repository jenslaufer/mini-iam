// @ts-check
import { test, expect } from '@playwright/test'
import { registerUser, getAdminToken, deleteUserApi } from './helpers.js'

const BASE_URL = 'http://localhost:3000'

test.describe('Login page', () => {
  test('shows login page at /login', async ({ page }) => {
    await page.goto('/login')
    await expect(page).toHaveURL('/login')
    await expect(page.getByRole('heading', { name: 'launch-kit' })).toBeVisible()
    await expect(page.getByPlaceholder('Leave empty for platform admin')).toBeVisible()
    await expect(page.getByPlaceholder('admin@example.com')).toBeVisible()
    await expect(page.getByPlaceholder('••••••••')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()
  })

  test('redirects to /login when not authenticated', async ({ page }) => {
    await page.goto('/login')
    await page.evaluate(() => localStorage.clear())

    await page.goto('/dashboard')
    await expect(page).toHaveURL('/login')

    await page.goto('/users')
    await expect(page).toHaveURL('/login')

    await page.goto('/clients')
    await expect(page).toHaveURL('/login')
  })

  test('shows error on wrong credentials', async ({ page }) => {
    await page.goto('/login')
    await page.getByPlaceholder('admin@example.com').fill('admin@launch-kit.local')
    await page.getByPlaceholder('••••••••').fill('wrongpassword')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page.locator('.bg-red-50')).toBeVisible()
    await expect(page).toHaveURL('/login')
  })

  test('shows error when non-admin tries to login', async ({ page }) => {
    const email = `user-${Date.now()}@example.com`
    const { id } = await registerUser(BASE_URL, email, 'testpass123', 'Regular User')

    await page.goto('/login')
    await page.getByPlaceholder('admin@example.com').fill(email)
    await page.getByPlaceholder('••••••••').fill('testpass123')
    await page.getByRole('button', { name: 'Sign in' }).click()

    await expect(page.locator('.bg-red-50')).toBeVisible()
    await expect(page.locator('.bg-red-50')).toContainText('admin role required')
    await expect(page).toHaveURL('/login')

    // Cleanup
    const token = await getAdminToken(BASE_URL)
    await deleteUserApi(BASE_URL, token, id)
  })

  test('successful admin login redirects to dashboard', async ({ page }) => {
    await page.goto('/login')
    await page.getByPlaceholder('admin@example.com').fill('admin@launch-kit.local')
    await page.getByPlaceholder('••••••••').fill('changeme')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page).toHaveURL('/dashboard')
    await expect(page.getByText('Total Users')).toBeVisible()
  })

  test('logout returns to login page', async ({ page }) => {
    await page.goto('/login')
    await page.getByPlaceholder('admin@example.com').fill('admin@launch-kit.local')
    await page.getByPlaceholder('••••••••').fill('changeme')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page).toHaveURL('/dashboard', { timeout: 10000 })

    await page.getByRole('button', { name: 'Sign out' }).click()
    await expect(page).toHaveURL('/login')

    await page.goto('/dashboard')
    await expect(page).toHaveURL('/login')
  })
})
