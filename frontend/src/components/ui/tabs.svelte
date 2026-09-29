<script module lang="ts">
  import { getContext } from "svelte";
  export function useTabs() {
    return getContext<{ get: () => string; set: (v: string) => void }>("tabs");
  }
</script>

<script lang="ts">
  import { setContext, type Snippet } from "svelte";

  let {
    value = $bindable(""),
    children,
    class: className = "",
  }: { value?: string; children: Snippet; class?: string } = $props();

  setContext("tabs", {
    get: () => value,
    set: (v: string) => (value = v),
  });
</script>

<div data-slot="tabs" class={className}>
  {@render children()}
</div>
