<script lang="ts">
  import { MetaService, type PageLabel } from "../bindings/services";
  import {
    currentDoc,
    notify,
    labelBase,
    labelProblems,
    type AppProblem,
    type LabelRow,
    type LabelProblem,
  } from "../stores";
  import * as UI from "./ui";
  import {
    Trash2,
    Loader2,
    Plus,
    TableProperties,
    FileQuestion,
    TriangleAlert,
  } from "lucide-svelte";

  let { pageCount = null as number | null } = $props();
  let doc = $derived($currentDoc);
  /** 逻辑页数（含未保存的插入/删除） */
  let total = $derived(pageCount ?? 0);
  let loadedKey = $state("");
  let attemptedKey = $state("");
  /** UI 内使用 1-based 起始页；保存时转回 0-based */
  let labels = $state<
    { startPage: number; prefix: string; style: string; startValue: number }[]
  >([]);
  let touched = $state(false);
  /** 基准区间行索引（radio 单选；-1 = 无基准）。仅影响书签视图页码显示，不写入 PDF */
  let baseIdx = $state(-1);
  let busy = $state(false);

  /** index → 该行的问题（每条规则最多命中一条，可直接映射） */
  let problemByIndex = $derived.by(() => {
    const m = new Map<number, LabelProblem>();
    for (const p of labelProbs) m.set(p.index, p);
    return m;
  });

  // 基准区间 → 共享偏移状态：显示页码 = 实际页码 - offset（基准页显示为该区间起始编号）
  function syncBaseStore() {
    if (baseIdx >= 0 && baseIdx < labels.length) {
      const b = labels[baseIdx];
      labelBase.set({
        offset: b.startPage - b.startValue,
        basePage: b.startPage,
      });
    } else {
      labelBase.set({ offset: 0, basePage: null });
    }
  }

  /** 初始化基准：自动选中 /PageLabels 中 /S = /D（十进制）的首个区间 */
  function autoSelectBase(raw: PageLabel[]) {
    baseIdx = raw.findIndex((l) => l.style === "D");
    syncBaseStore();
  }

  $effect(() => {
    const d = doc;
    const key = d ? d.sourcePath : "";
    if (!key || key === attemptedKey) return;
    attemptedKey = key;
    void load(key);
  });

  async function load(path: string) {
    busy = true;
    touched = false;
    try {
      const r = await MetaService.ReadPageLabels(path);
      const raw: PageLabel[] = r ?? [];
      labels = raw.map((l) => ({
        startPage: (l.startPage ?? 0) + 1,
        prefix: l.prefix ?? "",
        style: l.style ?? "D",
        startValue: l.startValue ?? 1,
      }));
      autoSelectBase(raw);
      loadedKey = path;
    } catch (e: any) {
      labels = [];
      loadedKey = "";
      notify("err", `读取页标签失败：${e?.message ?? e}`);
    } finally {
      busy = false;
    }
  }

  function addLabel() {
    const last = labels.length ? labels[labels.length - 1].startPage : 0;
    const nextStart = Math.min(total, last + 1);
    labels = [
      ...labels,
      { startPage: nextStart, prefix: "", style: "D", startValue: 1 },
    ];
    touched = true;
  }

  /** 切换基准区间（仅影响显示，不置 touched） */
  function setBase(i: number) {
    baseIdx = i;
    syncBaseStore();
  }

  function removeLabel(i: number) {
    labels = labels.filter((_, idx) => idx !== i);
    if (baseIdx === i) baseIdx = -1;
    else if (i < baseIdx) baseIdx--;
    touched = true;
    syncBaseStore();
  }

  function setNum(
    l: { startPage: number; startValue: number },
    key: "startPage" | "startValue",
    ev: Event,
  ) {
    const raw = (ev.currentTarget as HTMLInputElement).value;
    const v = parseInt(raw, 10);
    if (Number.isFinite(v)) {
      l[key] =
        key === "startPage" ? Math.max(1, Math.min(total, v)) : Math.max(0, v);
      touched = true;
      // 基准区间的起始页/起始编号变化时，同步书签视图的页码偏移
      if (labels[baseIdx] === l) syncBaseStore();
    }
  }

  /** 失焦提交：空值按业务语义回退，越界值把钳制结果写回 DOM */
  function commitNum(
    l: { startPage: number; startValue: number },
    key: "startPage" | "startValue",
    ev: FocusEvent,
  ) {
    const el = ev.currentTarget as HTMLInputElement;
    const raw = el.value.trim();
    const v = parseInt(raw, 10);

    // 空 / 非法：回退
    if (raw === "" || !Number.isFinite(v)) {
      if (key === "startValue") {
        // startValue 空 → 业务默认 1
        l.startValue = 1;
        touched = true;
        el.value = "1";
        if (labels[baseIdx] === l) syncBaseStore();
      } else {
        // startPage 空 → 回退到上一个有效值（模型未变，Svelte 不会自动回写 DOM，需手写）
        el.value = String(l.startPage);
      }
      return;
    }

    // 合法但越界：把钳制后的值写回 DOM，保持视觉一致
    const clamped =
      key === "startPage" ? Math.max(1, Math.min(total, v)) : Math.max(0, v);
    if (clamped !== v) el.value = String(clamped);
  }

  /** 校验：起始页需在 [1, total]，且必须严格递增（不能相等、不能逆序） */
  function computeLabelProblems(
    rows: LabelRow[],
    total: number,
  ): LabelProblem[] {
    if (total <= 0) return [];
    const problems: LabelProblem[] = [];
    let prevStart = -Infinity;
    let prevIdx = -1;
    for (let i = 0; i < rows.length; i++) {
      const cur = rows[i].startPage;
      if (!Number.isFinite(cur) || cur < 1 || cur > total) {
        problems.push({
          kind: "range",
          title: `第 ${i + 1} 个区间`,
          detail: `起始页 ${Number.isFinite(cur) ? cur : "无效"} 超出范围 [1, ${total}]`,
          index: i,
        });
        continue;
      }
      if (cur <= prevStart) {
        const reason =
          cur === prevStart
            ? `起始页 ${cur} 与第 ${prevIdx + 1} 个区间相等`
            : `起始页 ${cur} 小于第 ${prevIdx + 1} 个区间的 ${prevStart}`;
        problems.push({
          kind: "order",
          title: `第 ${i + 1} 个区间`,
          detail: `${reason}；起始页必须严格递增`,
          index: i,
        });
      }
      if (cur > prevStart) {
        prevStart = cur;
        prevIdx = i;
      }
    }
    return problems;
  }

  let labelProbs = $derived(computeLabelProblems(labels, total));

  $effect(() => {
    labelProblems.set(
      labelProbs.map(
        (p): AppProblem => ({
          source: "labels",
          kind: p.kind,
          title: p.title,
          detail: p.detail,
          tab: "labels",
        }),
      ),
    );
  });

  // ---------- 供 App 调用的接口 ----------

  export function getLabels(): PageLabel[] | undefined {
    if (loadedKey !== doc?.sourcePath) return undefined;
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
    if (loadedKey !== doc?.sourcePath) {
      notify("err", "页标签未成功加载，导入内容中的页标签未合并");
      return;
    }
    const map = new Map<number, PageLabel>();
    for (const l of labels) {
      map.set(l.startPage - 1, {
        startPage: l.startPage - 1,
        prefix: l.prefix || undefined,
        style: l.style || undefined,
        startValue: l.startValue || 1,
      });
    }
    for (const l of added)
      map.set(l.startPage, { ...l, startValue: l.startValue ?? 1 });
    labels = [...map.values()]
      .sort((a, b) => a.startPage - b.startPage)
      .map((l) => ({
        startPage: l.startPage + 1,
        prefix: l.prefix ?? "",
        style: l.style ?? "D",
        startValue: l.startValue ?? 1,
      }));
    touched = true;
    // 合并后行序可能变化，重新按 /D 规则选定基准
    autoSelectBase(labels.map((l) => ({ ...l, startPage: l.startPage - 1 })));
  }

  export function retryLabels() {
    attemptedKey = "";
  }
