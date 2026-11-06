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
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowRight, BookOpen, Mail, Send } from 'lucide-react'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Button } from '@/components/ui/button'
import { useStatus } from '@/hooks/use-status'
import { normalizeInterfaceLanguage } from '@/i18n/languages'
import { api } from '@/lib/api'

const publicContentSchema = z.object({
  kind: z.enum(['home', 'pricing']),
  lang: z.enum(['en', 'zh-CN']),
  title: z.string(),
  heading: z.string(),
  lead: z.string(),
  sections: z.array(
    z.object({
      Title: z.string(),
      Text: z.string(),
      ReferenceLabel: z.string(),
      ReferenceURL: z.string(),
    })
  ),
  contactIntro: z.string(),
  contactLabel: z.string(),
  aboutLabel: z.string(),
  projectNote: z.string(),
  maintainerLabel: z.string(),
  updatedLabel: z.string(),
  updatedDate: z.string(),
  modifiedDate: z.string(),
  signupLabel: z.string(),
  pricingLabel: z.string(),
  docsLabel: z.string(),
})

export function SitePublicContent(props: { page: 'home' | 'pricing' }) {
  const { i18n } = useTranslation()
  const lang =
    normalizeInterfaceLanguage(i18n.language) === 'zhCN' ? 'zh-CN' : 'en'
  const { data } = useQuery({
    queryKey: ['site-public-content', props.page, lang],
    queryFn: async () => {
      const response = await api.get('/site-public-content.json', {
        params: { page: props.page, lang },
      })
      return publicContentSchema.parse(response.data)
    },
    initialData: () => {
      const inline = document.querySelector('#site-public-content')?.textContent
      if (!inline) return undefined
      try {
        const parsed = publicContentSchema.parse(JSON.parse(inline))
        return parsed.kind === props.page && parsed.lang === lang
          ? parsed
          : undefined
      } catch {
        return undefined
      }
    },
    staleTime: 5 * 60 * 1000,
    retry: 1,
  })

  useEffect(() => {
    if (data) document.title = data.title
  }, [data])

  if (!data) return null

  const ContentContainer = props.page === 'home' ? 'main' : 'div'

  return (
    <div data-site-public-ready>
      <ContentContainer className='mx-auto w-full max-w-6xl px-6 pt-28 pb-14 md:pt-36 md:pb-20'>
        <div className='max-w-3xl'>
          <p className='text-primary mb-4 text-sm font-semibold'>Coder Proxy</p>
          <h1 className='text-4xl leading-tight font-bold md:text-5xl'>
            {data.heading}
          </h1>
          <p className='text-muted-foreground mt-6 text-base leading-8 md:text-lg'>
            {data.lead}
          </p>
          {data.modifiedDate && (
            <p className='text-muted-foreground mt-4 text-sm'>
              {data.maintainerLabel}: Coder Proxy ·{' '}
              <time dateTime={data.modifiedDate}>
                {data.updatedLabel} {data.updatedDate}
              </time>
            </p>
          )}
          {props.page === 'home' && (
            <div className='mt-8 flex flex-wrap gap-3'>
              <Button size='lg' render={<Link to='/sign-up' />}>
                {data.signupLabel}
                <ArrowRight className='size-4' />
              </Button>
              <Button
                variant='outline'
                size='lg'
                render={
                  <a
                    href={`/pricing${lang === 'zh-CN' ? '?lang=zh-CN' : ''}`}
                  />
                }
              >
                {data.pricingLabel}
              </Button>
              <Button
                variant='outline'
                size='lg'
                render={<a href='https://docs.newapi.pro/' />}
              >
                <BookOpen className='size-4' />
                {data.docsLabel}
              </Button>
            </div>
          )}
        </div>

        <div className='border-border mt-16 grid gap-x-10 gap-y-10 border-t pt-10 md:grid-cols-2'>
          {data.sections.map((section) => (
            <section key={section.Title} className='max-w-xl'>
              <h2 className='text-xl font-semibold'>{section.Title}</h2>
              <p className='text-muted-foreground mt-3 leading-7'>
                {section.Text}
              </p>
              {section.ReferenceURL && (
                <a
                  className='text-primary mt-3 inline-block text-sm hover:underline'
                  href={section.ReferenceURL}
                >
                  {section.ReferenceLabel}
                </a>
              )}
            </section>
          ))}
        </div>

        <section className='border-border mt-14 border-t pt-8'>
          <h2 className='text-lg font-semibold'>{data.contactLabel}</h2>
          <p className='text-muted-foreground mt-2'>{data.contactIntro}</p>
          <div className='mt-4 flex flex-wrap gap-x-8 gap-y-3 text-sm'>
            <a
              className='text-primary inline-flex items-center gap-2 hover:underline'
              href='https://t.me/krismileji'
            >
              <Send className='size-4' />
              @krismileji
            </a>
            <a
              className='text-primary inline-flex items-center gap-2 hover:underline'
              href='mailto:krismiledev@gmail.com'
            >
              <Mail className='size-4' />
              krismiledev@gmail.com
            </a>
          </div>
          <div className='mt-5 flex flex-wrap gap-6 text-sm'>
            <a
              className='text-primary hover:underline'
              href={`/about${lang === 'zh-CN' ? '?lang=zh-CN' : ''}`}
            >
              {data.aboutLabel}
            </a>
            <a
              className='text-primary hover:underline'
              href={`/contact${lang === 'zh-CN' ? '?lang=zh-CN' : ''}`}
            >
              {data.contactLabel}
            </a>
          </div>
        </section>

        {props.page === 'home' && (
          <p className='text-muted-foreground mt-12 text-sm'>
            {data.projectNote}{' '}
            <a
              className='underline underline-offset-4'
              href='https://github.com/QuantumNous/new-api'
            >
              New API · QuantumNous
            </a>
          </p>
        )}
      </ContentContainer>
    </div>
  )
}

export function SitePublicFooter() {
  const { status } = useStatus()

  return (
    <footer className='border-border border-t px-6 py-8 text-sm'>
      <div className='text-muted-foreground mx-auto flex max-w-6xl flex-wrap items-center gap-x-6 gap-y-3'>
        <span>Coder Proxy</span>
        {status?.user_agreement_enabled && (
          <a className='hover:underline' href='/user-agreement'>
            用户协议
          </a>
        )}
        {status?.privacy_policy_enabled && (
          <a className='hover:underline' href='/privacy-policy'>
            隐私政策
          </a>
        )}
        <a
          className='hover:underline'
          href='https://github.com/QuantumNous/new-api'
        >
          New API · QuantumNous
        </a>
      </div>
    </footer>
  )
}
