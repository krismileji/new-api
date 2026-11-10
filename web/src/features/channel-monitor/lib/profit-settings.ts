import { z } from 'zod'

export const channelMonitorProfitSettingsSchema = z.object({
  apiKeyIds: z
    .array(z.string())
    .max(1000, '最多配置 1000 个自用 API Key')
    .refine(
      (values) =>
        values.every(
          (value) =>
            /^\d+$/.test(value) &&
            Number(value) >= 1 &&
            Number(value) <= 2147483647
        ),
      'API Key ID 必须为 1–2147483647 的整数'
    ),
})
