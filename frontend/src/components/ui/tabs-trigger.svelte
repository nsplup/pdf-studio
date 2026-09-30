<script lang="ts">
  import { cn } from "../../lib/utils";
  import { useTabs } from "./tabs.svelte";

  let {
    value,
    children,
    class: className = "",
    ...rest
  }: {
    value: string;
    children?: import("svelte").Snippet;
    class?: string;
  } & Record<string, unknown> = $props();

  const tabs = useTabs();
  const active = $derived(tabs.get() === value);
</script>

<button
  type="button"
  data-slot="tabs-trigger"
  role="tab"
  aria-selected={active}
  class={cn(
    "inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-md px-3 py-1 text-sm font-medium transition-all",
    "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
    "disabled:pointer-events-none disabled:opacity-50",
    active
      ? "bg-background text-foreground shadow-sm"
      : "hover:text-foreground",
    className,
  )}
  onclick={() => tabs.set(value)}
  {...rest}
>
  {@render children?.()}
</button>
