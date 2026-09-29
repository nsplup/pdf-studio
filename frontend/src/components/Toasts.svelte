<script lang="ts">
  import { toasts } from "../stores";
  import { CheckCircle2, XCircle, Info } from "lucide-svelte";
</script>

<div class="fixed right-4 top-4 z-[1000] flex flex-col gap-2 max-w-sm">
  {#each $toasts as t (t.id)}
    <div
      class="flex items-start gap-2 rounded-lg border bg-popover px-4 py-3 text-sm shadow-lg
             {t.kind === 'ok' ? 'border-success/40' : t.kind === 'err' ? 'border-destructive/50' : 'border-border'}"
    >
      {#if t.kind === 'ok'}<CheckCircle2 class="h-4 w-4 mt-0.5 text-success shrink-0" />
      {:else if t.kind === 'err'}<XCircle class="h-4 w-4 mt-0.5 text-destructive shrink-0" />
      {:else}<Info class="h-4 w-4 mt-0.5 text-muted-foreground shrink-0" />{/if}
      <span class="leading-5">{t.text}</span>
    </div>
  {/each}
</div>
