<script lang="ts">
  import { cn } from "../../lib/utils";
  import type { Snippet } from "svelte";
  import type { HTMLButtonAttributes } from "svelte/elements";

  type Variant =
    | "default"
    | "secondary"
    | "outline"
    | "ghost"
    | "destructive"
    | "success";
  type Size = "default" | "sm" | "lg" | "icon" | "icon-sm";

  let {
    variant = "default",
    size = "default",
    class: className,
    children,
    ...rest
  }: {
    variant?: Variant;
    size?: Size;
    class?: string;
    children?: Snippet;
  } & HTMLButtonAttributes = $props();

  const variants: Record<Variant, string> = {
    default: "bg-primary text-primary-foreground shadow hover:bg-primary/90",
    secondary:
      "bg-secondary text-secondary-foreground shadow-sm hover:bg-secondary/80",
    outline:
      "border border-input bg-transparent shadow-sm hover:bg-accent hover:text-accent-foreground",
    ghost: "hover:bg-accent hover:text-accent-foreground",
    destructive:
      "bg-destructive text-destructive-foreground shadow-sm hover:bg-destructive/90",
    success: "bg-success text-white shadow-sm hover:bg-success/90",
  };
  const sizes: Record<Size, string> = {
    default: "h-9 px-4 py-2",
    sm: "h-8 px-3 text-xs",
    lg: "h-10 px-6",
    icon: "h-9 w-9",
    "icon-sm": "h-7 w-7",
  };
</script>

<button
  class={cn(
    "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors",
    "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
    "disabled:pointer-events-none disabled:opacity-50",
    "cursor-pointer select-none",
    variants[variant],
    sizes[size],
    className,
  )}
  {...rest}
>
  {@render children?.()}
</button>
