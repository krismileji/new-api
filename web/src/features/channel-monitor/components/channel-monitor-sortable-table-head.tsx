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
import {
  ArrowDown01Icon,
  ArrowUp01Icon,
  ArrowUpDownIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import type { ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { TableHead } from '@/components/ui/table'
import { cn } from '@/lib/utils'

export type ChannelMonitorSortDirection = 'asc' | 'desc'

type ChannelMonitorSortableTableHeadProps = {
  label: ReactNode
  direction?: ChannelMonitorSortDirection
  onSort: () => void
  align?: 'left' | 'right'
  className?: string
}

type ChannelMonitorSortButtonProps = Omit<
  ChannelMonitorSortableTableHeadProps,
  'className'
> & {
  className?: string
}

export function ChannelMonitorSortButton(props: ChannelMonitorSortButtonProps) {
  let directionLabel = '未排序'
  let sortIcon = ArrowUpDownIcon
  if (props.direction === 'asc') {
    directionLabel = '升序'
    sortIcon = ArrowUp01Icon
  } else if (props.direction === 'desc') {
    directionLabel = '降序'
    sortIcon = ArrowDown01Icon
  }

  return (
    <Button
      type='button'
      variant='ghost'
      size='sm'
      className={cn(
        'h-auto min-h-7 w-full py-1 whitespace-nowrap',
        props.align === 'right'
          ? 'justify-end text-right'
          : 'justify-start text-left',
        props.className
      )}
      aria-label={`按${String(props.label)}排序（当前${directionLabel}）`}
      onClick={props.onSort}
    >
      <span className='min-w-0 whitespace-nowrap'>{props.label}</span>
      <HugeiconsIcon
        icon={sortIcon}
        className='text-muted-foreground'
        data-icon='inline-end'
        aria-hidden='true'
      />
    </Button>
  )
}

export function ChannelMonitorSortableTableHead(
  props: ChannelMonitorSortableTableHeadProps
) {
  let ariaSort: 'ascending' | 'descending' | 'none' = 'none'
  if (props.direction === 'asc') ariaSort = 'ascending'
  if (props.direction === 'desc') ariaSort = 'descending'

  return (
    <TableHead aria-sort={ariaSort} className={cn('p-0', props.className)}>
      <ChannelMonitorSortButton {...props} />
    </TableHead>
  )
}
