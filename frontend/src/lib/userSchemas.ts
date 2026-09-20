import { z } from 'zod'
import { ROLES } from '@/lib/roles'

/** Mirrors backend handlers/password.go and handlers/users.go. */
export const PASSWORD_MIN = 8
export const PASSWORD_MAX_BYTES = 72
export const USERNAME_RE = /^[a-z0-9][a-z0-9._-]{2,49}$/
export const USERNAME_HINT = '3–50 characters: a–z, 0–9, dot, dash or underscore'

const password = z
  .string()
  .min(PASSWORD_MIN, `At least ${PASSWORD_MIN} characters`)
  .refine((p) => new TextEncoder().encode(p).length <= PASSWORD_MAX_BYTES, `At most ${PASSWORD_MAX_BYTES} bytes`)

const optionalEmail = z.union([z.literal(''), z.string().trim().email('Enter a valid email').max(100)])
const firstName = z.string().trim().min(1, 'First name is required').max(50)
const lastName = z.string().trim().max(50)
const role = z.enum(ROLES)

/**
 * One schema for the user dialog. `mode` decides what is enforced: on create
 * the username must match USERNAME_RE and a password is required; on edit
 * the username is display-only and a blank password keeps the current one.
 */
export const userDialogSchema = z
  .object({
    mode: z.enum(['create', 'edit']),
    username: z.string().trim().toLowerCase(),
    email: optionalEmail,
    password: z.union([z.literal(''), password]),
    first_name: firstName,
    last_name: lastName,
    role,
    is_active: z.boolean(),
  })
  .superRefine((v, ctx) => {
    if (v.mode !== 'create') return
    if (!USERNAME_RE.test(v.username)) ctx.addIssue({ code: z.ZodIssueCode.custom, path: ['username'], message: USERNAME_HINT })
    if (v.password === '') ctx.addIssue({ code: z.ZodIssueCode.custom, path: ['password'], message: `At least ${PASSWORD_MIN} characters` })
  })
export type UserDialogValues = z.infer<typeof userDialogSchema>

export const pinSchema = z
  .object({
    pin: z.string().regex(/^\d{4}$/, 'Exactly 4 digits'),
    confirm: z.string(),
  })
  .refine((v) => v.pin === v.confirm, { message: 'PINs do not match', path: ['confirm'] })
export type PinValues = z.infer<typeof pinSchema>

export const newPasswordSchema = z
  .object({ new_password: password, confirm: z.string() })
  .refine((v) => v.new_password === v.confirm, { message: 'Passwords do not match', path: ['confirm'] })
export type NewPasswordValues = z.infer<typeof newPasswordSchema>

export const changePasswordSchema = z
  .object({ current_password: z.string().min(1, 'Enter your current password'), new_password: password, confirm: z.string() })
  .refine((v) => v.new_password === v.confirm, { message: 'Passwords do not match', path: ['confirm'] })
  .refine((v) => v.new_password !== v.current_password, { message: 'Choose a different password', path: ['new_password'] })
export type ChangePasswordValues = z.infer<typeof changePasswordSchema>
