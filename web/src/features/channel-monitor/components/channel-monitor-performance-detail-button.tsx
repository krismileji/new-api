import { ViewIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import type { ReactNode } from 'react'

import { Button } from '@/components/ui/button'

import { useChannelMonitorPrivacy } from '../hooks/use-channel-monitor-privacy'

export function ChannelMonitorPerformanceDetailButton(props: {
  children: ReactNode
  label: string
  onClick: () => void
  disabled?: boolean
}) {
  const privateMode = useChannelMonitorPrivacy()
  const label = privateMode ? '查看性能明细' : props.label
  return (
    <Button
      type='button'
      variant='ghost'
      className='h-auto min-w-0 justify-start gap-1 px-1 py-0.5 text-left whitespace-normal'
      onClick={props.onClick}
      disabled={props.disabled}
      aria-label={label}
      aria-haspopup='dialog'
      title={label}
    >
      {props.children}
      <HugeiconsIcon icon={ViewIcon} data-icon='inline-end' />
    </Button>
  )
}
