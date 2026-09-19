import { createFileRoute, redirect } from '@tanstack/react-router'
import apiClient from '@/api/client'
import { defaultPath, isRole } from '@/lib/roles'

export const Route = createFileRoute('/')({
  beforeLoad: () => {
    const user = apiClient.getStoredUser()
    if (!apiClient.isAuthenticated() || !user || !isRole(user.role)) throw redirect({ to: '/login' })
    throw redirect({ to: defaultPath(user.role), replace: true })
  },
  component: () => null,
})
