import { render, screen } from '@testing-library/react'
import i18n from 'i18next'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { useStatus } from '@/hooks/use-status'

import { SiteInfo } from '../site-info'

vi.mock('@/hooks/use-status', () => ({ useStatus: vi.fn() }))

const mockedUseStatus = vi.mocked(useStatus)

afterEach(async () => {
  await i18n.changeLanguage('en')
  vi.resetAllMocks()
})

describe('Coder Proxy site information', () => {
  it('shows the English service description and direct contact links on the configured site', async () => {
    mockedUseStatus.mockReturnValue({
      status: { server_address: 'https://coder.work-proxy.com' },
      loading: false,
      error: null,
    } as ReturnType<typeof useStatus>)
    await i18n.changeLanguage('en')

    render(<SiteInfo />)

    expect(
      screen.getByRole('heading', { level: 2, name: 'About Coder Proxy' })
    ).toBeVisible()
    expect(screen.getByText(/AI aggregation platform/)).toBeVisible()
    expect(screen.getByRole('link', { name: '@krismileji' })).toHaveAttribute(
      'href',
      'https://t.me/krismileji'
    )
    expect(
      screen.getByRole('link', { name: 'krismiledev@gmail.com' })
    ).toHaveAttribute('href', 'mailto:krismiledev@gmail.com')
  })

  it('shows the Chinese description and a page heading on the about view', async () => {
    mockedUseStatus.mockReturnValue({
      status: { server_address: 'https://coder.work-proxy.com/' },
      loading: false,
      error: null,
    } as ReturnType<typeof useStatus>)
    await i18n.changeLanguage('zhCN')

    render(<SiteInfo variant='about' />)

    expect(
      screen.getByRole('heading', { level: 1, name: '关于 Coder Proxy' })
    ).toBeVisible()
    expect(screen.getByText(/统一 API 接入本站当前支持的模型/)).toBeVisible()
  })

  it('does not add downstream site information to other deployments', () => {
    mockedUseStatus.mockReturnValue({
      status: { server_address: 'https://other.example.com' },
      loading: false,
      error: null,
    } as ReturnType<typeof useStatus>)

    render(<SiteInfo />)

    expect(screen.queryByText(/Coder Proxy/)).not.toBeInTheDocument()
  })
})
