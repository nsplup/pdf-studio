<script module lang="ts">
  import { getContext, type Snippet } from "svelte";
  export function useTabs() {
    return getContext<{ get: () => string; set: (v: string) => void }>("tabs");
  }
</script>

<script lang="ts">
  import { cn } from "../../lib/utils";
  let {
    children,
    class: className = "",
  }: { children: Snippet; class?: string } = $props();
</script>

<div
  data-slot="tabs-list"
  class={cn(
    "inline-flex h-9 items-center justify-center rounded-lg bg-muted p-1 text-muted-foreground",
    className,
  )}
>
  {@render children()}
</div>
