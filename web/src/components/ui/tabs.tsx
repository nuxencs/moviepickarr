import * as React from "react"
import * as TabsPrimitive from "@radix-ui/react-tabs"
import { motion } from "framer-motion"

import { cn } from "@/lib/utils"

const SPRING_TRANSITION = {
    type: "spring" as const,
    stiffness: 200,
    damping: 25,
} as const;

const Tabs = TabsPrimitive.Root

const TabsList = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.List>
>(({ className, ...props }, ref) => {
  return (
    <TabsPrimitive.List
      ref={ref}
      className={cn(
        "relative inline-flex h-10 items-center justify-center rounded-md bg-muted p-1 text-muted-foreground",
        className
      )}
      {...props}
    >
      {props.children}
    </TabsPrimitive.List>
  )
})
TabsList.displayName = TabsPrimitive.List.displayName

const TabsTrigger = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>
>(({ className, ...props }, ref) => {
  const [isActive, setIsActive] = React.useState(false)
  const internalRef = React.useRef<HTMLButtonElement>(null)

  React.useImperativeHandle(ref, () => internalRef.current!)

  React.useEffect(() => {
    const element = internalRef.current
    if (!element) return

    const checkActive = () => {
      setIsActive(element.getAttribute('data-state') === 'active')
    }

    checkActive()
    
    const observer = new MutationObserver(checkActive)
    observer.observe(element, {
      attributes: true,
      attributeFilter: ['data-state']
    })

    return () => observer.disconnect()
  }, [])
  
  return (
    <TabsPrimitive.Trigger
      ref={internalRef}
      className={cn(
        "relative inline-flex items-center justify-center whitespace-nowrap rounded-sm px-3 py-1.5 text-sm font-medium ring-offset-background transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 data-[state=active]:text-foreground hover:text-foreground z-10",
        className
      )}
      {...props}
    >
      {isActive && (
        <motion.div
          layoutId="activeTab"
          className="absolute inset-0 bg-background rounded-sm shadow-sm"
          transition={SPRING_TRANSITION}
          style={{
            zIndex: -1
          }}
        />
      )}
      {props.children}
    </TabsPrimitive.Trigger>
  )
})
TabsTrigger.displayName = TabsPrimitive.Trigger.displayName

const TabsContent = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Content
    ref={ref}
    className={cn(
      "mt-4 ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
      className
    )}
    {...props}
  />
))
TabsContent.displayName = TabsPrimitive.Content.displayName

export { Tabs, TabsList, TabsTrigger, TabsContent }