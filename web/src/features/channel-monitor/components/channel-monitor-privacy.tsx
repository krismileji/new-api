import { ViewIcon, ViewOffSlashIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import type { ReactNode } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { useChannelMonitorPrivacyStore } from '@/stores/channel-monitor-privacy-store'

import {
  ChannelMonitorPrivacyContext,
  useChannelMonitorPrivacy,
} from '../hooks/use-channel-monitor-privacy'

export function ChannelMonitorPrivacyProvider(props: { children: ReactNode }) {
  const enabled = useChannelMonitorPrivacyStore((state) => state.enabled)
  return (
    <ChannelMonitorPrivacyContext value={enabled}>
      {/* 通知通过全局 Portal 渲染，异步回调也可能带回上游错误或金额。 */}
      {enabled ? (
        <style>{'[data-sonner-toaster] { display: none !important; }'}</style>
      ) : null}
      {props.children}
    </ChannelMonitorPrivacyContext>
  )
}

export function ChannelMonitorPrivate(props: {
  children: ReactNode
  fallback?: ReactNode
}) {
  const enabled = useChannelMonitorPrivacy()
  if (!enabled) return props.children
  if (props.fallback !== undefined) return props.fallback
  return (
    <span
      className='text-muted-foreground select-none'
      aria-label='敏感信息已隐藏'
    >
      ••••
    </span>
  )
}

export function ChannelMonitorPrivacyToggle() {
  const enabled = useChannelMonitorPrivacyStore((state) => state.enabled)
  const setEnabled = useChannelMonitorPrivacyStore((state) => state.setEnabled)
  const label = enabled ? '显示敏感信息' : '隐藏敏感信息'
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type='button'
            variant={enabled ? 'secondary' : 'outline'}
            size='icon'
            aria-label={label}
            aria-pressed={enabled}
            onClick={() => {
              if (!enabled) toast.dismiss()
              setEnabled(!enabled)
            }}
          >
            <HugeiconsIcon
              icon={enabled ? ViewOffSlashIcon : ViewIcon}
              aria-hidden='true'
            />
          </Button>
        }
      />
      <TooltipContent>{label} · 截图隐私模式</TooltipContent>
    </Tooltip>
  )
}

export function ChannelMonitorPrivacyNotice() {
  return (
    <p className='text-muted-foreground rounded-lg border p-6 text-center text-sm'>
      截图隐私模式已收起敏感详情，点击顶部眼睛图标可恢复显示。
    </p>
  )
}

export function ChannelMonitorPrivacyDialogGuard(props: {
  children: ReactNode
  open: boolean
  onClose: () => void
}) {
  const enabled = useChannelMonitorPrivacy()
  if (!enabled) return props.children
  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>敏感详情已隐藏</DialogTitle>
          <DialogDescription>
            截图隐私模式下不展示配置、运行记录及用户明细。返回后点击顶部眼睛图标可恢复显示。
          </DialogDescription>
        </DialogHeader>
        <Button onClick={props.onClose}>返回监控</Button>
      </DialogContent>
    </Dialog>
  )
}
