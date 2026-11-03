import { describe, expect, it } from 'vitest'

import { siteHomeLanguageURL } from '../site-identity'

describe('Coder Proxy home language URLs', () => {
  it('switches English and Chinese to distinct URLs while keeping unrelated query values', () => {
    const site = 'https://coder.work-proxy.com'
    expect(
      siteHomeLanguageURL('https://coder.work-proxy.com/?aff=abc', 'zhCN', site)
    ).toBe('https://coder.work-proxy.com/?aff=abc&lang=zh-CN')
    expect(
      siteHomeLanguageURL(
        'https://coder.work-proxy.com/?aff=abc&lang=zh-CN',
        'en',
        site
      )
    ).toBe('https://coder.work-proxy.com/?aff=abc')
  })

  it('does not rewrite other languages, pages, or deployments', () => {
    const site = 'https://coder.work-proxy.com'
    expect(siteHomeLanguageURL(`${site}/`, 'fr', site)).toBeNull()
    expect(siteHomeLanguageURL(`${site}/pricing`, 'zhCN', site)).toBeNull()
    expect(
      siteHomeLanguageURL(`${site}/`, 'zhCN', 'https://other.site')
    ).toBeNull()
    expect(siteHomeLanguageURL(`${site}/?lang=zh-CN`, 'zhCN', site)).toBeNull()
  })
})