</script>

{#if doc}
  <div class="px-4 py-4">
    <UI.Card>
      <UI.CardHeader>
        <UI.CardTitle class="flex items-center gap-2">
          <TableProperties class="h-4 w-4 text-primary" /> 页标签
        </UI.CardTitle>
        <div class="mt-1.5 space-y-1 text-xs leading-5 text-muted-foreground">
          <div>
            <span class="mr-1 inline-block min-w-16 font-medium text-foreground"
              >起始页</span
            >本规则从第几页开始生效（1–{total}），需大于之前的区间
          </div>
          <div>
            <span class="mr-1 inline-block min-w-16 font-medium text-foreground"
              >前缀</span
            >页码前固定的文字，如 Chap-、附录，可留空
          </div>
          <div>
            <span class="mr-1 inline-block min-w-16 font-medium text-foreground"
              >编号风格</span
            >页码的编号格式：数字、罗马数字、字母或不编号
          </div>
          <div>
            <span class="mr-1 inline-block min-w-16 font-medium text-foreground"
              >起始编号</span
            >本区间第一页显示的编号值，默认 1
          </div>
          <div>
            <span class="mr-1 inline-block min-w-16 font-medium text-foreground"
              >基准区间</span
            >书签视图页码按此区间换算显示
          </div>
        </div>
      </UI.CardHeader>
      <UI.CardContent>
        {#if busy && !labels.length}
          <div class="empty">
            <Loader2 class="h-5 w-5 animate-spin" /> 正在读取页标签
          </div>
        {:else if labels.length}
          <div class="-mx-1 overflow-x-auto px-1 pb-1">
            <div class="min-w-[48.75rem] text-sm">
              <!-- 表头 -->
              <div
                class="grid grid-cols-[1.25rem_7.5rem_minmax(10rem,1fr)_12rem_7.5rem_5rem_2.25rem] items-center gap-x-2 border-b border-border pb-1.5 text-xs font-medium text-muted-foreground"
              >
                <div></div>
                <div>起始页</div>
                <div>前缀</div>
                <div>编号风格</div>
                <div>起始编号</div>
                <div></div>
                <div></div>
              </div>
              <!-- 数据行 -->
              <div class="divide-y divide-border/60">
                {#each labels as l, i (i)}
                  {@const p = problemByIndex.get(i)}
                  <div
                    class="grid grid-cols-[1.25rem_7.5rem_minmax(10rem,1fr)_12rem_7.5rem_5rem_2.25rem] items-center gap-x-2 py-1.5"
                  >
                    <!-- 行首警告列 -->
                    <div class="flex items-center justify-center">
                      {#if p}
                        <span title={p.detail} aria-label={p.detail}>
                          <TriangleAlert class="h-3.5 w-3.5 text-amber-600" />
                        </span>
                      {/if}
                    </div>

                    <input
                      type="number"
                      min="1"
                      max={total}
                      class="h-8 w-full rounded-md border border-input bg-transparent px-2 text-sm"
                      value={l.startPage}
                      oninput={(e) => setNum(l, "startPage", e)}
                      onblur={(e) => commitNum(l, "startPage", e)}
                    />
                    <input
                      type="text"
                      class="h-8 w-full rounded-md border border-input bg-transparent px-2 text-sm"
                      value={l.prefix}
                      placeholder="如 Chap-"
                      oninput={(e) => {
                        l.prefix = (e.currentTarget as HTMLInputElement).value;
                        touched = true;
                      }}
                    />
                    <UI.Select
                      class="w-full"
                      bind:value={l.style}
                      onchange={() => {
                        touched = true;
                      }}
                      options={[
                        { value: "D", label: "1,2,3（数字）" },
                        { value: "R", label: "I,II,III（大写罗马）" },
                        { value: "r", label: "i,ii,iii（小写罗马）" },
                        { value: "A", label: "A,B,C（大写字母）" },
                        { value: "a", label: "a,b,c（小写字母）" },
                        { value: "", label: "无编号（仅前缀）" },
                      ]}
                    />
                    <input
                      type="number"
                      min="0"
                      class="h-8 w-full rounded-md border border-input bg-transparent px-2 text-sm"
                      value={l.startValue}
                      oninput={(e) => setNum(l, "startValue", e)}
                      onblur={(e) => commitNum(l, "startValue", e)}
                    />

                    <!-- 基准 radio（保持原样） -->
                    <div class="flex items-center justify-center">
                      <input
                        id="label-base-{i}"
                        type="radio"
                        name="label-base"
                        class="peer sr-only"
                        checked={baseIdx === i}
                        onchange={() => setBase(i)}
                      />
                      <label
                        for="label-base-{i}"
                        title="将此区间的页码计算规则应用到书签视图"
                        class="flex h-8 w-full cursor-pointer select-none items-center justify-center rounded-md border border-input text-xs text-muted-foreground transition-colors
               hover:border-primary/40 hover:bg-accent hover:text-foreground
               peer-checked:border-primary peer-checked:bg-primary/10 peer-checked:font-medium peer-checked:text-primary
               peer-focus-visible:ring-2 peer-focus-visible:ring-primary/50"
                      >
                        {baseIdx === i ? "当前基准" : "设为基准"}
                      </label>
                    </div>

                    <button
                      type="button"
                      class="mx-auto flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-destructive/15 hover:text-destructive"
                      title="删除该区间"
                      onclick={() => removeLabel(i)}
                    >
                      <Trash2 class="h-3.5 w-3.5" />
                    </button>
                  </div>
                {/each}
              </div>
            </div>
          </div>
        {:else if loadedKey !== doc?.sourcePath}
          <div class="empty-sm">
            读取页标签失败，保存时将不修改页标签。
            <button class="underline" onclick={retryLabels}>重试</button>
          </div>
        {:else}
          <div class="empty-sm">该文档没有页标签</div>
        {/if}
        <button
          type="button"
          class="mt-3 inline-flex items-center gap-1.5 rounded-md border border-dashed border-input px-2.5 py-1.5 text-xs text-muted-foreground transition-colors hover:border-primary/50 hover:bg-accent hover:text-foreground"
          onclick={addLabel}
        >
          <Plus class="h-3.5 w-3.5" /> 添加标签定义
        </button>
      </UI.CardContent>
    </UI.Card>
  </div>
{:else}
  <div
    class="flex h-full flex-col items-center justify-center gap-3 text-muted-foreground"
  >
    <FileQuestion class="h-12 w-12" />
    <p class="text-lg">打开 PDF 开始编辑</p>
  </div>
{/if}

<style>
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 80px 0;
    color: hsl(var(--muted-foreground));
    font-size: 14px;
  }
  .empty-sm {
    padding: 20px 0;
    font-size: 13px;
    color: hsl(var(--muted-foreground));
  }
</style>
