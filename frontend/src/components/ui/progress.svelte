<script lang="ts">
  import { cn } from "../../lib/utils";
  let { value = 0, class: className }: { value?: number; class?: string } =
    $props();
</script>

<div
  class={cn(
    "relative h-2 w-full overflow-hidden rounded-full bg-secondary",
    className,
  )}
>
  <div
    class="h-full bg-primary"
    style="width: {Math.min(100, Math.max(0, value))}%"
  ></div>
</div>
