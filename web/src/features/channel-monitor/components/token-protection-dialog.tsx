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
import { useQuery } from '@tanstack/react-query'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import {
  getTokenProtectionSettings,
  tokenProtectionQueryKey,
} from '../lib/token-protection'
import { channelMonitorDialogContentClassName } from './channel-monitor-dialog-layout'
import { TokenProtectionRecords } from './token-protection-records'
import { TokenProtectionSettings } from './token-protection-settings'

export default function TokenProtectionDialog(props: {
  onOpenChange: (open: boolean) => void
}) {
  const settings = useQuery({
    queryKey: [...tokenProtectionQueryKey, 'settings'],
    queryFn: getTokenProtectionSettings,
    refetchOnWindowFocus: false,
  })
  return (
    <Dialog open onOpenChange={props.onOpenChange}>
      <DialogContent
        className={channelMonitorDialogContentClassName(
          'flex h-[min(52rem,calc(100dvh-2rem))] max-w-4xl flex-col sm:max-w-4xl'
        )}
      >
        <DialogHeader>
          <DialogTitle>API Key 自动禁用</DialogTitle>
          <DialogDescription>
            上游错误命中规则后，禁用对应用户 Key 并终止它的在途请求。
          </DialogDescription>
        </DialogHeader>
        <Tabs
          defaultValue='rules'
          className='flex min-h-0 flex-1 flex-col gap-4'
        >
          <TabsList>
            <TabsTrigger value='rules'>禁用规则</TabsTrigger>
            <TabsTrigger value='records'>禁用记录</TabsTrigger>
          </TabsList>
          <TabsContent value='rules' className='flex min-h-0 flex-1 flex-col'>
            {settings.isPending && <p role='status'>正在加载规则…</p>}
            {settings.isError && (
              <Alert variant='destructive'>
                <AlertDescription>
                  规则加载失败。
                  <Button
                    variant='outline'
                    onClick={() => void settings.refetch()}
                  >
                    重新加载
                  </Button>
                </AlertDescription>
              </Alert>
            )}
            {settings.data && (
              <TokenProtectionSettings
                key={settings.data.revision}
                settings={settings.data}
              />
            )}
          </TabsContent>
          <TabsContent value='records' className='flex min-h-0 flex-1 flex-col'>
            <TokenProtectionRecords />
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}
