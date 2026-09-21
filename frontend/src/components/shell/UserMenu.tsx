import { useState } from 'react'
import { KeyRound, LogOut, User as UserIcon } from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Button } from '@/components/ui/button'
import { ThemeSwitcher } from '@/components/ui/theme-switcher'
import { ChangePasswordDialog } from '@/components/shell/ChangePasswordDialog'
import apiClient from '@/api/client'
import { roleLabel } from '@/lib/roles'
import { cn } from '@/lib/utils'
import type { User } from '@/types'

export function UserMenu({ user, collapsed = false }: { user: User; collapsed?: boolean }) {
  const [changeOpen, setChangeOpen] = useState(false)
  const logout = () => {
    apiClient.clearAuth()
    window.location.href = '/login'
  }
  return (
    <>
      <DropdownMenu>
        {/* This sits on the navy rail, so it carries rail colours rather than
            the page's — a muted-foreground name here would be unreadable. */}
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            className={cn(
              'text-rail-foreground hover:bg-white/10 hover:text-rail-foreground focus-visible:ring-primary focus-visible:ring-offset-rail',
              collapsed ? 'h-auto rounded-full p-1.5' : 'h-auto w-full justify-start rounded-md p-2',
            )}
          >
            <div className="flex w-full min-w-0 items-center gap-3">
              <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-white/10">
                <UserIcon className="h-4 w-4 text-rail-foreground" />
              </div>
              {!collapsed && (
                <div className="min-w-0 flex-1 text-left">
                  <p className="truncate text-sm font-semibold">{user.first_name} {user.last_name}</p>
                  <p className="truncate text-xs font-medium text-rail-muted">{roleLabel(user.role)}</p>
                </div>
              )}
            </div>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" side={collapsed ? 'right' : 'top'} className="z-[70] w-64">
          <DropdownMenuLabel className="font-normal">
            <p className="text-[15px] font-medium leading-none">{user.username}</p>
            <p className="mt-1 text-[13px] leading-none text-muted-foreground">{user.email ?? roleLabel(user.role)}</p>
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          <div className="px-2 py-1.5">
            <p className="px-1 pb-1.5 text-xs font-semibold text-muted-foreground">Appearance</p>
            <ThemeSwitcher />
          </div>
          <DropdownMenuItem onSelect={() => setChangeOpen(true)}>
            <KeyRound className="mr-2 h-4 w-4" />
            Change password
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={logout} className="text-destructive focus:text-destructive">
            <LogOut className="mr-2 h-4 w-4" />
            Log out
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <ChangePasswordDialog open={changeOpen} onOpenChange={setChangeOpen} />
    </>
  )
}
