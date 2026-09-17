import { z } from 'zod'

export function probePolicyThresholdTokens(value: string): number | null {
  if (!/^\d+(?:\.\d{1,3})?$/.test(value.trim())) return null
  const [whole, fraction = ''] = value.trim().split('.')
  const tokens = Number(whole) * 1000 + Number(fraction.padEnd(3, '0'))
  return Number.isSafeInteger(tokens) && tokens >= 1 && tokens <= 1_000_000
    ? tokens
    : null
}

export const probePolicySchema = z
  .object({
    autoProbeDisabled: z.boolean(),
    smallInputResponseEnabled: z.boolean(),
    thresholdK: z.string(),
    responseText: z.string(),
  })
  .superRefine((values, ctx) => {
    if ([...values.responseText].length > 16_384) {
      ctx.addIssue({
        code: 'custom',
        path: ['responseText'],
        message: '响应内容不能超过 16384 个字符',
      })
    }
    if (!values.autoProbeDisabled || !values.smallInputResponseEnabled) return
    if (probePolicyThresholdTokens(values.thresholdK) === null) {
      ctx.addIssue({
        code: 'custom',
        path: ['thresholdK'],
        message: '请输入 0.001～1000 k，最多三位小数',
      })
    }
    if (!values.responseText.trim()) {
      ctx.addIssue({
        code: 'custom',
        path: ['responseText'],
        message: '请输入自定义响应内容',
      })
    }
  })

export type ProbePolicyFormValues = z.infer<typeof probePolicySchema>
