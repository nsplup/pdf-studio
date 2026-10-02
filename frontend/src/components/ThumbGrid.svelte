<script lang="ts">
  import { onMount, untrack } from "svelte";
  import { get } from "svelte/store";
  import { createVirtualizer } from "@tanstack/svelte-virtual";
  import { Plus, Loader, Trash2, Maximize2 } from "lucide-svelte";
  import PagePreview from "./PagePreview.svelte";

  let {
    pageCount = 0,
    docID = "",
    pageRes = null as string[] | null,
    selected = new Set<number>(),
    busy = false,
    onSelectionChange = (_sel: Set<number>) => {},
    onDelete = (_pages: string) => {},
    placing = false,
    buffer = null,
    onPlace = (_atIndex: number, _before: boolean) => {},
    onStartMove = (_pages: string) => {},
    onCancelPlace = () => {},
  }: {
    pageCount?: number;
    docID?: string;
    /** 每页缩略图缓存版本（下标 p-1）；0 或缺省 = 尚未生成 */
    pageRes?: string[] | null;
    selected?: Set<number>;
    busy?: boolean;
    onSelectionChange?: (sel: Set<number>) => void;
    onDelete?: (pages: string) => void;
    placing?: boolean;
    buffer?: { id: string; pageCount: number } | null;
    onPlace?: (atIndex: number, before: boolean) => void;
    onStartMove?: (pages: string) => void;
    onCancelPlace?: () => void;
  } = $props();

  let viewport: HTMLDivElement | undefined = $state();
  let viewportW = $state(1000);

  const MIN_CARD_W = 132; // 低于该宽度则减列数
  const THUMB_H = 186;
  const GAP = 14;
  const ROW_H = THUMB_H + GAP;
  const OVERSCAN = 4;
  const VP_PAD = 24; // 视口左右内边距合计

  let contentW = $derived(Math.max(200, viewportW - VP_PAD));
  let cols = $derived(
    Math.max(1, Math.floor((contentW + GAP) / (MIN_CARD_W + GAP))),
  );
  let cardW = $derived((contentW - (cols - 1) * GAP) / cols); // 精确铺满一行
  let totalRows = $derived(Math.ceil(pageCount / cols));

  let anchor = $state<number | null>(null); // shift 范围选择锚点

  let menu = $state<{ x: number; y: number } | null>(null);
  let menuPages = $state<number[]>([]);
  let previewPage = $state<number | null>(null);

  // TanStack Virtual：只虚拟化行（列数固定由宽度决定）
  const virtualizer = createVirtualizer<HTMLDivElement, HTMLDivElement>({
    count: untrack(() => totalRows),
    getScrollElement: () => viewport ?? null,
    estimateSize: () => ROW_H,
    overscan: OVERSCAN,
  });

  // 行数（页数/列数）变化时同步到虚拟器
  $effect(() => {
    const c = totalRows;
    untrack(() => get(virtualizer)?.setOptions({ count: c }));
  });

  /** 缩略图 URL：按页版本；无版本（未生成）返回 null 显示占位 */
  /** 空白页标记 */
  function isBlank(page: number): boolean {
    return (pageRes?.[page - 1] ?? "").startsWith("blank-");
  }
  /** 内容寻址资源地址：/thumbs/<uuid>/140.png */
  /** 空白页占位：按标记中的页面尺寸呈现（与真实页面同样以 contain 方式落在卡片内） */
  function blankStyle(p: number): string {
    const u = pageRes?.[p - 1] ?? "";
    const m = u.match(/^blank-([\d.]+)x([\d.]+)$/);
    const w = m ? parseFloat(m[1]) : 595;
    const h = m ? parseFloat(m[2]) : 842;
    const land = w >= h;
    return `background:white; aspect-ratio:${w}/${h}; ${land ? "width:100%; height:auto;" : "height:100%; width:auto;"} margin:0 auto;`;
  }

  function urlOf(page: number): string | null {
    const u = pageRes?.[page - 1];
    if (!u || u.startsWith("blank-")) return null;
    return `/thumbs/${u}/140.png`;
  }

  function measure() {
    if (!viewport) return;
    viewportW = viewport.clientWidth;
  }
  onMount(() => {
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(viewport!);
    return () => ro.disconnect();
  });

  function togglePage(p: number, ev: MouseEvent) {
    if (placing || busy) return; // 插入模式下点击仅用于选择插入点，不改变选中
    ev.stopPropagation();
    if (ev.shiftKey && anchor != null) {
      // Shift 范围选择：锚点到当前页
      const lo = Math.min(anchor, p);
      const hi = Math.max(anchor, p);
      const next = new Set(selected);
      for (let i = lo; i <= hi; i++) next.add(i);
      selected = next;
      onSelectionChange(selected);
      return;
    }
    anchor = p;
    const next = new Set(selected);
    if (next.has(p)) next.delete(p);
    else next.add(p);
    selected = next;
    onSelectionChange(selected);
  }

  function contextMenu(p: number, ev: MouseEvent) {
    ev.preventDefault();
    ev.stopPropagation(); // 冒泡到视口会被 closeMenu 立即关闭（菜单失效根因）
    if (placing || busy) return;
    if (!selected.has(p)) {
      const next = new Set<number>([p]);
      selected = next;
      onSelectionChange(selected);
    }
    menu = { x: ev.clientX, y: ev.clientY };
    menuPages = [...selected].sort((a, b) => a - b);
  }

  function closeMenu() {
    menu = null;
    menuPages = [];
  }

  function clearSelection() {
    if (placing) return; // 插入模式下点空白处只关闭菜单，不清空选中
    anchor = null;
    if (selected.size) {
      selected = new Set();
      onSelectionChange(selected);
    }
  }

  function selectionString(pages: number[]): string {
    return pages.join(",");
  }

  function menuDelete() {
    const pages = selectionString(menuPages);
    closeMenu();
    onDelete(pages);
  }

  function menuMove() {
    const pages = selectionString(menuPages);
    closeMenu();
    onStartMove(pages);
  }

  function placeClick(p: number, before: boolean, ev: MouseEvent) {
    ev.stopPropagation();
    onPlace(p, before);
  }

  function onKeydown(ev: KeyboardEvent) {
    if (ev.key === "Escape") {
      if (menu) {
        closeMenu();
        return;
      }
      if (placing) onCancelPlace();
    }
  }

  $effect(() => {
    window.addEventListener("keydown", onKeydown);
    return () => window.removeEventListener("keydown", onKeydown);
  });

  // 菜单打开期间：捕获阶段吞掉任意外部 pointerdown/click —— 只关菜单，不影响选择
  let menuEl: HTMLDivElement | undefined = $state();
  $effect(() => {
    if (!menu) return;
    const inMenu = (ev: Event) =>
      !!menuEl && ev.target instanceof Node && menuEl.contains(ev.target);
    const swallow = (ev: Event) => {
      if (inMenu(ev)) return; // 菜单项点击放行
      ev.preventDefault();
      ev.stopPropagation();
      closeMenu();
    };
    window.addEventListener("pointerdown", swallow, true);
    window.addEventListener("click", swallow, true);
    return () => {
      window.removeEventListener("pointerdown", swallow, true);
      window.removeEventListener("click", swallow, true);
    };
  });

  // 页数变化时清理越界选择
  $effect(() => {
    if (!pageCount) return;
    const over = [...selected].filter((p) => p > pageCount);
    if (over.length) {
      const next = new Set(selected);
      over.forEach((p) => next.delete(p));
      selected = next;
      onSelectionChange(selected);
    }
  });
