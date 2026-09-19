import { LogOut, User as UserIcon } from 'lucide-react'
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
import apiClient from '@/api/client'
import { roleLabel } from '@/lib/roles'
import type { User } from '@/types'

export function UserMenu({ user, collapsed = false }: { user: User; collapsed?: boolean }) {
  const logout = () => {
    apiClient.clearAuth()
    window.location.href = '/login'
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" className={collapsed ? 'h-auto rounded-full p-1.5' : 'h-auto w-full justify-start rounded-lg bg-muted/30 p-3 hover:bg-muted'}>
          <div className="flex w-full min-w-0 items-center gap-3">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-primary">
              <UserIcon className="h-4 w-4 text-primary-foreground" />
            </div>
            {!collapsed && (
              <div className="min-w-0 flex-1 text-left">
                <p className="truncate text-sm font-medium">{user.first_name} {user.last_name}</p>
                <p className="truncate text-xs text-muted-foreground">{roleLabel(user.role)}</p>
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
          <p className="px-1 pb-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Appearance</p>
          <ThemeSwitcher />
        </div>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={logout} className="text-red-600 focus:text-red-600">
          <LogOut className="mr-2 h-4 w-4" />
          Log out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
