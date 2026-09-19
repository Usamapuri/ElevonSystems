import { Link, useLocation } from '@tanstack/react-router'
import { Flame } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent } from '@/components/ui/sheet'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { UserMenu } from '@/components/shell/UserMenu'
import { NAV_ITEMS } from '@/lib/roles'
import { cn } from '@/lib/utils'
import type { User } from '@/types'

interface SidebarProps {
  user: User
  isNarrowViewport: boolean
  drawerOpen: boolean
  onDrawerOpenChange: (open: boolean) => void
}

export function navTitleFromPath(pathname: string): string {
  return NAV_ITEMS.find((i) => pathname === i.to || pathname.startsWith(`${i.to}/`))?.label ?? 'Elevon POS'
}

export function Sidebar({ user, isNarrowViewport, drawerOpen, onDrawerOpenChange }: SidebarProps) {
  const { pathname } = useLocation()
  const items = NAV_ITEMS.filter((i) => i.roles.includes(user.role))

  const nav = (collapsed: boolean) => (
    <nav className="flex flex-1 flex-col gap-1 p-2">
      {items.map((item) => {
        const active = pathname === item.to || pathname.startsWith(`${item.to}/`)
        const button = (
          <Link key={item.id} to={item.to} className="block">
            <Button variant={active ? 'default' : 'ghost'} className={cn('w-full justify-start gap-3', collapsed && 'justify-center px-0')}>
              <item.icon className="h-5 w-5 shrink-0" />
              {!collapsed && <span>{item.label}</span>}
            </Button>
          </Link>
        )
        return collapsed ? (
          <Tooltip key={item.id}>
            <TooltipTrigger asChild>{button}</TooltipTrigger>
            <TooltipContent side="right">{item.label}</TooltipContent>
          </Tooltip>
        ) : (
          button
        )
      })}
    </nav>
  )

  const brand = (
    <div className="flex items-center gap-2 px-4 py-4">
      <Flame className="h-6 w-6 text-primary" />
      <span className="text-lg font-bold tracking-tight">Elevon POS</span>
    </div>
  )

  if (isNarrowViewport) {
    return (
      <Sheet open={drawerOpen} onOpenChange={onDrawerOpenChange}>
        <SheetContent side="left" className="flex w-72 flex-col p-0">
          {brand}
          {nav(false)}
          <div className="p-2">
            <UserMenu user={user} />
          </div>
        </SheetContent>
      </Sheet>
    )
  }

  return (
    <aside className="flex h-full w-60 flex-col border-r border-border bg-card">
      {brand}
      {nav(false)}
      <div className="p-2">
        <UserMenu user={user} />
      </div>
    </aside>
  )
}
