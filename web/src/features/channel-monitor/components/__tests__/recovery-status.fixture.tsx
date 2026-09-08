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
import fs from 'node:fs'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

import { renderToStaticMarkup } from 'react-dom/server'

import type { ChannelMonitorRecovery } from '../../types-recovery'
import { ChannelMonitorRecoverySummary } from '../channel-monitor-health-status'
import { ChannelMonitorPageLayout } from '../channel-monitor-page-layout'

const target = process.argv[2]
if (!target) throw new Error('Output directory is required')
const cssDirectory = path.resolve('dist/static/css')
const stylesheet = fs
  .readdirSync(cssDirectory)
  .find((name) => /^index\..*\.css$/.test(name))
if (!stylesheet) {
  throw new Error('Build frontend CSS before rendering this fixture')
}
const states: ChannelMonitorRecovery[] = [
  {
    status: 'degraded',
    recovery_status: 'recovering',
    node_id: 'monitor-node-with-a-long-instance-name-1234567890',
    checked_at: 1788856200,
    recovered_at: 0,
    pending_count: 128,
    message: '正在自动恢复',
    action: '后台正在处理积压事件，请留意待处理数量和最近进展。',
    data_gap_reasons: [],
  },
  {
    status: 'healthy',
    recovery_status: 'data_incomplete',
    node_id: 'monitor-node-a',
    checked_at: 1788856200,
    recovered_at: 1788856100,
    pending_count: 0,
    message: '运行正常，部分历史统计不完整',
    action: '请复核丢弃或隔离记录；运行恢复不代表历史数据已补齐。',
    data_gap_reasons: ['samples_dropped'],
  },
  {
    status: 'unavailable',
    recovery_status: 'manual_required',
    node_id: 'monitor-node-a',
    checked_at: 1788856200,
    recovered_at: 0,
    pending_count: 1024,
    message: '需要人工处理',
    action: '请检查 Redis、监控后台任务和数据库连接。',
    data_gap_reasons: [],
    notification_error: '邮件发送失败，将自动重试',
  },
]
fs.mkdirSync(target, { recursive: true })
for (const state of states) {
  const markup = renderToStaticMarkup(
    <ChannelMonitorPageLayout
      actions={null}
      realtimeStatus={<ChannelMonitorRecoverySummary data={state} />}
    >
      <div className='text-muted-foreground border-t pt-4 text-sm'>
        渠道列表
      </div>
    </ChannelMonitorPageLayout>
  )
  fs.writeFileSync(
    path.join(target, `recovery-${state.recovery_status}.html`),
    `<!doctype html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="${pathToFileURL(path.join(cssDirectory, stylesheet)).href}"><title>渠道监控恢复状态</title></head><body style="margin:0;height:100vh;background:#fff;color:#171717"><main style="height:100%;padding:16px">${markup}</main></body></html>`
  )
}
