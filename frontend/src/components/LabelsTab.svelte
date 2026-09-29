<script lang="ts">
  import { MetaService, type PageLabel } from "../bindings/services";
  import { currentDoc, notify } from "../stores";
  import * as UI from "./ui";
  import { Trash2, Loader2, Plus, TableProperties, FileQuestion } from "lucide-svelte";

  let { pageCount = null as number | null } = $props();
  let doc = $derived($currentDoc);
  /** 逻辑页数（含未保存的插入/删除） */
  let total = $derived(pageCount ?? total);
  let loadedKey = $state("");
  /** UI 内使用 1-based 起始页；保存时转回 0-based */
  let labels = $state<{ startPage: number; prefix: string; style: string; startValue: number }[]>([]);
  let touched = $state(false);
  let busy = $state(false);

  $effect(() => {
    const d = doc;
    const key = d ? d.sourcePath : "";
    if (!key || key === loadedKey) return;
    loadedKey = key;
    void load(key);
  });

  async function load(path: string) {
    busy = true;
    touched = false;
    try {
      const r = await MetaService.ReadPageLabels(path);
      labels = (r ?? []).map((l) => ({
        startPage: (l.startPage ?? 0) + 1,
        prefix: l.prefix ?? "",
        style: l.style ?? "D",
        startValue: l.startValue ?? 1,
      }));
    } catch (e: any) {
      labels = [];
      notify("err", `读取页标签失败：${e?.message ?? e}`);
    } finally {
      busy = false;
    }
  }

  function addLabel() {
    const nextStart = labels.length
      ? Math.min(total, labels[labels.length - 1].startPage + 1)
      : 1;
    labels = [...labels, { startPage: nextStart, prefix: "", style: "D", startValue: 1 }];
    touched = true;
  }

  function removeLabel(i: number) {
    labels = labels.filter((_, idx) => idx !== i);
    touched = true;
  }

  function setNum(l: { startPage: number; startValue: number }, key: "startPage" | "startValue", ev: Event) {
    const raw = (ev.currentTarget as HTMLInputElement).value;
    const v = parseInt(raw, 10);
    if (Number.isFinite(v)) {
      l[key] = key === "startPage"
        ? Math.max(1, Math.min(total, v))
        : Math.max(0, v);
      touched = true;
    }
  }

  // ---------- 供 App 调用的接口 ----------

  /** 当前页标签（0-based）；未修改返回 undefined = 保存时不修改 */
  export function getLabels(): PageLabel[] | undefined {
    if (!touched) return undefined;
    return labels.map((l) => ({
      startPage: Math.max(0, l.startPage - 1),
      prefix: l.prefix || undefined,
      style: l.style || undefined,
      startValue: l.startValue || 1,
    }));
  }

  /** 导入放置后合并来源 PDF 的页标签区间（0-based；内部转 1-based 存储） */
  export function mergeLabels(added: PageLabel[]) {
    if (!added?.length) return;
    const map = new Map<number, PageLabel>();
    for (const l of labels) {
      map.set(l.startPage - 1, {
        startPage: l.startPage - 1,
        prefix: l.prefix || undefined,
        style: l.style || undefined,
        startValue: l.startValue || 1,
      });
    }
    for (const l of added) map.set(l.startPage, { ...l, startValue: l.startValue ?? 1 });
    labels = [...map.values()]
      .sort((a, b) => a.startPage - b.startPage)
      .map((l) => ({
        startPage: l.startPage + 1,
        prefix: l.prefix ?? "",
        style: l.style ?? "D",
        startValue: l.startValue ?? 1,
      }));
    touched = true;
  }
</script>

