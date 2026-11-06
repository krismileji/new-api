/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import i18n from 'i18next'
import { afterEach, expect, test, vi } from 'vitest'

import { SitePublicContent, SitePublicFooter } from '../site-public-content'

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, to }: { children: React.ReactNode; to: string }) => (
    <a href={to}>{children}</a>
  ),
}))

afterEach(async () => {
  document.querySelector('#site-public-content')?.remove()
  await i18n.changeLanguage('en')
})

test('shows published legal links when the hosted site enables both policies', () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], {
    server_address: 'https://coder.work-proxy.com',
    user_agreement_enabled: true,
    privacy_policy_enabled: true,
  })

  render(
    <QueryClientProvider client={client}>
      <SitePublicFooter />
    </QueryClientProvider>
  )

  expect(screen.getByRole('link', { name: '用户协议' })).toHaveAttribute(
    'href',
    '/user-agreement'
  )
  expect(screen.getByRole('link', { name: '隐私政策' })).toHaveAttribute(
    'href',
    '/privacy-policy'
  )
  expect(
    screen.getByRole('link', { name: 'New API · QuantumNous' })
  ).toHaveAttribute('href', 'https://github.com/QuantumNous/new-api')
})

test('uses the server-published Chinese pricing copy and links to real support channels', async () => {
  await i18n.changeLanguage('zhCN')
  const inline = document.createElement('script')
  inline.id = 'site-public-content'
  inline.type = 'application/json'
  inline.textContent = JSON.stringify({
    kind: 'pricing',
    lang: 'zh-CN',
    title: 'New API | 查看价格',
    heading: '如何查看 Coder Proxy 当前价格？',
    lead: '请查看当前价格和适用条件。',
    sections: [
      {
        Title: '价格以什么为准？',
        Text: '以本页实时展示的数据为准。',
        ReferenceLabel: '联系价格支持',
        ReferenceURL: '/contact?lang=zh-CN',
      },
    ],
    contactIntro: '对价格或用量有疑问，可联系我们。',
    contactLabel: '联系我们',
    aboutLabel: '关于我们',
    projectNote: '本站网关软件基于 New API 项目',
    maintainerLabel: '内容维护',
    updatedLabel: '更新于',
    updatedDate: '2026 年 11 月 06 日',
    modifiedDate: '2026-11-06',
    signupLabel: '开始使用',
    pricingLabel: '查看价格',
    docsLabel: '使用文档',
  })
  document.head.append(inline)

  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <SitePublicContent page='pricing' />
    </QueryClientProvider>
  )

  expect(
    screen.getByRole('heading', {
      level: 1,
      name: '如何查看 Coder Proxy 当前价格？',
    })
  ).toBeVisible()
  expect(document.title).toBe('New API | 查看价格')
  expect(screen.getByText('以本页实时展示的数据为准。')).toBeVisible()
  expect(screen.getByRole('time')).toHaveAttribute('datetime', '2026-11-06')
  expect(screen.getByRole('link', { name: '联系价格支持' })).toHaveAttribute(
    'href',
    '/contact?lang=zh-CN'
  )
  expect(screen.getByRole('link', { name: /krismileji/ })).toHaveAttribute(
    'href',
    'https://t.me/krismileji'
  )
  expect(screen.getByRole('link', { name: /krismiledev/ })).toHaveAttribute(
    'href',
    'mailto:krismiledev@gmail.com'
  )
  expect(screen.getByRole('link', { name: '联系我们' })).toHaveAttribute(
    'href',
    '/contact?lang=zh-CN'
  )
})
