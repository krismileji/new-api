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

import { renderToStaticMarkup } from 'react-dom/server'
import { useForm } from 'react-hook-form'

import { Form } from '@/components/ui/form'

import type { ChannelMonitorSettingsFormValues } from '../../lib/schema'
import { ChannelMonitorCostRetentionField } from '../channel-monitor-settings-dialog'

function CostRetentionFieldFixture() {
  const form = useForm<ChannelMonitorSettingsFormValues>({
    defaultValues: { costRetentionDays: 120 },
  })
  return (
    <Form {...form}>
      <ChannelMonitorCostRetentionField form={form} />
    </Form>
  )
}

describe('channel monitor settings dialog', () => {
  test('shows persisted cost retention days with bounded numeric input', () => {
    const markup = renderToStaticMarkup(<CostRetentionFieldFixture />)

    assert.ok(markup.includes('成本数据保留天数'))
    assert.match(markup, /type="number"[^>]*min="1"[^>]*max="3650"/)
    assert.match(markup, /value="120"/)
    assert.ok(markup.includes('删除后不可恢复'))
  })
})
