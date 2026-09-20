import { z } from 'zod'

/** Mirrors backend/internal/handlers/customers.go. */
export const BUYER_REGISTRATION_TYPES = ['Registered', 'Unregistered'] as const

const PHONE_RE = /^[0-9+ ]{1,30}$/
const NTN_RE = /^[0-9-]{1,20}$/
const CNIC_RE = /^[0-9]{1,15}$/
const MONEY_RE = /^\d+(\.\d{1,2})?$/

export const customerDialogSchema = z
  .object({
    name: z.string().trim().min(1, 'Name is required').max(120, 'At most 120 characters'),
    phone: z.string().trim(),
    ntn: z.string().trim(),
    cnic: z.string().trim(),
    buyer_registration_type: z.enum(BUYER_REGISTRATION_TYPES),
    address: z.string().trim(),
    province: z.string().trim().max(40, 'At most 40 characters'),
    credit_allowed: z.boolean(),
    credit_limit: z.string().trim(),
    notes: z.string().trim(),
    is_active: z.boolean(),
  })
  .superRefine((v, ctx) => {
    if (v.phone !== '' && !PHONE_RE.test(v.phone)) {
      ctx.addIssue({ code: z.ZodIssueCode.custom, path: ['phone'], message: 'Digits, + or spaces, at most 30 characters' })
    }
    if (v.ntn !== '' && !NTN_RE.test(v.ntn)) {
      ctx.addIssue({ code: z.ZodIssueCode.custom, path: ['ntn'], message: 'Digits or dashes, at most 20 characters' })
    }
    if (v.cnic !== '' && !CNIC_RE.test(v.cnic)) {
      ctx.addIssue({ code: z.ZodIssueCode.custom, path: ['cnic'], message: 'Digits only, at most 15 characters' })
    }
    if (v.credit_limit !== '' && (!MONEY_RE.test(v.credit_limit) || Number(v.credit_limit) < 0)) {
      ctx.addIssue({ code: z.ZodIssueCode.custom, path: ['credit_limit'], message: 'Zero or more, at most 2 decimal places' })
    }
  })
export type CustomerDialogValues = z.infer<typeof customerDialogSchema>
