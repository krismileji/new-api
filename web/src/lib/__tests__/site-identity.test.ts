import { describe, expect, it } from 'vitest'

import { sitePublicLanguageURL } from '../site-identity'

describe('Coder Proxy public language URLs', () => {
  it('switches English and Chinese to distinct URLs while keeping unrelated query values', () => {
    const site = 'https://coder.work-proxy.com'
    expect(
      sitePublicLanguageURL(
        'https://coder.work-proxy.com/?aff=abc',
        'zhCN',
        site
      )
    ).toBe('https://coder.work-proxy.com/?aff=abc&lang=zh-CN')
    expect(
      sitePublicLanguageURL(
        'https://coder.work-proxy.com/?aff=abc&lang=zh-CN',
        'en',
        site
      )
    ).toBe('https://coder.work-proxy.com/?aff=abc')
  })

  it('keeps the pricing page path when switching its canonical language', () => {
    const site = 'https://coder.work-proxy.com'
    expect(sitePublicLanguageURL(`${site}/pricing`, 'zhCN', site)).toBe(
      `${site}/pricing?lang=zh-CN`
    )
    expect(
      sitePublicLanguageURL(`${site}/pricing?lang=zh-CN`, 'en', site)
    ).toBe(`${site}/pricing`)
    expect(sitePublicLanguageURL(`${site}/pricing/`, 'zhCN', site)).toBe(
      `${site}/pricing?lang=zh-CN`
    )
  })

  it('does not rewrite other languages, pages, or deployments', () => {
    const site = 'https://coder.work-proxy.com'
    expect(sitePublicLanguageURL(`${site}/`, 'fr', site)).toBeNull()
    expect(sitePublicLanguageURL(`${site}/about`, 'zhCN', site)).toBeNull()
    expect(
      sitePublicLanguageURL(`${site}/`, 'zhCN', 'https://other.site')
    ).toBeNull()
    expect(
      sitePublicLanguageURL(`${site}/?lang=zh-CN`, 'zhCN', site)
    ).toBeNull()
  })
})
