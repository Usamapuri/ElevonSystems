import { createFileRoute, Outlet, redirect, useLocation } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { PanelLeft } from 'lucide-react'
import apiClient from '@/api/client'
import { Button } from '@/components/ui/button'
import { Sidebar, navTitleFromPath } from '@/components/shell/Sidebar'
import { useMediaQuery } from '@/hooks/useMediaQuery'
import { canAccess, defaultPath, isRole } from '@/lib/roles'

// Auth and role guard live in beforeLoad, not in the component: rendering
// <Navigate> from a layout re-fires on every render while the redirect is
// pending and loops (seen as "Maximum update depth exceeded" on logout).
export const Route = createFileRoute('/_app')({
  beforeLoad: ({ location }) => {
    const user = apiClient.getStoredUser()
    if (!apiClient.isAuthenticated() || !user || !isRole(user.role)) {
      throw redirect({ to: '/login' })
    }
    if (!canAccess(user.role, location.pathname)) {
      throw redirect({ to: defaultPath(user.role), replace: true })
    }
    return { user }
  },
  component: AppLayout,
})

function AppLayout() {
  const { user } = Route.useRouteContext()
  const { pathname } = useLocation()
  const isNarrow = useMediaQuery('(max-width: 767px)')
  const [drawerOpen, setDrawerOpen] = useState(false)

  useEffect(() => setDrawerOpen(false), [pathname])

  return (
    <div className="flex h-[100dvh] overflow-hidden bg-background">
      <div className={isNarrow ? 'w-0 shrink-0' : 'shrink-0'}>
        <Sidebar user={user} isNarrowViewport={isNarrow} drawerOpen={drawerOpen} onDrawerOpenChange={setDrawerOpen} />
      </div>
      <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <header className="flex h-14 shrink-0 items-center gap-3 border-b border-border px-3 md:hidden">
          <Button variant="ghost" size="icon" aria-label="Open navigation" onClick={() => setDrawerOpen((o) => !o)}>
            <PanelLeft className="h-6 w-6" />
          </Button>
          <span className="truncate text-base font-semibold">{navTitleFromPath(pathname)}</span>
        </header>
        <div className="min-h-0 flex-1 overflow-y-auto">
          <Outlet />
        </div>
      </main>
    </div>
  )
}
