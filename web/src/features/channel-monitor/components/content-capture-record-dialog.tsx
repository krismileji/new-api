import { useQuery } from '@tanstack/react-query'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { formatNumber, formatTimestamp } from '@/lib/format'

import {
  contentCaptureQueryKey,
  getContentCaptureRecord,
} from '../lib/content-capture'

function prettyBody(body: string): string {
  try {
    return JSON.stringify(JSON.parse(body), null, 2)
  } catch {
    return body
  }
}

function CapturedBody(props: {
  label: string
  body: string
  size: number
  truncated?: boolean
  raw?: boolean
}) {
  const text = props.raw ? prettyBody(props.body) : props.body
  return (
    <section className='space-y-2'>
      <div className='flex items-center justify-between gap-2'>
        <h3 className='font-medium'>
          {props.label}
          <span className='text-muted-foreground ml-2 text-xs font-normal'>
            {props.size >= 0
              ? `原始报文 ${formatNumber(props.size)} 字节`
              : '原始报文大小未知'}
            {props.truncated && '，内容不完整'}
          </span>
        </h3>
        {props.body && (
          <CopyButton
            value={props.body}
            variant='outline'
            size='sm'
            aria-label={`复制${props.label}`}
          >
            复制
          </CopyButton>
        )}
      </div>
      <pre className='bg-muted max-h-96 overflow-auto rounded-md p-3 text-xs break-words whitespace-pre-wrap'>
        {text || '（空）'}
      </pre>
    </section>
  )
}

export function ContentCaptureRecordDialog(props: {
  id: string
  onClose: () => void
}) {
  const record = useQuery({
    queryKey: [...contentCaptureQueryKey, 'record', props.id],
    queryFn: ({ signal }) => getContentCaptureRecord(props.id, signal),
    retry: false,
  })
  const data = record.data
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title='请求内容'
      description={props.id}
      contentClassName='sm:max-w-5xl'
      contentHeight='min(48rem, calc(100dvh - 8rem))'
      bodyClassName='space-y-4'
    >
      {record.isPending && <LoadingState message='正在加载请求内容…' />}
      {record.isError && (
        <ErrorState
          title='请求内容加载失败'
          description='记录可能已被新请求覆盖，或抓取已关闭并清空。'
          onRetry={() => void record.refetch()}
        />
      )}
      {data && (
        <>
          <dl className='grid grid-cols-2 gap-3 text-sm sm:grid-cols-4'>
            {(
              [
                ['时间', formatTimestamp(data.created_at)],
                ['用户', `${data.username || '-'}（#${data.user_id}）`],
                ['API Key', `${data.token_name || '-'}（#${data.token_id}）`],
                ['分组', data.group || '-'],
                ['模型', data.model || '-'],
                ['渠道', data.channel_id ? `#${data.channel_id}` : '-'],
                ['IP', data.ip || '-'],
                [
                  '状态',
                  `${data.status_code}${data.is_stream ? '，流式' : ''}，${formatNumber(data.duration_ms)} ms`,
                ],
              ] as const
            ).map(([label, value]) => (
              <div key={label}>
                <dt className='text-muted-foreground text-xs'>{label}</dt>
                <dd className='mt-1 break-all'>{value}</dd>
              </div>
            ))}
          </dl>
          {data.content_extracted && (
            <>
              <CapturedBody
                label='本轮用户输入'
                body={data.request_text ?? ''}
                size={data.request_size}
                truncated={data.request_text_truncated}
              />
              <CapturedBody
                label='模型回复'
                body={data.response_text ?? ''}
                size={data.response_size}
                truncated={data.response_text_truncated}
              />
            </>
          )}
          {(data.request_body || data.response_body) && (
            <Collapsible defaultOpen={!data.content_extracted}>
              <CollapsibleTrigger
                render={<Button variant='outline' size='sm' />}
              >
                原始报文
              </CollapsibleTrigger>
              <CollapsibleContent className='space-y-4 pt-3'>
                <CapturedBody
                  raw
                  label='原始请求'
                  body={data.request_body}
                  size={data.request_size}
                  truncated={data.request_truncated}
                />
                <CapturedBody
                  raw
                  label='原始响应'
                  body={data.response_body}
                  size={data.response_size}
                  truncated={data.response_truncated}
                />
              </CollapsibleContent>
            </Collapsible>
          )}
        </>
      )}
    </Dialog>
  )
}
