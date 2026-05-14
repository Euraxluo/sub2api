import { beforeEach, describe, expect, it, vi } from 'vitest'

const post = vi.fn()
const get = vi.fn()

vi.mock('@/api/client', () => ({
  apiClient: {
    post: (...args: unknown[]) => post(...args),
    get: (...args: unknown[]) => get(...args),
  },
}))

describe('custom tools codex quota guard api', () => {
  beforeEach(() => {
    post.mockReset()
    get.mockReset()
  })

  it('adds x-api-key header only for start when provided', async () => {
    post.mockResolvedValueOnce({ data: { running: true } })
    const { startCodexQuotaGuard } = await import('../api')

    await startCodexQuotaGuard(
      {
        enabled: true,
        reserve_percent: 1,
        windows: ['5h', '7d'],
        interval_seconds: 60,
        account_types: ['oauth'],
        dry_run: false,
      },
      'sk-admin-test'
    )

    expect(post).toHaveBeenCalledWith(
      '/admin/codex-quota-guard/start',
      expect.objectContaining({
        enabled: true,
        reserve_percent: 1,
      }),
      expect.objectContaining({
        headers: {
          'x-api-key': 'sk-admin-test',
        },
      })
    )
  })

  it('does not add x-api-key header when omitted', async () => {
    post.mockResolvedValueOnce({ data: { running: true } })
    const { startCodexQuotaGuard } = await import('../api')

    await startCodexQuotaGuard({
      enabled: true,
      reserve_percent: 1,
      windows: ['5h', '7d'],
      interval_seconds: 60,
      account_types: ['oauth'],
      dry_run: false,
    })

    expect(post).toHaveBeenCalledWith(
      '/admin/codex-quota-guard/start',
      expect.objectContaining({
        enabled: true,
      }),
      expect.objectContaining({
        headers: undefined,
      })
    )
  })
})
