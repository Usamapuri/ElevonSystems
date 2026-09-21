import * as React from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'

const Sheet = DialogPrimitive.Root
const SheetTrigger = DialogPrimitive.Trigger
const SheetPortal = DialogPrimitive.Portal
const SheetClose = DialogPrimitive.Close

const SheetOverlay = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay
    ref={ref}
    className={cn(
      'fixed inset-0 z-50 bg-rail/70 backdrop-blur-[1px]',
      'data-[state=open]:animate-fade-in data-[state=closed]:animate-fade-out',
      className,
    )}
    {...props}
  />
))
SheetOverlay.displayName = 'SheetOverlay'

type SheetSide = 'right' | 'left'
type SheetSize = 'default' | 'lg' | 'xl'

const SIDE_BASE: Record<SheetSide, string> = {
  right: 'inset-y-0 right-0 border-l',
  left: 'inset-y-0 left-0 border-r',
}

const SIDE_OPEN_ANIM: Record<SheetSide, string> = {
  right: 'data-[state=open]:animate-slide-in-from-right data-[state=closed]:animate-slide-out-to-right',
  left: 'data-[state=open]:animate-slide-in-from-left data-[state=closed]:animate-slide-out-to-left',
}

const SIZE_WIDTH: Record<SheetSize, string> = {
  default: 'w-full sm:max-w-md',
  lg: 'w-full sm:max-w-xl',
  // `xl` hosts wide data tables (e.g. the Daily Sales drill-in with its
  // 11-column orders table). 94vw + a generous 1440px cap means the panel
  // grows with the screen and avoids the horizontal-scroll-inside-a-drawer
  // trap, while still leaving a visible sliver of the page behind so the
  // user keeps spatial context.
  xl: 'w-full sm:w-[94vw] sm:max-w-[1440px]',
}

interface SheetContentProps
  extends React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> {
  side?: SheetSide
  size?: SheetSize
  /** Set to false to omit the built-in close (×) button in the top-right corner. */
  showCloseButton?: boolean
}

const SheetContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  SheetContentProps
>(
  (
    {
      className,
      children,
      side = 'right',
      size = 'default',
      showCloseButton = true,
      ...props
    },
    ref,
  ) => (
    <SheetPortal>
      <SheetOverlay />
      <DialogPrimitive.Content
        ref={ref}
        className={cn(
          'fixed z-50 flex h-full flex-col bg-card shadow-xl outline-none',
          SIDE_BASE[side],
          SIZE_WIDTH[size],
          SIDE_OPEN_ANIM[side],
          className,
        )}
        {...props}
      >
        {children}
        {showCloseButton && (
          <DialogPrimitive.Close
            className="absolute right-4 top-4 rounded-md p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus:outline-none focus:ring-2 focus:ring-ring"
            aria-label="Close"
          >
            <X className="h-4 w-4" />
          </DialogPrimitive.Close>
        )}
      </DialogPrimitive.Content>
    </SheetPortal>
  ),
)
SheetContent.displayName = 'SheetContent'

const SheetHeader = ({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn('flex flex-col gap-1.5 border-b border-border px-5 py-4', className)}
    {...props}
  />
)
SheetHeader.displayName = 'SheetHeader'

const SheetBody = ({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
  <div className={cn('flex-1 overflow-y-auto px-5 py-5', className)} {...props} />
)
SheetBody.displayName = 'SheetBody'

const SheetFooter = ({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn(
      'flex flex-col-reverse gap-2 border-t border-border px-5 py-3 sm:flex-row sm:justify-end',
      className,
    )}
    {...props}
  />
)
SheetFooter.displayName = 'SheetFooter'

const SheetTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title
    ref={ref}
    className={cn('text-lg font-semibold leading-tight tracking-tight text-foreground', className)}
    {...props}
  />
))
SheetTitle.displayName = 'SheetTitle'

const SheetDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description
    ref={ref}
    className={cn('text-sm text-muted-foreground', className)}
    {...props}
  />
))
SheetDescription.displayName = 'SheetDescription'

export {
  Sheet,
  SheetTrigger,
  SheetClose,
  SheetPortal,
  SheetOverlay,
  SheetContent,
  SheetHeader,
  SheetBody,
  SheetFooter,
  SheetTitle,
  SheetDescription,
}
