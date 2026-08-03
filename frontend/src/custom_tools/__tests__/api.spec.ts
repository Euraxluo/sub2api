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

  it('sends limiter policies and adds x-api-key only for start when provided', async () => {
    post.mockResolvedValueOnce({ data: { running: true } })
    const { startCodexQuotaGuard } = await import('../api')

    await startCodexQuotaGuard(
      {
        policies: [
          {
            id: 'daily-openai',
            name: 'Daily OpenAI',
            enabled: true,
            interval_seconds: 60,
            account_ids: [12, 15],
            daily_spend_limit_usd: 500,
            daily_token_limit: 1000000,
            weekly_spend_limit_usd: 2000,
            weekly_token_limit: 7000000,
            daily_spend_timezone: 'Asia/Shanghai',
            dry_run: false,
          },
        ],
      },
      'sk-admin-test'
    )

    expect(post).toHaveBeenCalledWith(
      '/admin/codex-quota-guard/start',
      expect.objectContaining({
        policies: [
          expect.objectContaining({
            id: 'daily-openai',
            name: 'Daily OpenAI',
            enabled: true,
            interval_seconds: 60,
            dry_run: false,
          }),
        ],
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
      policies: [
        {
          id: 'daily-openai',
          name: 'Daily OpenAI',
          enabled: true,
          interval_seconds: 60,
          dry_run: false,
        },
      ],
    })

    expect(post).toHaveBeenCalledWith(
      '/admin/codex-quota-guard/start',
      expect.objectContaining({
        policies: expect.arrayContaining([expect.objectContaining({ id: 'daily-openai' })]),
      }),
      expect.objectContaining({
        headers: undefined,
      })
    )
  })
})
