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
import { ArrowRight01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'

import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import { formatChannelMonitorCost } from '../lib/format'
import type { ChannelMonitorCostAPIKey } from '../types'

type ChannelMonitorAPIKeyCostTableProps = {
  items: ChannelMonitorCostAPIKey[]
}

export function ChannelMonitorAPIKeyCostTable(
  props: ChannelMonitorAPIKeyCostTableProps
) {
  return (
    <section
      className='flex flex-col gap-2'
      aria-labelledby='api-key-cost-title'
    >
      <h3 id='api-key-cost-title' className='text-sm font-medium'>
        API Key 成本（按名称）
      </h3>
      {props.items.length === 0 ? (
        <Empty className='min-h-32 border'>
          <EmptyHeader>
            <EmptyTitle>暂无 API Key 成本</EmptyTitle>
            <EmptyDescription>新请求结算后开始记录</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className='max-h-[min(30rem,50dvh)] overflow-auto rounded-md border'>
          <div className='divide-border divide-y'>
            {props.items.map((item) => {
              let apiKeyName = item.api_key_name
              if (!apiKeyName && item.api_key_id > 0) {
                apiKeyName = `未命名 API Key #${item.api_key_id}`
              } else if (!apiKeyName && item.api_key) {
                apiKeyName = `上游 Key ${item.api_key}`
              } else if (!apiKeyName) {
                apiKeyName = '未识别 API Key'
              }
              return (
                <details
                  key={`${item.api_key_id}:${item.id}:${apiKeyName}`}
                  className='group'
                >
                  <summary className='hover:bg-muted/40 flex cursor-pointer list-none items-center gap-2 px-3 py-2.5 [&::-webkit-details-marker]:hidden'>
                    <HugeiconsIcon
                      icon={ArrowRight01Icon}
                      aria-hidden='true'
                      className='text-muted-foreground size-4 shrink-0 transition-transform group-open:rotate-90'
                    />
                    <span className='min-w-0 flex-1'>
                      <span
                        className='block truncate font-medium'
                        title={apiKeyName}
                      >
                        {apiKeyName}
                      </span>
                      {item.api_key_id > 0 ? (
                        <span className='text-muted-foreground block truncate text-xs'>
                          ID {item.api_key_id}
                        </span>
                      ) : null}
                      {item.api_key ? (
                        <span
                          className='text-muted-foreground block truncate font-mono text-xs'
                          title={item.api_key}
                        >
                          上游 Key {item.api_key}
                        </span>
                      ) : null}
                    </span>
                    <span className='text-muted-foreground shrink-0 text-xs'>
                      {item.channels.length} 个渠道
                    </span>
                    <span className='shrink-0 text-right font-mono font-semibold tabular-nums'>
                      {formatChannelMonitorCost(item.cost_cny)}
                    </span>
                  </summary>
                  <div className='bg-muted/20 border-t px-3 py-2'>
                    {item.channels.length === 0 ? (
                      <p className='text-muted-foreground py-2 text-xs'>
                        暂无渠道明细
                      </p>
                    ) : (
                      <Table className='min-w-[520px] table-fixed'>
                        <TableHeader>
                          <TableRow>
                            <TableHead className='w-[52%]'>关联渠道</TableHead>
                            <TableHead className='w-[24%] text-right'>
                              渠道成本
                            </TableHead>
                            <TableHead className='w-[12%] text-right'>
                              结算
                            </TableHead>
                            <TableHead className='w-[12%] text-right'>
                              未确认
                            </TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {item.channels.map((channel) => (
                            <TableRow key={channel.channel_id}>
                              <TableCell className='min-w-0'>
                                <span
                                  className='block truncate'
                                  title={channel.channel_name}
                                >
                                  {channel.channel_name}
                                </span>
                              </TableCell>
                              <TableCell className='text-right font-mono tabular-nums'>
                                {formatChannelMonitorCost(channel.cost_cny)}
                              </TableCell>
                              <TableCell className='text-right font-mono tabular-nums'>
                                {channel.settled_count}
                              </TableCell>
                              <TableCell className='text-right font-mono tabular-nums'>
                                {channel.unresolved_count > 0 ? (
                                  <span
                                    className='text-warning'
                                    title={`${channel.unresolved_count} 次成本未确认`}
                                  >
                                    {channel.unresolved_count}
                                  </span>
                                ) : (
                                  0
                                )}
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    )}
                  </div>
                </details>
              )
            })}
          </div>
        </div>
      )}
    </section>
  )
}
