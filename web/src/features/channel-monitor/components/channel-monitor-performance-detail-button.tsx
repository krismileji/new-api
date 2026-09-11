import { ViewIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import type { ReactNode } from 'react'

import { Button } from '@/components/ui/button'

export function ChannelMonitorPerformanceDetailButton(props: {
  children: ReactNode
  label: string
  onClick: () => void
  disabled?: boolean
}) {
  return (
    <Button
      type='button'
      variant='ghost'
      className='h-auto min-w-0 justify-start gap-1 px-1 py-0.5 text-left whitespace-normal'
      onClick={props.onClick}
      disabled={props.disabled}
      aria-label={props.label}
      aria-haspopup='dialog'
      title={props.label}
    >
      {props.children}
      <HugeiconsIcon icon={ViewIcon} data-icon='inline-end' />
    </Button>
  )
}
