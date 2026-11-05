export function isCoderProxySite(address: unknown): boolean {
  return (
    typeof address === 'string' &&
    address.replace(/\/$/, '') === 'https://coder.work-proxy.com'
  )
}

export function sitePublicLanguageURL(
  currentURL: string,
  code: string,
  address: unknown
): string | null {
  if (!isCoderProxySite(address) || (code !== 'en' && code !== 'zhCN')) {
    return null
  }
  const nextURL = new URL(currentURL)
  if (!['/', '/pricing', '/pricing/'].includes(nextURL.pathname)) return null
  if (nextURL.pathname === '/pricing/') nextURL.pathname = '/pricing'
  nextURL.searchParams.delete('lang')
  if (code === 'zhCN') nextURL.searchParams.set('lang', 'zh-CN')
  return nextURL.href === currentURL ? null : nextURL.href
}
