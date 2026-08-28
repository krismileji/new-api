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
import type { ReactNode } from 'react'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { useStatus } from '@/hooks/use-status'
import { isHttpUrl } from '@/lib/content-format'

const SHOP_IFRAME_SANDBOX =
  'allow-forms allow-popups allow-popups-to-escape-sandbox allow-scripts allow-same-origin allow-top-navigation-by-user-activation'

function resolveShopRechargeUrl(raw: unknown): string {
  if (typeof raw !== 'string' || raw.trim() === '') return ''

  try {
    const parsed = JSON.parse(raw) as {
      personal?: { shop_url?: unknown }
    }
    const shopUrl = parsed.personal?.shop_url
    return typeof shopUrl === 'string' && isHttpUrl(shopUrl.trim())
      ? shopUrl.trim()
      : ''
  } catch {
    return ''
  }
}

function getShopIframeSandbox(url: string): string | undefined {
  if (typeof window !== 'undefined') {
    try {
      if (new URL(url).origin === window.location.origin) return undefined
    } catch {
      return SHOP_IFRAME_SANDBOX
    }
  }

  return SHOP_IFRAME_SANDBOX
}

export function ShopRecharge() {
  const { status, loading } = useStatus()
  const shopUrl = resolveShopRechargeUrl(status?.SidebarModulesAdmin)
  let content: ReactNode

  if (loading && !status) {
    content = (
      <div className='text-muted-foreground flex h-full items-center justify-center text-sm'>
        正在加载小铺充值页面…
      </div>
    )
  } else if (shopUrl) {
    content = (
      <iframe
        src={shopUrl}
        className='h-full min-h-0 w-full border-0'
        title='小铺充值'
        sandbox={getShopIframeSandbox(shopUrl)}
      />
    )
  } else {
    content = (
      <div className='flex h-full items-center justify-center p-6'>
        <Alert className='max-w-xl'>
          <AlertTitle>小铺充值暂未配置</AlertTitle>
          <AlertDescription>
            请管理员在系统管理的侧边栏模块中启用小铺充值并填写链接。
          </AlertDescription>
        </Alert>
      </div>
    )
  }

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>小铺充值</SectionPageLayout.Title>
      <SectionPageLayout.Content>{content}</SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
