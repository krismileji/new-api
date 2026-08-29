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

import {
  getSidebarShopEntries,
  getSidebarShopEntryTitle,
} from '../system-settings/maintenance/config'

const SHOP_IFRAME_SANDBOX =
  'allow-forms allow-popups allow-popups-to-escape-sandbox allow-scripts allow-same-origin allow-top-navigation-by-user-activation'

function resolveShopRechargeEntries(raw: unknown) {
  if (typeof raw !== 'string' || raw.trim() === '') return []

  try {
    const parsed = JSON.parse(raw) as { personal?: Record<string, unknown> }
    return getSidebarShopEntries(parsed.personal).filter((entry) =>
      isHttpUrl(entry.url)
    )
  } catch {
    return []
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

type ShopRechargeProps = {
  shopIndex?: number
}

export function ShopRecharge({ shopIndex }: ShopRechargeProps) {
  const { status, loading } = useStatus()
  const shopEntries = resolveShopRechargeEntries(status?.SidebarModulesAdmin)
  const requestedIndex =
    typeof shopIndex === 'number' &&
    Number.isInteger(shopIndex) &&
    shopIndex >= 0
      ? shopIndex
      : 0
  const selectedIndex = requestedIndex < shopEntries.length ? requestedIndex : 0
  const selectedEntry = shopEntries[selectedIndex] ?? shopEntries[0]
  const shopUrl = selectedEntry?.url ?? ''
  const shopTitle = selectedEntry
    ? getSidebarShopEntryTitle(selectedEntry, selectedIndex, shopEntries.length)
    : '小铺充值'
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
        title={shopTitle}
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
      <SectionPageLayout.Title>{shopTitle}</SectionPageLayout.Title>
      <SectionPageLayout.Content>{content}</SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
