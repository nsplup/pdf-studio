<script lang="ts">
  import { onMount } from "svelte";
  import { notify } from "../stores";
  import { X, ChevronLeft, ChevronRight, ZoomIn, ZoomOut, RotateCcw, Loader2 } from "lucide-svelte";

  let {
    page = 1,
    pageCount = 0,
    pageRes = null as string[] | null,
    onClose = () => {},
  }: {
    page?: number;
    pageCount?: number;
    /** 页面内容资源 uuid 序列（与缩略图同源） */
    pageRes?: string[] | null;
    onClose?: () => void;
  } = $props();

  let cur = $state(page);
  let url = $state("");
  let loading = $state(true);
  let scale = $state(1);
  let tx = $state(0);
  let ty = $state(0);

  let isBlankPage = $state(false);
  let dragging = $state(false);
  let dragStart = { x: 0, y: 0, tx: 0, ty: 0 };

  const MIN_SCALE = 0.2;
  const MAX_SCALE = 8;

  // 内容寻址：地址即内容，无版本失效问题
  $effect(() => {
    const p = cur;
    const u = pageRes?.[p - 1] ?? "";
    if (!u) {
      url = "";
      loading = false;
      return;
    }
    if (u.startsWith("blank-")) {
      url = "";
      loading = false;
      isBlankPage = true;
      return;
    }
    isBlankPage = false;
    url = `/thumbs/${u}/900.png`;
    loading = true;
    resetView();
  });

  /** 空白页预览：按标记尺寸呈现，并应用与普通页面一致的平移/缩放变换 */
  function blankPreviewStyle(): string {
    const u = pageRes?.[cur - 1] ?? "";
    const m = u.match(/^blank-([\d.]+)x([\d.]+)$/);
    const w = m ? parseFloat(m[1]) : 595;
    const h = m ? parseFloat(m[2]) : 842;
    const land = w >= h;
    return `aspect-ratio:${w}/${h}; ${land ? "width:70%; height:auto;" : "height:80%; width:auto;"} transform: translate(${tx}px, ${ty}px) scale(${scale});`;
  }

  function resetView() {
    scale = 1;
    tx = 0;
    ty = 0;
  }

  function go(delta: number) {
    const next = cur + delta;
    if (next < 1 || next > pageCount) return;
    cur = next;
  }

  function zoomAt(factor: number) {
    scale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, scale * factor));
  }

  function onWheel(e: WheelEvent) {
    e.preventDefault();
    zoomAt(e.deltaY < 0 ? 1.15 : 1 / 1.15);
  }

  function onDown(e: MouseEvent) {
    dragging = true;
    dragStart = { x: e.clientX, y: e.clientY, tx, ty };
  }
  function onMove(e: MouseEvent) {
    if (!dragging) return;
    tx = dragStart.tx + (e.clientX - dragStart.x);
    ty = dragStart.ty + (e.clientY - dragStart.y);
  }
  function onUp() {
    dragging = false;
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") onClose();
    else if (e.key === "ArrowLeft") go(-1);
    else if (e.key === "ArrowRight") go(1);
    else if (e.key === "+" || e.key === "=") zoomAt(1.2);
    else if (e.key === "-") zoomAt(1 / 1.2);
  }

  onMount(() => {
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });
</script>

<svelte:window onmouseup={onUp} />
<div
  class="preview-overlay"
  role="dialog"
  aria-modal="true"
  onwheel={onWheel}
  onmousedown={onDown}
  onmousemove={onMove}
>
  {#if isBlankPage}
    <div
      class="preview-img"
      class:grabbing={dragging}
      style="background:white; {blankPreviewStyle()}"
    ></div>
  {:else if url}
    <img
      class="preview-img"
      class:grabbing={dragging}
      onload={() => (loading = false)}
      onerror={() => {
        loading = false;
        notify("err", "预览资源加载失败");
      }}
      src={url}
      alt={`第 ${cur} 页预览`}
      draggable="false"
      style="transform: translate({tx}px, {ty}px) scale({scale})"
    />
    {#if loading}
      <div class="preview-spin"><Loader2 class="h-8 w-8 animate-spin" /></div>
    {/if}
  {:else}
    <div class="preview-spin text-sm text-muted-foreground">预览资源未就绪，请关闭后重试</div>
  {/if}

  <div class="preview-topbar">
    <span class="preview-title">第 {cur} / {pageCount} 页 · 滚轮缩放 · 拖拽平移</span>
    <span class="flex-1"></span>
    <button class="pv-btn" title="缩小" onclick={() => zoomAt(1 / 1.2)}><ZoomOut class="h-4 w-4" /></button>
    <span class="pv-scale">{Math.round(scale * 100)}%</span>
    <button class="pv-btn" title="放大" onclick={() => zoomAt(1.2)}><ZoomIn class="h-4 w-4" /></button>
    <button class="pv-btn" title="重置视图" onclick={resetView}><RotateCcw class="h-4 w-4" /></button>
    <button class="pv-btn" title="关闭 (Esc)" onclick={onClose}><X class="h-4 w-4" /></button>
  </div>

  {#if cur > 1}
    <button class="preview-nav preview-prev" title="上一页 (←)" onclick={() => go(-1)}>
      <ChevronLeft class="h-6 w-6" />
    </button>
  {/if}
  {#if cur < pageCount}
    <button class="preview-nav preview-next" title="下一页 (→)" onclick={() => go(1)}>
      <ChevronRight class="h-6 w-6" />
    </button>
  {/if}
</div>

<style>
  .preview-overlay {
    position: fixed; inset: 0; z-index: 1200;
    background: rgba(0, 0, 0, 0.82);
    display: flex; align-items: center; justify-content: center;
    overflow: hidden; cursor: grab;
    user-select: none;
  }
  .preview-overlay:active { cursor: grabbing; }
  .preview-spin {
    position: absolute; inset: 0;
    display: flex; align-items: center; justify-content: center;
    color: hsl(var(--muted-foreground));
  }
  .preview-img {
    max-width: 92vw; max-height: 86vh;
    box-shadow: 0 10px 50px rgba(0, 0, 0, 0.6);
    background: white;
    transition: transform 0.06s linear;
    pointer-events: none;
  }
  .preview-img.grabbing { transition: none; }
  .preview-topbar {
    position: absolute; top: 0; left: 0; right: 0;
    display: flex; align-items: center; gap: 6px;
    padding: 10px 14px;
    background: linear-gradient(rgba(0,0,0,0.55), transparent);
    color: hsl(var(--foreground));
  }
  .preview-title { font-size: 13px; opacity: 0.85; }
  .pv-scale { font-size: 12px; min-width: 44px; text-align: center; opacity: 0.85; }
  .pv-btn {
    display: flex; align-items: center; justify-content: center;
    height: 30px; width: 30px; border-radius: 6px;
    color: white; background: rgba(255, 255, 255, 0.12);
    cursor: pointer;
  }
  .pv-btn:hover { background: rgba(255, 255, 255, 0.22); }
  .preview-nav {
    position: absolute; top: 50%; transform: translateY(-50%);
    display: flex; align-items: center; justify-content: center;
    height: 46px; width: 46px; border-radius: 50%;
    background: rgba(255, 255, 255, 0.12); color: white;
    cursor: pointer;
  }
  .preview-nav:hover { background: rgba(255, 255, 255, 0.25); }
  .preview-prev { left: 18px; }
  .preview-next { right: 18px; }
</style>
