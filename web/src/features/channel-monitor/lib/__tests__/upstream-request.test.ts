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

import { createChannelMonitorCustomFormConfig } from '../custom-upstream'
import type { UpstreamConfigFormValues } from '../schema'
import { createChannelMonitorUpstreamRequest } from '../upstream-request'

const values: UpstreamConfigFormValues = {
  upstreamType: 'sub2api',
  baseUrl: ' https://upstream.example ',
  group: ' vip ',
  authType: 'refresh_token',
  userId: 0,
  accessToken: ' access-token ',
  refreshToken: ' refresh-token ',
  account: '',
  password: '',
  singleChannelAction: 'none',
  multipleChannelsAction: 'none',
  ratioSyncEnabled: true,
  balanceSyncEnabled: true,
  balanceWarningThreshold: null,
  balanceAutoDisableThreshold: null,
  costConversionMode: 'none',
  rechargePaidCny: 1,
  rechargeCreditedUsd: 1,
  subscriptionPeriod: 'month',
  subscriptionPriceCny: 1,
  subscriptionDailyUsd: 1,
  customConfig: createChannelMonitorCustomFormConfig(undefined),
}

describe('Sub2API credential test requests', () => {
  test('manual Token test sends only the manual Token credential', () => {
    const request = createChannelMonitorUpstreamRequest(values, 'token')

    assert.equal(request.auth_type, 'token')
    assert.equal(request.access_token, 'access-token')
  })

  test('Refresh Token test sends only the Refresh Token credential', () => {
    const request = createChannelMonitorUpstreamRequest(values, 'refresh_token')

    assert.equal(request.auth_type, 'refresh_token')
    assert.equal(request.access_token, 'refresh-token')
  })
})
