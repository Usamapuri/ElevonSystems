import { describe, expect, it } from 'vitest'
import { changePasswordSchema, newPasswordSchema, pinSchema, userDialogSchema } from './userSchemas'

const create = { mode: 'create' as const, username: 'Ali.K', email: '', password: 'till-pass-1', first_name: 'Ali', last_name: 'Khan', role: 'counter' as const, is_active: true }

describe('userDialogSchema (create)', () => {
  it('lowercases the username and allows a blank email', () => {
    const r = userDialogSchema.safeParse(create)
    expect(r.success && r.data.username).toBe('ali.k')
  })
  it('rejects short usernames, bad emails, short or missing passwords, unknown roles', () => {
    expect(userDialogSchema.safeParse({ ...create, username: 'ab' }).success).toBe(false)
    expect(userDialogSchema.safeParse({ ...create, email: 'nope' }).success).toBe(false)
    expect(userDialogSchema.safeParse({ ...create, password: 'short' }).success).toBe(false)
    expect(userDialogSchema.safeParse({ ...create, password: '' }).success).toBe(false)
    expect(userDialogSchema.safeParse({ ...create, role: 'manager' }).success).toBe(false)
    expect(userDialogSchema.safeParse({ ...create, first_name: '' }).success).toBe(false)
  })
})

describe('userDialogSchema (edit)', () => {
  it('password may be blank (keep current) but not short; username is not re-validated', () => {
    const edit = { ...create, mode: 'edit' as const, password: '', username: 'legacy name' }
    expect(userDialogSchema.safeParse(edit).success).toBe(true)
    expect(userDialogSchema.safeParse({ ...edit, password: 'short' }).success).toBe(false)
  })
})

describe('pinSchema', () => {
  it('needs four digits twice', () => {
    expect(pinSchema.safeParse({ pin: '1234', confirm: '1234' }).success).toBe(true)
    expect(pinSchema.safeParse({ pin: '123', confirm: '123' }).success).toBe(false)
    expect(pinSchema.safeParse({ pin: '1234', confirm: '4321' }).success).toBe(false)
  })
})

describe('password schemas', () => {
  it('new password must match its confirmation', () => {
    expect(newPasswordSchema.safeParse({ new_password: 'new-pass-1', confirm: 'new-pass-1' }).success).toBe(true)
    expect(newPasswordSchema.safeParse({ new_password: 'new-pass-1', confirm: 'other' }).success).toBe(false)
  })
  it('change password needs the current one and a different new one', () => {
    expect(changePasswordSchema.safeParse({ current_password: 'old-pass-1', new_password: 'new-pass-1', confirm: 'new-pass-1' }).success).toBe(true)
    expect(changePasswordSchema.safeParse({ current_password: '', new_password: 'new-pass-1', confirm: 'new-pass-1' }).success).toBe(false)
    expect(changePasswordSchema.safeParse({ current_password: 'same-pass-1', new_password: 'same-pass-1', confirm: 'same-pass-1' }).success).toBe(false)
  })
})
