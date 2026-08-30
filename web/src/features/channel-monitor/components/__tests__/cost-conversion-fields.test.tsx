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

import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { useForm } from 'react-hook-form'
import { I18nextProvider } from 'react-i18next'
import { describe, test } from 'vitest'

import { Form } from '@/components/ui/form'

import type { UpstreamConfigFormValues } from '../../lib/schema'
import { ChannelMonitorCostConversionFields } from '../channel-monitor-cost-conversion-fields'

const noop = () => {}
const testI18n = createInstance()

await testI18n.init({
  lng: 'zhCN',
  resources: { zhCN: { translation: {} } },
})

function CostConversionFieldsFixture({
  mode = 'none',
}: {
  mode?: UpstreamConfigFormValues['costConversionMode']
} = {}) {
  const form = useForm<UpstreamConfigFormValues>({
    defaultValues: {
      costConversionMode: mode,
      rechargePaidCny: 1,
      rechargeCreditedUsd: 1,
      subscriptionPeriod: 'month',
      subscriptionPriceCny: 1,
      subscriptionDailyUsd: 1,
    },
  })

  return (
    <I18nextProvider i18n={testI18n}>
      <Form {...form}>
        <ChannelMonitorCostConversionFields
          form={form}
          upstreamRatio={1.25}
          onEditRatio={noop}
        />
      </Form>
    </I18nextProvider>
  )
}

describe('channel monitor cost conversion fields', () => {
  test('places manual upstream ratio editing beside the conversion summary', () => {
    const markup = renderToStaticMarkup(<CostConversionFieldsFixture />)

    assert.ok(markup.includes('倍率换算'))
    assert.ok(markup.includes('上游倍率'))
    assert.ok(markup.includes('aria-label="修改上游原始倍率"'))
  })

  test('uses inset focus rings for conversion amount inputs', () => {
    const markup = renderToStaticMarkup(
      <CostConversionFieldsFixture mode='recharge' />
    )

    const inputGroups = [
      ...markup.matchAll(/class="([^"]*group\/input-group[^"]*)"/g),
    ]
    assert.ok(inputGroups.length > 0)
    assert.ok(
      inputGroups.every(([, className]) => className.includes('ring-inset'))
    )
  })
})
