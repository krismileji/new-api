import { describe, expect, it } from 'vitest'

import { orderGroupNames } from '../group-order'

describe('orderGroupNames', () => {
  it('keeps the pricing page usable when the configured order is null', () => {
    expect(() => orderGroupNames(['vip', 'default'], null)).not.toThrow()
    expect(orderGroupNames(['vip', 'default'], null)).toEqual([
      'default',
      'vip',
    ])
  })
})
