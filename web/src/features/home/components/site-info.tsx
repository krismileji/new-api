import { useTranslation } from 'react-i18next'

import { useStatus } from '@/hooks/use-status'
import { normalizeInterfaceLanguage } from '@/i18n/languages'
import { isCoderProxySite } from '@/lib/site-identity'

export function SiteInfo(props: { variant?: 'home' | 'about' }) {
  const { i18n } = useTranslation()
  const { status } = useStatus()
  if (!isCoderProxySite(status?.server_address)) {
    return null
  }

  const chinese = normalizeInterfaceLanguage(i18n.language) === 'zhCN'
  const about = props.variant === 'about'
  const heading = chinese ? '关于 Coder Proxy' : 'About Coder Proxy'
  const summary = chinese
    ? 'Coder Proxy 是面向开发者和应用的 AI 聚合平台，通过统一 API 接入本站当前支持的模型，并集中管理密钥、用量和费用。'
    : 'Coder Proxy is an AI aggregation platform for developers and applications. Access currently supported models through a unified API and manage credentials, usage, and costs in one place.'
  const details = chinese
    ? '模型、能力与价格可能随服务配置变化，使用前请查看实时定价页和接入文档。'
    : 'Available models, capabilities, and prices may change with the service configuration. Check the live pricing page and integration documentation before use.'
  const languageQuery = chinese ? '?lang=zh-CN' : ''

  return (
    <section className='border-border/40 border-t px-6 py-16 md:py-20'>
      <div className='mx-auto grid max-w-6xl gap-10 md:grid-cols-[minmax(0,2fr)_minmax(230px,1fr)]'>
        <div className='space-y-4'>
          {about ? (
            <h1 className='text-3xl font-semibold'>{heading}</h1>
          ) : (
            <h2 className='text-2xl font-semibold'>{heading}</h2>
          )}
          <p className='text-muted-foreground max-w-2xl leading-7'>{summary}</p>
          <p className='text-muted-foreground max-w-2xl leading-7'>{details}</p>
          <div className='flex flex-wrap gap-x-6 gap-y-2 text-sm'>
            <a
              className='text-primary hover:underline'
              href={`/about${languageQuery}`}
            >
              {chinese ? '了解平台' : 'About the platform'}
            </a>
            <a className='text-primary hover:underline' href='/pricing'>
              {chinese ? '查看当前价格' : 'View current pricing'}
            </a>
            <a
              className='text-primary hover:underline'
              href='https://docs.newapi.pro/'
            >
              {chinese ? '接入文档' : 'Integration documentation'}
            </a>
          </div>
        </div>
        <div className='space-y-3 text-sm'>
          <h2 className='text-base font-semibold'>
            {chinese ? '联系我们' : 'Contact'}
          </h2>
          <p className='text-muted-foreground'>
            {chinese
              ? '有关接入、账户或计费问题，可通过以下渠道联系。'
              : 'For integration, account, or billing questions, reach us here.'}
          </p>
          <p>
            Telegram:{' '}
            <a
              className='text-primary hover:underline'
              href='https://t.me/krismileji'
            >
              @krismileji
            </a>
          </p>
          <p>
            Email:{' '}
            <a
              className='text-primary hover:underline'
              href='mailto:krismiledev@gmail.com'
            >
              krismiledev@gmail.com
            </a>
          </p>
          <p>
            <a
              className='text-primary hover:underline'
              href={`/contact${languageQuery}`}
            >
              {chinese ? '联系页面' : 'Contact page'}
            </a>
          </p>
        </div>
      </div>
    </section>
  )
}
