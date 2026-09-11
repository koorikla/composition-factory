const { test, expect } = require('@playwright/test')
const { resetDoc, guardPageErrors } = require('./helpers')
guardPageErrors()

test.beforeEach(async ({ request }) => {
  await resetDoc(request)
})

test.describe('CF-237: Docker environment fix tip suppression on pipeline render errors', () => {
  test('pipeline fatal error during validation does not display Docker environment fix tip', async ({ page }) => {
    await page.route('**/api/render', async route => {
      if (route.request().method() === 'POST') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            ok: false,
            resources: 0,
            error: 'crossplane: error: cannot render composite resource: crossplane internal render in Docker: pipeline returned fatal: cannot render template: template: trust-policy:10: executing "trust-policy" at <.spec.oidcProviderArn>: map has no entry for key "oidcProviderArn"',
            unavailable: ''
          })
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')

    const valBtn = page.locator('#validateBtn')
    await expect(valBtn).toBeEnabled()
    await valBtn.click()

    // Status chip should indicate validation error
    const chip = page.locator('#valid')
    await expect(chip).toHaveText('validation error')

    // Chip title and aria-label must not display the Docker environment fix tip
    const chipTitle = (await chip.getAttribute('title')) || ''
    const chipAria = (await chip.getAttribute('aria-label')) || ''
    expect(chipTitle).not.toMatch(/Environment Fix Tip/i)
    expect(chipTitle).not.toMatch(/Docker Desktop|dockerd/i)
    expect(chipAria).not.toMatch(/Environment Fix Tip/i)
    expect(chipAria).not.toMatch(/Docker Desktop|dockerd/i)

    // Warnbar or alert banner should be visible
    const warn = page.locator('#render-warn, #render-warn-banner, .warnbar').first()
    await expect(warn).toBeVisible()
    const warnText = await warn.textContent()

    // Verbatim pipeline error is shown
    expect(warnText).toContain('trust-policy')
    expect(warnText).toContain('oidcProviderArn')

    // Must NOT contain the Docker environment fix tip
    expect(warnText).not.toMatch(/Environment Fix Tip/i)
    expect(warnText).not.toMatch(/Docker Desktop|dockerd/i)
  })

  test('Docker daemon connection failure displays Docker environment fix tip', async ({ page }) => {
    await page.route('**/api/render', async route => {
      if (route.request().method() === 'POST') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            ok: false,
            resources: 0,
            error: '',
            unavailable: 'Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?'
          })
        })
      } else {
        await route.continue()
      }
    })

    await page.goto('/')

    const valBtn = page.locator('#validateBtn')
    await expect(valBtn).toBeEnabled()
    await valBtn.click()

    const warn = page.locator('#render-warn, #render-warn-banner, .warnbar').first()
    await expect(warn).toBeVisible()
    const warnText = await warn.textContent()

    expect(warnText).toMatch(/Environment Fix Tip/i)
    expect(warnText).toMatch(/Docker Desktop or dockerd is running/i)
  })

  test('diagnoseError unit evaluations: pipeline fatal vs daemon unavailability', async ({ page }) => {
    await page.goto('/')

    const results = await page.evaluate(async () => {
      const outputMod = await import('./js/regions/output.js')
      return {
        pipelineFatal: outputMod.diagnoseError(
          'crossplane: error: cannot render composite resource: crossplane internal render in Docker: pipeline returned fatal: cannot render template: template: trust-policy:10: executing "trust-policy" at <.spec.oidcProviderArn>: map has no entry for key "oidcProviderArn"'
        ),
        pipelineFatalShort: outputMod.diagnoseError(
          'pipeline returned fatal: step "render-queue" returned fatal'
        ),
        dockerUnavailable: outputMod.diagnoseError(
          'Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?'
        ),
        dockerCmdNotFound: outputMod.diagnoseError(
          'docker: command not found'
        ),
        daemonNotAccessible: outputMod.diagnoseError(
          'daemon not accessible'
        ),
        dockerSchemaField: outputMod.diagnoseError(
          'field spec.resources[0].base.spec.forProvider.dockerImage is invalid'
        )
      }
    })

    // Pipeline fatal errors must not be classified as environment failures
    expect(results.pipelineFatal.isEnv).toBe(false)
    expect(results.pipelineFatal.tip).toBe('')
    expect(results.pipelineFatalShort.isEnv).toBe(false)
    expect(results.pipelineFatalShort.tip).toBe('')

    // Genuine Docker daemon issues must be classified as environment failures
    expect(results.dockerUnavailable.isEnv).toBe(true)
    expect(results.dockerUnavailable.tip).toMatch(/Docker Desktop or dockerd is running/i)
    expect(results.dockerCmdNotFound.isEnv).toBe(true)
    expect(results.dockerCmdNotFound.tip).toMatch(/Docker Desktop or dockerd is running/i)
    expect(results.daemonNotAccessible.isEnv).toBe(true)
    expect(results.daemonNotAccessible.tip).toMatch(/Docker Desktop or dockerd is running/i)

    // Unrelated mentions of docker must not trigger environment tip
    expect(results.dockerSchemaField.isEnv).toBe(false)
    expect(results.dockerSchemaField.tip).toBe('')
  })
})
