import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { formatTimestampToDate } from '@/lib/format'

import type {
  ChannelMonitorFailureCategory,
  ChannelMonitorSuccessMode,
} from '../types'

export function ChannelMonitorAnalyticsFailures(props: {
  categories: readonly ChannelMonitorFailureCategory[]
  mode: ChannelMonitorSuccessMode
  failureCount: number
  truncated?: boolean
}) {
  const categories = props.categories.filter((item) =>
    props.mode === 'final' ? item.final_count > 0 : item.actual_count > 0
  )
  return (
    <section className='flex shrink-0 flex-col gap-2' aria-label='失败报错分类'>
      <h3 className='text-sm font-medium'>失败报错分类</h3>
      <Table aria-label='失败报错分类' className='min-w-[42rem]'>
        <TableHeader>
          <TableRow>
            <TableHead scope='col'>错误分类</TableHead>
            <TableHead scope='col'>报错示例</TableHead>
            <TableHead scope='col' className='text-right'>
              失败次数
            </TableHead>
            <TableHead scope='col' className='text-right'>
              失败占比
            </TableHead>
            <TableHead scope='col'>最近发生</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {categories.map((category) => {
            const count =
              props.mode === 'final'
                ? category.final_count
                : category.actual_count
            return (
              <TableRow
                key={`${category.channel_id}:${category.status_code}:${category.error_type}:${category.error_code}:${category.sample_content}`}
              >
                <TableCell>
                  <div className='flex max-w-64 flex-wrap gap-1'>
                    {category.status_code > 0 ? (
                      <Badge variant='outline'>
                        HTTP {category.status_code}
                      </Badge>
                    ) : null}
                    {category.error_code ? (
                      <Badge
                        variant='secondary'
                        className='max-w-full'
                        title={category.error_code}
                      >
                        <span className='truncate'>{category.error_code}</span>
                      </Badge>
                    ) : null}
                    {category.error_type ? (
                      <Badge
                        variant='outline'
                        className='max-w-full'
                        title={category.error_type}
                      >
                        <span className='truncate'>{category.error_type}</span>
                      </Badge>
                    ) : null}
                    {!category.status_code &&
                    !category.error_code &&
                    !category.error_type
                      ? '其他错误'
                      : null}
                  </div>
                </TableCell>
                <TableCell>
                  <p
                    className='line-clamp-2 max-w-96 break-words whitespace-normal'
                    title={category.sample_content}
                  >
                    {category.sample_content || '未记录错误内容'}
                  </p>
                </TableCell>
                <TableCell className='text-right font-mono'>
                  {count} 次
                  {props.mode === 'actual' &&
                  category.actual_count !== category.final_count ? (
                    <span className='text-muted-foreground block text-xs'>
                      最终失败 {category.final_count} 次
                    </span>
                  ) : null}
                </TableCell>
                <TableCell className='text-right font-mono'>
                  {props.failureCount > 0
                    ? `${((count / props.failureCount) * 100).toFixed(1)}%`
                    : '-'}
                </TableCell>
                <TableCell>
                  {category.last_occurred_at > 0
                    ? formatTimestampToDate(category.last_occurred_at)
                    : '-'}
                </TableCell>
              </TableRow>
            )
          })}
          {categories.length === 0 ? (
            <TableRow>
              <TableCell
                colSpan={5}
                className='text-muted-foreground text-center'
              >
                没有失败报错
              </TableCell>
            </TableRow>
          ) : null}
        </TableBody>
      </Table>
      {props.truncated ? (
        <p className='text-muted-foreground text-xs'>
          仅显示部分错误分类，请缩小统计范围。
        </p>
      ) : null}
    </section>
  )
}
