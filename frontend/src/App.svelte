<script lang="ts">
  import {
    DocumentService,
    type DocInfo,
    type OutlineNode,
    type PageLabel,
  } from "./bindings/services";
  import { pickPDF, pickImport, pickSavePDF } from "./lib/dialogs";
  import {
    currentDoc,
    pageRes,
    pageDims,
    setPageRes,
    setPageDims,
    replacePageRes,
    replacePageDims,
    openDocument,
    applyDocUpdate,
    startPlacement,
    stopPlacement,
    placement,
    notify,
    waitTask,
    outlineProblems,
    labelProblems,
    type AppProblem,
  } from "./stores";
  import ThumbGrid from "./components/ThumbGrid.svelte";
  import OutlineTab from "./components/OutlineTab.svelte";
  import LabelsTab from "./components/LabelsTab.svelte";
  import AttachmentsTab from "./components/AttachmentsTab.svelte";
  import TaskOverlay from "./components/TaskOverlay.svelte";
  import Toasts from "./components/Toasts.svelte";
  import * as UI from "./components/ui";
  import {
    FolderOpen,
    FileInput,
    FilePlus2,
    Save,
    Download,
    Loader2,
    SquareStack,
    ListTree,
    TableProperties,
    Paperclip,
    X,
    Move,
    FileQuestion,
    TriangleAlert,
    ArrowDownUp,
  } from "lucide-svelte";

  type Tab = "thumbs" | "outline" | "labels" | "attachments";

  const tabs: { id: Tab; label: string; icon: typeof SquareStack }[] = [
    { id: "thumbs", label: "缩略图", icon: SquareStack },
    { id: "outline", label: "书签", icon: ListTree },
    { id: "labels", label: "页标签", icon: TableProperties },
    { id: "attachments", label: "附件", icon: Paperclip },
  ];

  let tab: Tab = $state("thumbs");
  let busy = $state(false);
  let selected = $state<Set<number>>(new Set());

  let outlineTab: OutlineTab | null = $state(null);
  let labelsTab: LabelsTab | null = $state(null);
  let attachmentsTab: AttachmentsTab | null = $state(null);

  let doc = $derived($currentDoc);
  let placing = $derived($placement.active);

  let allProblems = $derived<AppProblem[]>([
    ...$outlineProblems,
    ...$labelProblems,
  ]);
  let showProblems = $state(false);

  /** 打开 PDF（按路径复用全局会话） */
  async function openFile() {
    tab = "thumbs"; // 页面类操作回到缩略图视图
    const [p] = await pickPDF(false);
    if (!p) return;
    busy = true;
    try {
      const d = await openDocument(p);
      applyDocUpdate(d);
      await ensureThumbs(d);
      selected = new Set();
    } catch (e: any) {
      notify("err", `打开失败：${e?.message ?? e}`);
    } finally {
      busy = false;
    }
  }

  /** 确保缩略图已生成：已有版本（本会话生成过）直接复用；文件在但无版本则初始化；否则全量生成 */
  /** 拉取页面内容资源（内容寻址；未加载过时后端渲染 140px 基图） */
  async function ensureThumbs(d: DocInfo) {
    const cur = $pageRes[d.id];
    if (Array.isArray(cur)) return; // 已就绪
    if (cur === null) return; // 拉取中
    if (d.pageRes && d.pageRes.length === d.pageCount) {
      setPageRes(d.id, d.pageRes);
      setPageDims(
        d.id,
        d.pages.map((p) => ({ ...p })),
      );
      return;
    }
    setPageRes(d.id, null);
    try {
      const res = await DocumentService.PageResources(d.id);
      setPageRes(d.id, res);
      setPageDims(
        d.id,
        d.pages.map((p) => ({ ...p })),
      );
    } catch (e: any) {
      setPageRes(d.id, null);
      notify("err", `缩略图加载失败：${e?.message ?? e}`);
    }
  }

  /** 当前逻辑页数（含未保存的插入/删除） */
  function logicalCount(id: string, fallback: number): number {
    const r = $pageRes[id];
    return Array.isArray(r) ? r.length : fallback;
  }

  /** 解析 "1,3-5" 形式的页码串为有序数组 */
  function parsePageSel(pages: string): number[] {
    const out: number[] = [];
    for (const seg of pages.split(",")) {
      if (seg.includes("-")) {
        const [a, b] = seg.split("-").map(Number);
        if (Number.isFinite(a) && Number.isFinite(b))
          for (let i = a; i <= b; i++) out.push(i);
      } else {
        const n = Number(seg);
        if (Number.isFinite(n)) out.push(n);
      }
    }
    return out.sort((x, y) => x - y);
  }

  // ---------- 工具栏 ----------

  async function doImport() {
    if (!doc) return;
    tab = "thumbs"; // 页面类操作回到缩略图视图
    const files = await pickImport(true);
    if (!files.length) return;
    busy = true;
    try {
      const tid = await DocumentService.Import(doc.id, files);
      const res = await waitTask(tid);
      selected = new Set(); // 导入放置期间临时清空选中，避免误移动
      startPlacement("import", {
        id: res?.bufferID ?? "",
        pageCount: res?.pageCount ?? 0,
        res: res?.res ?? [],
        dims: res?.dims ?? [],
      });
    } catch (e: any) {
      notify("err", `导入失败：${e?.message ?? e}`);
    } finally {
      busy = false;
    }
  }

  async function doSave() {
    if (!doc) return;
    if (allProblems.length) {
      // 理论上进不来（按钮已被替换），保留兜底
      showProblems = true;
      return;
    }
    busy = true;
    try {
      const d = await DocumentService.Save(doc.id, collectSaveOptions());
      applyDocUpdate(d);
      notify("ok", "已保存");
    } catch (e: any) {
      notify("err", `保存失败：${e?.message ?? e}`);
    } finally {
      busy = false;
    }
  }

  async function doSaveAs() {
    if (!doc) return;
    if (allProblems.length) {
      showProblems = true;
      return;
    }
    const out = await pickSavePDF(
      doc.fileName.replace(/\.pdf$/i, "") + "-副本.pdf",
    );
    if (!out) return;
    busy = true;
    try {
      const d = await DocumentService.SaveAs(doc.id, out, collectSaveOptions());
      applyDocUpdate(d);
      notify("ok", `已保存到：${out.split(/[\\/]/).pop()}`);
    } catch (e: any) {
      notify("err", `保存失败：${e?.message ?? e}`);
    } finally {
      busy = false;
    }
  }

  /** 收集当前文档的全部可保存数据；只要文档打开就返回完整对象 */
  function collectSaveOptions() {
    if (!doc) return null;
    const opts: {
      outline?: OutlineNode[];
      labels?: PageLabel[];
      pageSeq?: string[];
    } = {};

    const tree = outlineTab?.getTree();
    if (tree !== undefined) opts.outline = tree;

    const labels = labelsTab?.getLabels();
    if (labels !== undefined) opts.labels = labels;

    const seq = $pageRes[doc.id];
    if (Array.isArray(seq)) opts.pageSeq = seq;

    return opts;
  }

  // ---------- 放置流 ----------

  /** 「插入空白页」开始 */
  function beginBlank() {
    tab = "thumbs"; // 页面类操作回到缩略图视图
    selected = new Set(); // 空白页放置期间临时清空选中
    startPlacement("blank", null);
  }

  /** 「移动到」开始（由 ThumbGrid 右键菜单触发） */
  function beginMove(pages: string) {
    startPlacement("move", null);
    movePagesSel = pages;
  }
  let movePagesSel = "";

  async function handlePlace(atIndex: number, before: boolean) {
    if (!doc) return;
    const res = $pageRes[doc.id];
    const dims = $pageDims[doc.id];
    if (!Array.isArray(res) || !Array.isArray(dims)) return;
    busy = true;
    try {
      if ($placement.kind === "move") {
        const selPages = [...selected].sort((a, b) => a - b);
        const selSet = new Set(selPages);
        const moved = selPages.map((n) => res[n - 1]);
        const movedDims = selPages.map((n) => dims[n - 1]);
        const rest = res.filter((_, i) => !selSet.has(i + 1));
        const restDims = dims.filter((_, i) => !selSet.has(i + 1));

        // 目标插入位（原序列 1-based，表示“插入到该索引页之前”）
        const anchor = before ? atIndex : atIndex + 1;
        // 关键：比较基准固定为 anchor，不随递减变化
        let removedBefore = 0;
        for (const n of selPages) if (n < anchor) removedBefore++;
        const insertAt = Math.max(
          1,
          Math.min(anchor - removedBefore, rest.length + 1),
        );

        replacePageRes(doc.id, [
          ...rest.slice(0, insertAt - 1),
          ...moved,
          ...rest.slice(insertAt - 1),
        ]);
        replacePageDims(doc.id, [
          ...restDims.slice(0, insertAt - 1),
          ...movedDims,
          ...restDims.slice(insertAt - 1),
        ]);

        const nextSel = new Set<number>();
        for (let i = 0; i < moved.length; i++) nextSel.add(insertAt + i);
        selected = nextSel;
        stopPlacement();
        notify("ok", `已移动 ${selPages.length} 页`);
      } else if ($placement.kind === "blank") {
        // 空白页：尺寸 = 被点击 ⊕ 的那个页面（锚点页）的尺寸；保存时由后端生成
        const anchorIdx = Math.min(Math.max(atIndex, 1), dims.length);
        const ad = dims[anchorIdx - 1] ??
          dims[0] ?? { width: 595, height: 842 };
        const marker = `blank-${Math.round(ad.width)}x${Math.round(ad.height)}`;
        const insertAt = before ? atIndex : atIndex + 1;
        const next = [
          ...res.slice(0, insertAt - 1),
          marker,
          ...res.slice(insertAt - 1),
        ];
        replacePageRes(doc.id, next);
        replacePageDims(doc.id, [
          ...dims.slice(0, insertAt - 1),
          { ...ad },
          ...dims.slice(insertAt - 1),
        ]);
        stopPlacement();
        notify("ok", "已插入空白页");
      } else {
        // 导入放置：缓冲页资源 uuid 直接拼接
        const bres = $placement.buffer?.res ?? [];
        const bdims = $placement.buffer?.dims ?? [];
        const insertAt = before ? atIndex : atIndex + 1;
        replacePageRes(doc.id, [
          ...res.slice(0, insertAt - 1),
          ...bres,
          ...res.slice(insertAt - 1),
        ]);
        replacePageDims(doc.id, [
          ...dims.slice(0, insertAt - 1),
          ...bdims.map((d) => ({ ...d })),
          ...dims.slice(insertAt - 1),
        ]);
        // 缓冲区辅助数据（书签/页标签/附件）按插入位置平移合并
        const r = await DocumentService.AbsorbBufferAux(
          doc.id,
          atIndex,
          before,
        );
        if (r?.addedOutline?.length) outlineTab?.addNodes(r.addedOutline);
        if (r?.addedLabels?.length) labelsTab?.mergeLabels(r.addedLabels);
        if (r?.addedAttachments?.length) await attachmentsTab?.reload();
        stopPlacement();
        notify("ok", `已放置导入内容 ${bres.length} 页`);
      }
    } finally {
      busy = false;
    }
  }

  async function handleCancelPlace() {
    if ($placement.kind === "import" && doc) {
      try {
        await DocumentService.DiscardBuffer(doc.id);
      } catch {
        /* 忽略 */
      }
    }
    stopPlacement();
  }
