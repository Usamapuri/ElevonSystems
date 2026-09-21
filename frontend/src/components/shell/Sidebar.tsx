/**
 * The navigation rail.
 *
 * It is navy in every theme. The rail is the one surface that says which
 * application this is, and an identity that changes colour with the theme
 * is not an identity — so light, dark and high-contrast all get the same
 * cylinder navy, and only the canvas beside it changes.
 *
 * The active item is marked twice over: a 3px safety-orange bar on the left
 * edge and a lifted row. Orange means "this is where you are" here and "this
 * is the action" on a button — it never decorates anything else.
 */
import { Link, useLocation } from '@tanstack/react-router'
import { Sheet, SheetContent } from '@/components/ui/sheet'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { BrandMark } from '@/components/shell/BrandMark'
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

export function Sidebar({ user, isNarrowViewport, drawerOpen, onDrawerOpenChange }: SidebarProps) {
  const { pathname } = useLocation()
  const items = NAV_ITEMS.filter((i) => i.roles.includes(user.role))

  const nav = (collapsed: boolean) => (
    <nav className="flex flex-1 flex-col gap-0.5 overflow-y-auto p-2" aria-label="Main">
      {items.map((item) => {
        const active = pathname === item.to || pathname.startsWith(`${item.to}/`)
        const button = (
          <Link
            key={item.id}
            to={item.to}
            aria-current={active ? 'page' : undefined}
            className={cn(
              // 44px tall: the rail is used with a thumb on a counter tablet.
              'relative flex h-11 items-center gap-3 rounded-md pl-4 pr-3 text-sm font-semibold transition-colors',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 focus-visible:ring-offset-rail',
              active
                ? 'bg-white/10 text-rail-foreground'
                : 'text-rail-muted hover:bg-white/5 hover:text-rail-foreground',
              collapsed && 'justify-center px-0',
            )}
          >
            {active && (
              <span
                className="absolute inset-y-1.5 left-0 w-[3px] rounded-r-sm bg-primary"
                aria-hidden
              />
            )}
            <item.icon className="h-5 w-5 shrink-0" />
            {!collapsed && <span className="truncate">{item.label}</span>}
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
    <div className="flex items-center gap-3 px-4 py-4">
      <BrandMark />
      <span className="text-base font-extrabold tracking-tight text-rail-foreground">Elevon POS</span>
    </div>
  )

  const body = (
    <>
      {brand}
      {nav(false)}
      <div className="border-t border-white/10 p-2">
        <UserMenu user={user} />
      </div>
    </>
  )

  if (isNarrowViewport) {
    return (
      <Sheet open={drawerOpen} onOpenChange={onDrawerOpenChange}>
        <SheetContent
          side="left"
          // The sheet's own close button is styled for a light panel; on the
          // navy rail it would be a grey smudge.
          className="flex w-72 flex-col border-r-0 bg-rail p-0 [&>button]:text-rail-muted [&>button]:hover:bg-white/10 [&>button]:hover:text-rail-foreground"
        >
          {body}
        </SheetContent>
      </Sheet>
    )
  }

  return <aside className="flex h-full w-60 flex-col bg-rail">{body}</aside>
}
