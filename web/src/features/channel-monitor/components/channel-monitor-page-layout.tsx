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

type ChannelMonitorPageLayoutProps = {
  actions: ReactNode
  children: ReactNode
  realtimeStatus: ReactNode
}

export function ChannelMonitorPageLayout(props: ChannelMonitorPageLayoutProps) {
  return (
    <SectionPageLayout stackHeaderOnMobile>
      <SectionPageLayout.Title>渠道监控</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <div
          role='toolbar'
          aria-label='渠道监控操作'
          className='flex w-full flex-wrap items-center justify-end gap-2 sm:w-auto sm:gap-x-4'
        >
          {props.actions}
        </div>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div
          data-slot='channel-monitor-page-content'
          className='flex min-w-0 flex-col gap-4'
        >
          <section
            aria-label='实时运行状态'
            className='min-w-0 shrink-0 empty:hidden'
          >
            {props.realtimeStatus}
          </section>
          {props.children}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
