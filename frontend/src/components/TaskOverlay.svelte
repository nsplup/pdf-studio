<script lang="ts">
  import { tasks } from "../stores";
  import { Progress } from "./ui";
  import { Loader } from "lucide-svelte";
</script>

{#if $tasks.length}
  <div
    class="fixed bottom-6 left-1/2 z-[900] -translate-x-1/2 rounded-lg border bg-popover px-5 py-4 shadow-2xl min-w-80 space-y-3"
  >
    {#each $tasks as t (t.id)}
      <div>
        <div
          class="mb-1.5 flex items-center gap-2 text-xs text-muted-foreground"
        >
          <Loader class="h-3.5 w-3.5 animate-spin" />
          <span>{t.message || t.label}</span>
          <span class="ml-auto tabular-nums">{t.percent.toFixed(0)}%</span>
        </div>
        <Progress value={t.percent} />
      </div>
    {/each}
  </div>
{/if}
