/** @type {import('tailwindcss').Config} */
export default {
  darkMode: 'class',
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    container: {
      center: true,
      padding: "2rem",
      screens: {
        "2xl": "1400px",
      },
    },
    extend: {
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
          // Safety orange darkens on hover rather than fading: bg-primary/90
          // over a white card lightens it, which reads as "going away".
          hover: "hsl(var(--primary-hover))",
        },
        // The navy rail. Same colour in every theme — it is the one surface
        // that identifies the app, so it does not follow light/dark.
        rail: {
          DEFAULT: "hsl(var(--rail))",
          foreground: "hsl(var(--rail-foreground))",
          muted: "hsl(var(--rail-muted))",
        },
        // Semantics carry meaning, never decoration: green is an open day or a
        // settled account, amber is a day that needs attention, red is a void.
        success: {
          DEFAULT: "hsl(var(--success))",
          foreground: "hsl(var(--success-foreground))",
          soft: "hsl(var(--success-soft))",
          ink: "hsl(var(--success-ink))",
        },
        warning: {
          DEFAULT: "hsl(var(--warning))",
          foreground: "hsl(var(--warning-foreground))",
          soft: "hsl(var(--warning-soft))",
          ink: "hsl(var(--warning-ink))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
          soft: "hsl(var(--destructive-soft))",
          ink: "hsl(var(--destructive-ink))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
      },
      // One family for the whole interface. Manrope's figures are open and its
      // heavier weights stay legible at a glance across a counter, which is
      // what a till and a weighbridge plate need.
      fontFamily: {
        sans: ['Manrope', 'ui-sans-serif', 'system-ui', '-apple-system', '"Segoe UI"', 'sans-serif'],
      },
      // Two radii on purpose: 12px on panels (lg/xl), 6px on controls (md).
      // --radius is 0.75rem, so md/sm are subtracted down to the control size
      // rather than tracking the panel radius the way stock shadcn does.
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 6px)",
        sm: "calc(var(--radius) - 8px)",
      },
      keyframes: {
        "accordion-down": {
          from: { height: "0" },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: "0" },
        },
        "fade-in":  { "0%": { opacity: "0" }, "100%": { opacity: "1" } },
        "fade-out": { "0%": { opacity: "1" }, "100%": { opacity: "0" } },
        "slide-in": { "0%": { transform: "translateX(-100%)" }, "100%": { transform: "translateX(0)" } },
        "slide-out":{ "0%": { transform: "translateX(0)" }, "100%": { transform: "translateX(-100%)" } },
        "slide-in-from-right":  { "0%": { transform: "translateX(100%)" },  "100%": { transform: "translateX(0)" } },
        "slide-out-to-right":   { "0%": { transform: "translateX(0)" },     "100%": { transform: "translateX(100%)" } },
        "slide-in-from-left":   { "0%": { transform: "translateX(-100%)" }, "100%": { transform: "translateX(0)" } },
        "slide-out-to-left":    { "0%": { transform: "translateX(0)" },     "100%": { transform: "translateX(-100%)" } },
      },
      animation: {
        "accordion-down": "accordion-down 0.2s ease-out",
        "accordion-up":   "accordion-up 0.2s ease-out",
        "fade-in":        "fade-in 0.2s ease-out",
        "fade-out":       "fade-out 0.2s ease-out",
        "slide-in":       "slide-in 0.3s ease-out",
        "slide-out":      "slide-out 0.3s ease-out",
        "slide-in-from-right":  "slide-in-from-right 0.25s ease-out",
        "slide-out-to-right":   "slide-out-to-right 0.2s ease-in",
        "slide-in-from-left":   "slide-in-from-left 0.25s ease-out",
        "slide-out-to-left":    "slide-out-to-left 0.2s ease-in",
      },
    },
  },
  plugins: [require('@tailwindcss/container-queries')],
}