</script>

<div class="flex h-screen w-screen flex-col overflow-hidden">
  <!-- 顶部工具栏（放置模式下暗化禁用） -->
  <header
    class="flex h-14 shrink-0 items-center gap-3 border-b bg-card px-4 transition-opacity"
    class:dimmed={placing}
  >
    <UI.Button onclick={openFile} disabled={busy}>
      <FolderOpen class="h-4 w-4" /> 打开 PDF
    </UI.Button>
    <UI.Button variant="secondary" onclick={doImport} disabled={busy || !doc}>
      <FileInput class="h-4 w-4" /> 导入图片 / PDF
    </UI.Button>
    <UI.Button
      variant="secondary"
      onclick={beginBlank}
      disabled={busy || !doc || placing}
    >
      <FilePlus2 class="h-4 w-4" /> 插入空白页
    </UI.Button>
    <span class="flex-1"></span>
    {#if doc}
      <span class="truncate text-sm text-muted-foreground">
        {doc.fileName} · {logicalCount(doc.id, doc.pageCount)} 页
      </span>
    {:else}
      <span class="text-sm text-muted-foreground">未打开文档</span>
    {/if}
    <span class="flex-1"></span>
    {#if allProblems.length}
      <UI.Button
        variant="secondary"
        onclick={() => (showProblems = true)}
        title="存在校验问题，请先处理"
      >
        <TriangleAlert class="h-4 w-4 text-amber-600" />
        问题
        <span
          class="ml-0.5 rounded-full bg-amber-500/20 px-1.5 text-xs font-medium text-amber-600"
        >
          {allProblems.length}
        </span>
      </UI.Button>
    {:else}
      <UI.Button variant="outline" onclick={doSave} disabled={busy || !doc}>
        <Save class="h-4 w-4" /> 保存
      </UI.Button>
      <UI.Button variant="outline" onclick={doSaveAs} disabled={busy || !doc}>
        <Download class="h-4 w-4" /> 另存为
      </UI.Button>
    {/if}
  </header>

  <!-- 放置模式提示条 -->
  {#if placing}
    <div
      class="flex h-10 shrink-0 items-center justify-center gap-3 bg-primary/15 text-sm text-foreground"
    >
      <Move class="h-4 w-4 text-primary" />
      {#if $placement.kind === "move"}
        点击目标缩略图左右两侧的 ⊕ 插入所选 {selected.size} 页
      {:else}
        点击缩略图左右两侧的 ⊕ 放置导入内容
      {/if}
      <UI.Button variant="ghost" size="sm" onclick={handleCancelPlace}>
        <X class="h-3.5 w-3.5" /> 取消
      </UI.Button>
    </div>
  {/if}

  <!-- 主内容区（keep-alive） -->
  <main class="relative min-h-0 flex-1">
    <div style:display={tab === "thumbs" ? "block" : "none"} class="h-full">
      {#if doc}
        <ThumbGrid
          pageCount={logicalCount(doc.id, doc.pageCount)}
          docID={doc.id}
          pageRes={$pageRes[doc.id] ?? null}
          {selected}
          {busy}
          onSelectionChange={(s) => (selected = s)}
          onDelete={async (pages) => {
            if (!doc) return;
            const res = $pageRes[doc.id];
            const dims = $pageDims[doc.id];
            if (!Array.isArray(res) || !Array.isArray(dims)) return;
            const del = new Set(parsePageSel(pages));
            const next = res.filter((_, i) => !del.has(i + 1));
            if (!next.length) {
              notify("err", "不能删除全部页面");
              return;
            }
            replacePageRes(doc.id, next);
            replacePageDims(
              doc.id,
              dims.filter((_, i) => !del.has(i + 1)),
            );
            selected = new Set();
            notify("ok", "已删除所选页面");
          }}
          {placing}
          buffer={$placement.buffer}
          onPlace={handlePlace}
          onStartMove={beginMove}
          onCancelPlace={handleCancelPlace}
        />
      {:else}
        <div
          class="flex h-full flex-col items-center justify-center gap-3 text-muted-foreground"
        >
          <FileQuestion class="h-12 w-12" />
          <p class="text-lg">打开 PDF 开始编辑</p>
        </div>
      {/if}
    </div>
    <div
      style:display={tab === "outline" ? "block" : "none"}
      class="h-full overflow-y-auto"
    >
      <OutlineTab
        bind:this={outlineTab}
        pageCount={doc ? logicalCount(doc.id, doc.pageCount) : null}
      />
    </div>
    <div
      style:display={tab === "labels" ? "block" : "none"}
      class="h-full overflow-y-auto"
    >
      <LabelsTab
        bind:this={labelsTab}
        pageCount={doc ? logicalCount(doc.id, doc.pageCount) : null}
      />
    </div>
    <div
      style:display={tab === "attachments" ? "block" : "none"}
      class="h-full overflow-y-auto"
    >
      <AttachmentsTab bind:this={attachmentsTab} />
    </div>
  </main>

  <!-- 底部标签页 -->
  <footer
    class="flex h-12 shrink-0 items-center gap-1 border-t bg-card px-3"
    class:dimmed={placing}
  >
    {#each tabs as t (t.id)}
      <button
        class="flex items-center gap-2 rounded-md px-3 py-1.5 text-sm transition-colors
               {tab === t.id
          ? 'bg-primary/15 font-medium text-foreground'
          : 'text-muted-foreground hover:bg-accent hover:text-foreground'}"
        onclick={() => (tab = t.id)}
      >
        <t.icon class="h-4 w-4 {tab === t.id ? 'text-primary' : ''}" />
        {t.label}
      </button>
    {/each}
    {#if busy}
      <span
        class="ml-auto flex items-center gap-1.5 text-xs text-muted-foreground"
      >
        <Loader2 class="h-3.5 w-3.5 animate-spin" /> 处理中
      </span>
    {/if}
  </footer>
</div>

<TaskOverlay />
{#if showProblems}
  <!-- svelte-ignore a11y_interactive_supports_focus -->
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div
    class="log-overlay"
    role="dialog"
    aria-modal="true"
    onclick={(e) => e.target === e.currentTarget && (showProblems = false)}
  >
    <div class="log-card">
      <div
        class="flex items-center justify-between border-b border-border px-4 py-3"
      >
        <h3 class="flex items-center gap-2 text-sm font-medium">
          <TriangleAlert class="h-4 w-4 text-amber-600" /> 校验问题
          <span class="text-xs font-normal text-muted-foreground">
            （{allProblems.length} 项，点击可跳转对应页处理）
          </span>
        </h3>
        <button class="pv-btn" onclick={() => (showProblems = false)}>
          <X class="h-4 w-4" />
        </button>
      </div>
      <div class="max-h-[50vh] overflow-y-auto px-2 py-2">
        <ul class="flex flex-col gap-1">
          {#each allProblems as p, i (i)}
            <li>
              <button
                type="button"
                class="flex w-full items-start gap-2 rounded-md px-3 py-2 text-left text-sm transition-colors hover:bg-accent"
                onclick={() => {
                  tab = p.tab;
                  showProblems = false;
                }}
              >
                {#if p.kind === "order"}
                  <ArrowDownUp class="mt-0.5 h-4 w-4 shrink-0 text-amber-600" />
                {:else}
                  <TriangleAlert
                    class="mt-0.5 h-4 w-4 shrink-0 text-amber-600"
                  />
                {/if}
                <div class="flex-1">
                  <div class="flex items-center gap-2">
                    <span class="font-medium">{p.title}</span>
                    <span
                      class="rounded-sm bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground"
                    >
                      {p.source === "outline" ? "书签" : "页标签"}
                    </span>
                  </div>
                  <div class="text-xs text-muted-foreground">{p.detail}</div>
                </div>
              </button>
            </li>
          {/each}
        </ul>
      </div>
      <div class="flex justify-end border-t border-border px-4 py-3">
        <UI.Button variant="outline" onclick={() => (showProblems = false)}>
          关闭
        </UI.Button>
      </div>
    </div>
  </div>
{/if}
<Toasts />

<style>
  .dimmed {
    opacity: 0.25;
    pointer-events: none;
    filter: grayscale(0.4);
  }
  .log-overlay {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgb(0 0 0 / 0.5);
  }
  .log-card {
    width: min(560px, 92vw);
    background: hsl(var(--card));
    border: 1px solid hsl(var(--border));
    border-radius: 10px;
    overflow: hidden;
    box-shadow: 0 12px 40px rgb(0 0 0 / 0.35);
  }
  .pv-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border-radius: 6px;
    color: hsl(var(--muted-foreground));
  }
  .pv-btn:hover {
    background: hsl(var(--accent));
    color: hsl(var(--foreground));
  }
</style>
