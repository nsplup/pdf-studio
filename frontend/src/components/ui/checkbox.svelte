<script lang="ts">
  import { cn } from "../../lib/utils";
  let {
    checked = $bindable(false),
    label,
    class: className,
    disabled = false,
  }: {
    checked?: boolean;
    label?: string;
    class?: string;
    disabled?: boolean;
  } = $props();
</script>

<label
  class={cn(
    "inline-flex cursor-pointer items-center gap-2 text-sm text-muted-foreground select-none",
    disabled && "opacity-50 cursor-not-allowed",
    className,
  )}
>
  <input
    type="checkbox"
    bind:checked
    {disabled}
    class="h-4 w-4 rounded border-input bg-transparent accent-primary cursor-pointer"
  />
  {#if label}{label}{/if}
</label>