</script>

<!-- 点击空白处：关闭菜单 + 取消选中 -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="thumb-viewport"
  bind:this={viewport}
  onclick={() => {
    closeMenu();
    clearSelection();
  }}
  oncontextmenu={(e) => {
    e.preventDefault();
    closeMenu();
  }}
>
  <div
    style="height:{$virtualizer.getTotalSize()}px; position:relative; width:100%"
  >
    {#each $virtualizer.getVirtualItems() as vrow (vrow.key)}
      <div
        class="thumb-row"
        style="transform:translateY({vrow.start}px); height:{ROW_H}px"
      >
        {#each Array(cols) as _, colI (colI)}
          {@const p = vrow.index * cols + colI + 1}
          {#if p <= pageCount}
            <div
              class="thumb-card"
              class:selected={selected.has(p)}
              class:dimmed={placing && selected.has(p)}
              style="width:{cardW}px; height:{THUMB_H}px"
              role="button"
              tabindex="-1"
              onclick={(e) => togglePage(p, e)}
              oncontextmenu={(e) => contextMenu(p, e)}
            >
              {#if placing && !selected.has(p)}
                <button
                  class="plus plus-right"
                  title="插入到本页之后"
                  onclick={(e) => placeClick(p, false, e)}
                >
                  <Plus class="h-4 w-4" />
                </button>
                <button
                  class="plus plus-left"
                  title="插入到本页之前"
                  onclick={(e) => placeClick(p, true, e)}
                >
                  <Plus class="h-4 w-4" />
                </button>
              {/if}
              <div class="thumb-imgwrap">
                {#if isBlank(p)}
                  <div class="blank-thumb" style={blankStyle(p)}></div>
                {:else if urlOf(p)}
                  <img
                    src={urlOf(p)!}
                    alt="第 {p} 页"
                    loading="lazy"
                    draggable="false"
                  />
                {:else}
                  <div
                    class="flex h-full w-full items-center justify-center text-muted-foreground"
                  >
                    <Loader class="h-5 w-5 animate-spin" />
                  </div>
                {/if}
              </div>
              <div class="thumb-label">
                <span class="label-num">第 {p} 页</span>
                {#if !busy}
                  <button
                    class="thumb-act thumb-preview"
                    title="预览本页"
                    onclick={(e) => {
                      e.stopPropagation();
                      previewPage = p;
                    }}
                  >
                    <Maximize2 class="h-3.5 w-3.5" />
                  </button>
                  <button
                    class="thumb-act thumb-del"
                    title="删除本页"
                    onclick={(e) => {
                      e.stopPropagation();
                      onDelete(String(p));
                    }}
                  >
                    <Trash2 class="h-3.5 w-3.5" />
                  </button>
                {/if}
              </div>
            </div>
          {:else}
            <div style="width:{cardW}px"></div>
          {/if}
        {/each}
      </div>
    {/each}
  </div>
</div>

{#if menu}
  <!-- svelte-ignore a11y_interactive_supports_focus -->
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div
    bind:this={menuEl}
    class="ctx-menu"
    style="left:{menu.x}px; top:{menu.y}px"
    role="menu"
    onclick={(e) => e.stopPropagation()}
  >
    <button class="ctx-item" onclick={menuMove}
      >移动（共 {menuPages.length} 页）</button
    >
    <button class="ctx-item ctx-danger" onclick={menuDelete}
      >删除（共 {menuPages.length} 页）</button
    >
  </div>
{/if}

{#if previewPage != null}
  <PagePreview
    page={previewPage}
    {pageCount}
    {pageRes}
    onClose={() => (previewPage = null)}
  />
{/if}

<style>
  .thumb-viewport {
    height: 100%;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 12px 12px 40px 12px;
  }
  .thumb-row {
    position: absolute;
    top: 0;
    left: 0;
    display: flex;
    gap: 14px;
    align-items: flex-start;
  }
  .blank-thumb {
    display: block;
    background: white;
  }
  .thumb-card {
    position: relative;
    flex: none;
    border-radius: 8px;
    cursor: pointer;
    user-select: none;
    border: 2px solid hsl(var(--border));
    background: hsl(var(--card));
    display: flex;
    flex-direction: column;
    transition: border-color 0.12s;
  }
  .thumb-card:hover {
    border-color: hsl(var(--primary) / 0.5);
  }
  .thumb-card.selected {
    border-color: hsl(var(--primary));
  }
  .thumb-card.dimmed {
    opacity: 0.3;
    pointer-events: none;
    filter: grayscale(0.5);
  }
  .thumb-imgwrap {
    height: 156px;
    border-radius: 6px 6px 0 0;
    overflow: hidden;
    background: hsl(var(--muted) / 0.4);
    pointer-events: none;
  }
  .thumb-imgwrap img {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
  .thumb-label {
    height: 26px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 12px;
    color: hsl(var(--muted-foreground));
    position: relative;
  }
  .thumb-act {
    position: absolute;
    top: 50%;
    transform: translateY(-50%);
    display: none;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    border-radius: 5px;
    color: hsl(var(--muted-foreground));
  }
  .thumb-preview {
    right: 2px;
  }
  .thumb-preview:hover {
    background: hsl(var(--primary) / 0.12);
    color: hsl(var(--primary));
  }
  .thumb-del {
    left: 2px;
    color: hsl(var(--destructive));
  }
  .thumb-del:hover {
    background: hsl(var(--destructive) / 0.12);
  }
  .thumb-card:hover .thumb-act {
    display: inline-flex;
  }
  .plus {
    position: absolute;
    top: 70px;
    z-index: 10;
    width: 26px;
    height: 26px;
    border-radius: 50%;
    background: hsl(var(--primary));
    color: hsl(var(--primary-foreground));
    display: flex;
    align-items: center;
    justify-content: center;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.4);
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.12s;
  }
  .thumb-card:hover .plus,
  .plus:focus-visible {
    opacity: 1;
  }
  .plus:hover {
    background: hsl(var(--primary) / 0.85);
  }
  .plus-left {
    left: -12px;
  }
  .plus-right {
    right: -12px;
  }
  .ctx-menu {
    position: fixed;
    z-index: 1000;
    min-width: 180px;
    background: hsl(var(--popover));
    border: 1px solid hsl(var(--border));
    border-radius: 8px;
    padding: 4px;
    box-shadow: 0 8px 30px rgba(0, 0, 0, 0.45);
  }
  .ctx-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 7px 10px;
    font-size: 13px;
    border-radius: 6px;
    cursor: pointer;
    color: hsl(var(--foreground));
    text-align: left;
  }
  .ctx-item:hover {
    background: hsl(var(--accent));
  }
  .ctx-danger:hover {
    background: hsl(var(--destructive) / 0.12);
    color: hsl(var(--destructive));
  }
</style>