{#if doc}
  <div class="h-full px-4 py-4">
    <UI.Card>
      <UI.CardHeader>
        <UI.CardTitle class="flex items-center gap-2">
          <TableProperties class="h-4 w-4 text-primary" /> 页标签
        </UI.CardTitle>
        <div class="mt-1.5 space-y-1 text-xs leading-5 text-muted-foreground">
          <div><span class="mr-1 inline-block min-w-16 font-medium text-foreground">起始页</span>该编号规则从第几页开始（1-{total}，不得小于上一区间的起始页）。</div>
          <div><span class="mr-1 inline-block min-w-16 font-medium text-foreground">前缀</span>页码前固定的文字，如「Chap-」「附录」，可留空。</div>
          <div><span class="mr-1 inline-block min-w-16 font-medium text-foreground">编号风格</span>编号格式：数字 1,2,3 / 大小写罗马 / 大小写字母 / 仅前缀不编号。</div>
          <div><span class="mr-1 inline-block min-w-16 font-medium text-foreground">起始编号</span>该区间第一页显示的编号值，默认 1，可从任意数开始（如从 D 开始填 4）。</div>
          <div class="text-muted-foreground/80">各区间自上而下生效；更改将在顶部「保存 / 另存为」时写入 PDF。</div>
        </div>
      </UI.CardHeader>
      <UI.CardContent>
        {#if busy && !labels.length}
          <div class="empty"><Loader2 class="h-5 w-5 animate-spin" /> 读取页标签…</div>
        {:else if labels.length}
          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <thead>
                <tr class="text-left text-xs text-muted-foreground">
                  <th class="py-1 pr-1.5 font-normal">起始页</th>
                  <th class="py-1 pr-1.5 font-normal">前缀</th>
                  <th class="py-1 pr-1.5 font-normal">编号风格</th>
                  <th class="py-1 pr-1.5 font-normal">起始编号</th>
                  <th class="py-1 font-normal w-8"></th>
                </tr>
              </thead>
              <tbody>
                {#each labels as l, i (i)}
                  <tr>
                    <td class="py-1 pr-1.5">
                      <input
                        type="number" min="1" max={total}
                        class="h-8 w-28 rounded-md border border-input bg-transparent px-2 text-sm"
                        value={l.startPage}
                        oninput={(e) => setNum(l, "startPage", e)}
                      />
                    </td>
                    <td class="py-1 pr-1.5">
                      <input
                        type="text"
                        class="h-8 w-64 rounded-md border border-input bg-transparent px-2 text-sm"
                        value={l.prefix}
                        placeholder="如 Chap-"
                        oninput={(e) => { l.prefix = (e.currentTarget as HTMLInputElement).value; touched = true; }}
                      />
                    </td>
                    <td class="py-1 pr-1.5">
                      <UI.Select
                        class="w-40"
                        bind:value={l.style}
                        onchange={() => { touched = true; }}
                        options={[
                          { value: "D", label: "1,2,3（数字）" },
                          { value: "R", label: "I,II,III（大写罗马）" },
                          { value: "r", label: "i,ii,iii（小写罗马）" },
                          { value: "A", label: "A,B,C（大写字母）" },
                          { value: "a", label: "a,b,c（小写字母）" },
                          { value: "", label: "无编号（仅前缀）" },
                        ]}
                      />
                    </td>
                    <td class="py-1 pr-1.5">
                      <input
                        type="number" min="0"
                        class="h-8 w-28 rounded-md border border-input bg-transparent px-2 text-sm"
                        value={l.startValue}
                        oninput={(e) => setNum(l, "startValue", e)}
                      />
                    </td>
                    <td class="py-1">
                      <button
                        type="button"
                        class="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-destructive/15 hover:text-destructive"
                        title="删除该区间"
                        onclick={() => removeLabel(i)}
                      >
                        <Trash2 class="h-3.5 w-3.5" />
                      </button>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {:else}
          <div class="empty-sm">该文档没有页标签</div>
        {/if}
        <button
          type="button"
          class="mt-3 flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-accent hover:text-foreground"
          onclick={addLabel}
        >
          <Plus class="h-3.5 w-3.5" /> 添加标签定义
        </button>
      </UI.CardContent>
    </UI.Card>
  </div>
{:else}
  <div class="empty"><FileQuestion class="h-10 w-10" /> 打开 PDF 后编辑页标签</div>
{/if}

<style>
  .empty {
    display: flex; flex-direction: column; align-items: center; justify-content: center;
    gap: 10px; padding: 80px 0; color: hsl(var(--muted-foreground)); font-size: 14px;
  }
  .empty-sm {
    padding: 20px 0; font-size: 13px; color: hsl(var(--muted-foreground));
  }
</style>
