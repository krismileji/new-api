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
import type { Row } from '@tanstack/react-table'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { TableCell, TableRow } from '@/components/ui/table'
import { formatNumber } from '@/lib/format'

import { formatChannelMonitorSmartScheduleUntil } from '../lib/smart-schedule-display'
import {
  primaryEntryStatus,
  primaryRouteStatus,
  type PrimaryEntry,
} from '../lib/smart-schedule-primary-management'

export function ChannelMonitorSmartSchedulePrimaryRow(props: {
  row: Row<PrimaryEntry>
  expanded: boolean
  search: string
  disabled: boolean
  onExpand: () => void
  onEdit: () => void
  onRemove: () => void
}) {
  const entry = props.row.original
  const status = primaryEntryStatus(entry)
  const active =
    entry.kind === 'channel'
      ? entry.routes.filter((route) => route.status === 'active').length
      : 0
  const statusLabels = {
    active: '全部生效',
    partial: '部分生效',
    inactive: '未生效',
    pending: '待新增路由',
    route: '',
  }
  const routes = entry.kind === 'channel' ? entry.routes : []
  const matched = routes.filter((route) =>
    [route.group, route.model, primaryRouteStatus(route)].some((value) =>
      value.toLowerCase().includes(props.search)
    )
  )
  const shownRoutes = matched.length > 0 ? matched : routes
  const label = `${entry.channel_name}${entry.kind === 'route' ? ` / ${entry.group} / ${entry.model}` : ''}`
  return (
    <>
      <TableRow
        data-state={props.row.getIsSelected() ? 'selected' : undefined}
        className='max-sm:grid max-sm:grid-cols-[2rem_minmax(0,1fr)_auto] [&>td]:whitespace-normal'
      >
        <TableCell className='w-10 align-top max-sm:w-auto'>
          <Checkbox
            checked={props.row.getIsSelected()}
            disabled={props.disabled}
            onCheckedChange={(checked) =>
              props.row.toggleSelected(checked === true)
            }
            aria-label={`选择 ${label}`}
          />
        </TableCell>
        <TableCell className='min-w-0 break-words'>
          <div>
            {entry.kind === 'channel' ? entry.channel_name : entry.model}
          </div>
          <div data-table-text='secondary' className='text-muted-foreground'>
            {entry.kind === 'channel' ? `#${entry.channel_id}` : entry.group}
          </div>
        </TableCell>
        <TableCell className='min-w-0 break-words max-sm:col-start-2'>
          <div>
            {entry.kind === 'channel'
              ? entry.group || '全部分组'
              : entry.channel_name}
          </div>
          <div data-table-text='secondary' className='text-muted-foreground'>
            {entry.kind === 'channel'
              ? `${formatNumber(entry.routes.length, 'zh-CN')} 条路由`
              : `#${entry.channel_id}`}
          </div>
        </TableCell>
        <TableCell className='max-sm:col-start-2'>
          {entry.kind === 'channel' ? (
            <>
              <Badge
                variant={
                  status === 'partial' || status === 'inactive'
                    ? 'warning'
                    : 'secondary'
                }
              >
                {statusLabels[status]}
              </Badge>
              <Button
                type='button'
                variant='link'
                size='sm'
                className='mt-1 block h-auto px-0 text-xs'
                disabled={props.disabled}
                aria-expanded={props.expanded}
                aria-controls={`primary-details-${entry.channel_id}`}
                onClick={props.onExpand}
              >
                {props.expanded
                  ? '收起详情'
                  : `${formatNumber(active, 'zh-CN')}/${formatNumber(entry.routes.length, 'zh-CN')} 已生效 · 详情`}
              </Button>
            </>
          ) : (
            <span className='text-muted-foreground'>
              {entry.allow_stability_degrade
                ? '允许稳定性降级'
                : '固定期间不降级'}
            </span>
          )}
        </TableCell>
        <TableCell className='break-words max-sm:col-start-3 max-sm:row-start-2 max-sm:max-w-28'>
          {formatChannelMonitorSmartScheduleUntil(entry.manual_primary_until)}
        </TableCell>
        <TableCell className='text-right max-sm:col-start-3 max-sm:row-start-1'>
          <div className='flex flex-wrap justify-end gap-1'>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              disabled={props.disabled}
              aria-label={`编辑 ${label}`}
              id={`primary-edit-${entry.key}`}
              onClick={props.onEdit}
            >
              编辑
            </Button>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              disabled={props.disabled}
              aria-label={`解除 ${label} 的固定`}
              onClick={props.onRemove}
            >
              解除
            </Button>
          </div>
        </TableCell>
      </TableRow>
      {props.expanded && entry.kind === 'channel' ? (
        <TableRow className='bg-muted/20 max-sm:block'>
          <TableCell colSpan={6} className='whitespace-normal max-sm:block'>
            <section
              id={`primary-details-${entry.channel_id}`}
              aria-label={`${entry.channel_name} 路由详情`}
              className='space-y-2 p-2'
            >
              <p className='text-muted-foreground text-xs'>
                单模型固定优先于渠道规则；详情按当前搜索内容筛选。
              </p>
              {shownRoutes.length ? (
                <ul className='divide-y'>
                  {shownRoutes.map((route) => (
                    <li
                      key={JSON.stringify([route.group, route.model])}
                      className='grid gap-1 py-2 text-xs sm:grid-cols-2'
                    >
                      <span className='break-all'>
                        {route.group} / {route.model}
                      </span>
                      <span className='text-muted-foreground break-words'>
                        {primaryRouteStatus(route)}
                      </span>
                    </li>
                  ))}
                </ul>
              ) : (
                <p>
                  当前没有模型路由；以后新增并参与调度的模型会自动纳入固定。
                </p>
              )}
            </section>
          </TableCell>
        </TableRow>
      ) : null}
    </>
  )
}
