import { z } from 'zod'

/** Mirrors backend/internal/handlers/customers.go. */
export const BUYER_REGISTRATION_TYPES = ['Registered', 'Unregistered'] as const

/**
 * FBR's /pdi/v1/provinces list as the gateway spells it (mirrors
 * backend/internal/fiscal/provinces.go). A buyer province outside this
 * list is refused with 0074 when the sale is filed, so the form offers
 * exactly these and nothing typed. Blank means the store's own province.
 */
export const FBR_PROVINCES = [
  'PUNJAB',
  'SINDH',
  'KHYBER PAKHTUNKHWA',
  'BALOCHISTAN',
  'CAPITAL TERRITORY',
  'AZAD JAMMU AND KASHMIR',
  'GILGIT BALTISTAN',
] as const
export type FbrProvince = (typeof FBR_PROVINCES)[number]

/**
 * Maps a stored province onto FBR's spelling, ignoring case and spacing, so
 * a row saved as "Punjab" before the list existed still selects PUNJAB in
 * the dialog. Anything FBR would not know ("ICT") comes back blank and has
 * to be picked again.
 */
export function canonicalProvince(stored: string | null | undefined): FbrProvince | '' {
  const want = (stored ?? '').trim().toUpperCase().split(/\s+/).join(' ')
  return FBR_PROVINCES.find((p) => p === want) ?? ''
}

const PHONE_RE = /^[0-9+ ]{1,30}$/
/** Digits and dashes, optionally led by one letter: FBR issues NTNs such as A723262. */
const NTN_RE = /^[A-Za-z]?[0-9-]{1,19}$/
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
    province: z.union([z.literal(''), z.enum(FBR_PROVINCES, { errorMap: () => ({ message: 'Pick one of FBR’s province names' }) })]),
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
      ctx.addIssue({ code: z.ZodIssueCode.custom, path: ['ntn'], message: 'Digits or dashes with at most one leading letter, at most 20 characters' })
    }
    if (v.cnic !== '' && !CNIC_RE.test(v.cnic)) {
      ctx.addIssue({ code: z.ZodIssueCode.custom, path: ['cnic'], message: 'Digits only, at most 15 characters' })
    }
    if (v.credit_limit !== '' && (!MONEY_RE.test(v.credit_limit) || Number(v.credit_limit) < 0)) {
      ctx.addIssue({ code: z.ZodIssueCode.custom, path: ['credit_limit'], message: 'Zero or more, at most 2 decimal places' })
    }
  })
export type CustomerDialogValues = z.infer<typeof customerDialogSchema>
