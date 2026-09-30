<script lang="ts">
  import { cn } from "../../lib/utils";
  import type { Snippet } from "svelte";
  let {
    variant = "secondary",
    class: className,
    children,
  }: {
    variant?: "default" | "secondary" | "destructive" | "outline";
    class?: string;
    children?: Snippet;
  } = $props();
  const variants = {
    default: "bg-primary text-primary-foreground",
    secondary: "bg-secondary text-secondary-foreground",
    destructive: "bg-destructive text-destructive-foreground",
    outline: "border border-input text-foreground",
  };
</script>

<span
  class={cn(
    "inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium",
    variants[variant],
    className,
  )}
>
  {@render children?.()}
</span>
