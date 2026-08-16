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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { renderAuditContent } from '../format'

const passthrough = (key: string) => key

describe('channel monitor audit content', () => {
  test('renders retained configuration changes as readable Chinese', () => {
    assert.equal(
      renderAuditContent(
        {
          op: {
            action: 'channel.monitor_smart_schedule_group_pause_update',
            params: {
              id: 7,
              group: 'vip',
              model: 'gpt-test',
              duration_minutes: 30,
            },
          },
        },
        passthrough
      ),
      '已将渠道 7 在分组 vip、模型 gpt-test 的流量暂停时间更新为 30 分钟'
    )

    assert.equal(
      renderAuditContent(
        {
          op: {
            action: 'channel.model_detection_config_update',
            params: {
              channel_id: 7,
              schedule_status: '关闭',
              target_count: 3,
            },
          },
        },
        passthrough
      ),
      '已更新渠道 7 的模型检测配置（定时检测：关闭，3 个目标）'
    )

    assert.equal(
      renderAuditContent(
        {
          op: {
            action: 'channel.monitor_settings_changed',
            params: {
              auto_update_interval_minutes: 15,
              smart_schedule_status: '开启',
              email_notification_status: '关闭',
              probe_response_status: '开启',
            },
          },
        },
        passthrough
      ),
      '已更新渠道监控设置（自动倍率更新间隔 15 分钟，智能调度：开启，邮件通知：关闭，本地探针：开启）'
    )
  })

  test('renders historical actions that previously appeared blank', () => {
    assert.equal(
      renderAuditContent(
        {
          op: {
            action: 'channel.monitor_group_channels_update',
            params: { group: 'vip', added_count: 2, removed_count: 1 },
          },
        },
        passthrough
      ),
      '已更新分组 vip 的关联渠道（新增 2 个，移除 1 个）'
    )

    assert.equal(
      renderAuditContent(
        {
          op: {
            action: 'channel.status_probe_run',
            params: { channel_id: 7, manual_request_id: 'probe-1' },
          },
        },
        passthrough
      ),
      '已请求立即探测渠道 7（请求 probe-1）'
    )

    assert.equal(
      renderAuditContent(
        {
          op: {
            action: 'channel.status_update',
            params: { id: 7, status: 2 },
          },
        },
        passthrough
      ),
      '已将渠道 7 的状态更新为 2'
    )

    assert.equal(
      renderAuditContent(
        {
          op: {
            action: 'channel.monitor_settings_update',
            params: {},
          },
        },
        passthrough
      ),
      '已更新渠道监控设置'
    )
  })
})
