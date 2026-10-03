<script lang="ts">
  import { tasks, type RunningTask } from "../stores";
  import { Progress } from "./ui";
  import { Loader } from "lucide-svelte";

  /**
   * 阶段切换时进度条视觉归零。
   * 后端在 Progress 里显式给出 Phase，这里直接比较字符串，无需解析 message。
   */
  const phaseState = new Map<string, { phase: string; display: number }>();

  function displayPct(t: RunningTask): number {
    const prev = phaseState.get(t.id);
    if (!prev) {
      phaseState.set(t.id, { phase: t.phase, display: t.percent });
      return t.percent;
    }
    if (prev.phase !== t.phase) {
      prev.phase = t.phase;
      prev.display = 0;
      return 0;
    }
    prev.display = t.percent;
    return t.percent;
  }

  $effect(() => {
    const live = new Set($tasks.map((t) => t.id));
    for (const k of [...phaseState.keys()]) {
      if (!live.has(k)) phaseState.delete(k);
    }
  });
</script>

{#if $tasks.length}
  <div
    class="fixed bottom-6 left-1/2 z-[900] -translate-x-1/2 rounded-lg border bg-popover px-5 py-4 shadow-2xl min-w-80 space-y-3"
  >
    {#each $tasks as t (t.id)}
      {@const pct = displayPct(t)}
      <div>
        <div
          class="mb-1.5 flex items-center gap-2 text-xs text-muted-foreground"
        >
          <Loader class="h-3.5 w-3.5 animate-spin" />
          <span>{t.phase || t.label}{t.detail ? `：${t.detail}` : ""}</span>
          <span class="ml-auto tabular-nums">{pct.toFixed(0)}%</span>
        </div>
        <Progress value={pct} />
      </div>
    {/each}
  </div>
{/if}
