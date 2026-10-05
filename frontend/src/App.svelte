<script lang="ts">
  import {
    AppService,
    DocumentService,
    type DocInfo,
    type OutlineNode,
    type PageDim, // ← 新增
    type PageLabel,
  } from "@bindings";
  import { Events } from "@wailsio/runtime";
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
    pageResBaseline,
    snapshotPageRes,
    hasUnsavedChanges,
    refreshDirtyFlag,
    outlineDirty,
    labelsDirty,
    attachmentsDirty,
    clearDocState,
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
    Loader,
    SquareStack,
    ListTree,
    TableProperties,
    Paperclip,
    X,
    Move,
    FileQuestionMark,
    TriangleAlert,
    ArrowDownUp,
    CircleDotDashedIcon,
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
  let isDirty = $derived.by(() => {
    if (!doc) return false;
    const cur = $pageRes[doc.id];
    const base = $pageResBaseline[doc.id];
    if (Array.isArray(cur) && Array.isArray(base)) {
      if (cur.length !== base.length) return true;
      for (let i = 0; i < cur.length; i++) {
        if (cur[i] !== base[i]) return true;
      }
    }
    if ($outlineDirty) return true;
    if ($labelsDirty) return true;
    if ($attachmentsDirty) return true;
    return false;
  });

  interface ConfirmState {
    open: boolean;
    title: string;
    body: string;
    confirmText: string;
    destructive: boolean;
    resolve: ((ok: boolean) => void) | null;
  }

  let confirmState = $state<ConfirmState>({
    open: false,
    title: "",
    body: "",
    confirmText: "确认",
    destructive: false,
    resolve: null,
  });

  function askConfirm(opts: {
    title: string;
    body: string;
    confirmText?: string;
    destructive?: boolean;
  }): Promise<boolean> {
    return new Promise((resolve) => {
      confirmState = {
        open: true,
        title: opts.title,
        body: opts.body,
        confirmText: opts.confirmText ?? "确认",
        destructive: opts.destructive ?? false,
        resolve,
      };
    });
  }

  function resolveConfirm(ok: boolean) {
    const r = confirmState.resolve;
    confirmState = { ...confirmState, open: false, resolve: null };
    r?.(ok);
  }

  function removeFileExtension(str: string): string {
    if (typeof str !== "string") {
      throw new TypeError("参数必须是字符串");
    }

    // 匹配：普通字符 + 最后一个点 + 点后的非点/非路径分隔符字符
    // 然后用前面的普通字符替换整个匹配，相当于删除扩展名
    return str.replace(/([^./\\])\.[^./\\]+$/, "$1");
  }

  /** 打开 PDF（按路径复用全局会话） */
  async function openFile() {
    if (isDirty) {
      const ok = await askConfirm({
        title: "有未保存的改动",
        body: "当前文档有未保存的改动，打开新文件将丢失这些改动。是否继续？",
        confirmText: "放弃改动并打开",
        destructive: true,
      });
      if (!ok) return;
    }
    tab = "thumbs";
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

  async function ensureThumbs(d: DocInfo) {
    const cur = $pageRes[d.id];
    if (Array.isArray(cur)) return;
    if (cur === null) return;
    if (d.pageRes && d.pageRes.length === d.pageCount) {
      setPageRes(d.id, d.pageRes);
      setPageDims(
        d.id,
        d.pages.map((p) => ({ ...p })),
      );
      snapshotPageRes(d.id, d.pageRes);
      return;
    }
    setPageRes(d.id, null);
    try {
      const tid = await DocumentService.PageResources(d.id);
      const res = await waitTask(tid);
      setPageRes(d.id, res as string[]);
      setPageDims(
        d.id,
        d.pages.map((p) => ({ ...p })),
      );
      snapshotPageRes(d.id, res as string[]);
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

  /**
   * 解析 "1,3-5" 形式的页码串为升序去重的有效页码数组。
   * 严格校验：空段、非数字、0/负数、倒序区间、超出 [1,total] 均视为非法，
   * 整体返回 null。调用方据此提示错误并中止操作，不再静默 no-op。
   */
  function parsePageSel(pages: string, total: number): number[] | null {
    if (total < 1) return null;
    const trimmed = pages.trim();
    if (!trimmed) return null;

    const out = new Set<number>();
    for (const rawSeg of trimmed.split(",")) {
      const seg = rawSeg.trim();
      if (!seg) return null; // 空段：如 "1,,3"、末尾逗号

      const dash = seg.indexOf("-");
      if (dash >= 0) {
        // 区间：只接受恰好一个 "-"，两侧均为纯数字
        const aStr = seg.slice(0, dash).trim();
        const bStr = seg.slice(dash + 1).trim();
        if (seg.indexOf("-", dash + 1) >= 0) return null; // "1-2-3"
        if (!/^\d+$/.test(aStr) || !/^\d+$/.test(bStr)) return null;
        const a = parseInt(aStr, 10);
        const b = parseInt(bStr, 10);
        if (a < 1 || b < 1 || a > b) return null; // "3-1"、"0-3"
        if (b > total) return null; // 越界
        for (let i = a; i <= b; i++) out.add(i);
      } else {
        if (!/^\d+$/.test(seg)) return null; // "abc"、"+1"、"1.5"
        const n = parseInt(seg, 10);
        if (n < 1 || n > total) return null;
        out.add(n);
      }
    }

    if (out.size === 0) return null;
    return [...out].sort((x, y) => x - y);
  }

  // ---------- 工具栏 ----------

  async function doImport() {
    // 分支一：已有文档 → 原有「导入到缓冲区 + 放置」
    if (doc) {
      tab = "thumbs";
      const files = await pickImport(true);
      if (!files.length) return;
      busy = true;
      try {
        const tid = await DocumentService.Import(doc.id, { paths: files });
        const res = await waitTask(tid);
        selected = new Set();
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
      return;
    }

    // 分支二：无文档 → 新建空白 + 导入 + 直接赋为页面列表
    const files = await pickImport(true);
    if (!files.length) return;
    busy = true;
    let newDocID = "";
    try {
      const d = await DocumentService.CreateBlank();
      if (d === null) throw new TypeError("文档类型不能为 null");
      newDocID = d.id;
      applyDocUpdate(d);

      // baseline = 空（尚未保存过任何页面内容）
      setPageRes(d.id, []);
      setPageDims(d.id, []);
      snapshotPageRes(d.id, []);

      // 预扫描总页数 → 渲染等量 loading 占位符
      let placeholderN = 0;
      try {
        placeholderN = await DocumentService.ScanImportCount(files);
      } catch {
        /* 扫描失败退化为空网格 */
      }
      if (placeholderN > 0) {
        setPageRes(d.id, Array(placeholderN).fill(""));
        setPageDims(
          d.id,
          Array.from({ length: placeholderN }, () => ({
            width: 595,
            height: 842,
          })),
        );
      }

      // 异步导入
      const tid = await DocumentService.Import(d.id, { paths: files });
      const res = await waitTask(tid);
      const bres: string[] = res?.res ?? [];
      const bdims: PageDim[] = (res?.dims ?? []).map((x: any) => ({
        width: x.width,
        height: x.height,
      }));
      if (!bres.length) throw new Error("导入内容为空");

      const r = await DocumentService.AbsorbBufferAux(d.id, 1, true);

      // 原子替换占位符为真实资源；不再 snapshotPageRes（保持 dirty）
      replacePageRes(d.id, bres);
      replacePageDims(d.id, bdims);

      if (r?.addedOutline?.length) outlineTab?.addNodes(r.addedOutline);
      if (r?.addedLabels?.length) labelsTab?.mergeLabels(r.addedLabels);
      if (r?.addedAttachments?.length) await attachmentsTab?.reload();

      tab = "thumbs";
      selected = new Set();
      notify("ok", `已导入 ${bres.length} 页`);
    } catch (e: any) {
      if (newDocID) {
        try {
          await DocumentService.Close(newDocID);
        } catch {}
        currentDoc.set(null);
      }
      notify("err", `导入失败：${e?.message ?? e}`);
    } finally {
      busy = false;
    }
  }

  async function doSave() {
    if (!doc) return;
    if (allProblems.length) {
      showProblems = true;
      return;
    }
    // 尚未保存过（新建空白文档导入后）：走另存为，先让用户选保存位置
    if (!doc.sourcePath) {
      await doSaveAs();
      return;
    }
    busy = true;
    try {
      const d = await DocumentService.Save(doc.id, collectSaveOptions());
      if (d === null) throw new TypeError("文档类型不能为 null");
      applyDocUpdate(d);
      if (Array.isArray($pageRes[doc.id]))
        snapshotPageRes(doc.id, $pageRes[doc.id] as string[]);
      outlineTab?.markClean?.();
      labelsTab?.markClean?.();
      attachmentsTab?.markClean?.();
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
    const base = (doc.fileName || "未命名").replace(/\.pdf$/i, "");
    const suggested = doc.sourcePath ? `${base}-副本.pdf` : `${base}.pdf`;
    const out = await pickSavePDF(suggested);
    if (!out) return;
    busy = true;
    try {
      const d = await DocumentService.SaveAs(doc.id, out, collectSaveOptions());
      if (d === null) throw new TypeError("文档类型不能为 null");
      applyDocUpdate(d);
      if (Array.isArray($pageRes[doc.id]))
        snapshotPageRes(doc.id, $pageRes[doc.id] as string[]);
      outlineTab?.markClean?.();
      labelsTab?.markClean?.();
      attachmentsTab?.markClean?.();
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
    if (!Array.isArray(res) || !Array.isArray(dims)) {
      notify("err", "页面资源尚未加载完成，请稍后再试");
      return;
    }
    busy = true;
    try {
      if ($placement.kind === "move") {
        const selPages = [...selected].sort((a, b) => a - b);
        const selSet = new Set(selPages);
        const moved = selPages.map((n) => res[n - 1]);
        const movedDims = selPages.map((n) => dims[n - 1]);
        const rest = res.filter((_, i) => !selSet.has(i + 1));
        const restDims = dims.filter((_, i) => !selSet.has(i + 1));

        const anchor = before ? atIndex : atIndex + 1;
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
        // 导入放置：先让后端消费缓冲区（合并书签/页标签/附件），
        // 成功后再一次性写入前端状态。失败则前端保持原状，
        // placement 保留，用户可重试或取消。
        const bres = $placement.buffer?.res ?? [];
        const bdims = $placement.buffer?.dims ?? [];
        if (!bres.length) {
          notify("err", "导入缓冲区为空，已取消放置");
          stopPlacement();
          return;
        }

        // 1) 后端：消费 buffer 并返回平移后的辅助数据。
        //    这一步失败会抛错，进入外层 catch。
        const r = await DocumentService.AbsorbBufferAux(
          doc.id,
          atIndex,
          before,
        );

        // 2) 前端：后端已成功，原子写入页面序列与尺寸。
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

        // 3) 合并辅助数据到各子页面。
        //    B7 修复后，若对应 Tab 尚未成功加载，addNodes/mergeLabels
        //    会丢弃增量并 notify，不会把空数组误写回。
        if (r?.addedOutline?.length) outlineTab?.addNodes(r.addedOutline);
        if (r?.addedLabels?.length) labelsTab?.mergeLabels(r.addedLabels);
        if (r?.addedAttachments?.length) await attachmentsTab?.reload();

        stopPlacement();
        notify("ok", `已放置导入内容 ${bres.length} 页`);
      }
    } catch (e: any) {
      // 不再吞异常：显式提示，placement 保留，
      // 用户可重试或点「取消」（handleCancelPlace 会 DiscardBuffer）。
      notify("err", `放置失败：${e?.message ?? e}`);
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

  $effect(() => {
    refreshDirtyFlag(isDirty);
    void AppService.SetUnsavedChanges(isDirty);
  });
  $effect(() => {
    const unsub = Events.On("app:close-requested", async () => {
      const ok = await askConfirm({
        title: "有未保存的改动",
        body: "当前文档有未保存的改动，确定要退出吗？",
        confirmText: "放弃改动并退出",
        destructive: true,
      });
      if (ok) await AppService.ForceQuit();
    });
    return unsub;
  });

  async function doClose() {
    if (!doc) return;
    if (isDirty) {
      const ok = await askConfirm({
        title: "有未保存的改动",
        body: "关闭文档将丢失未保存的改动，是否继续？",
        confirmText: "放弃改动并关闭",
        destructive: true,
      });
      if (!ok) return;
    }
    busy = true;
    try {
      const id = doc.id;
      await DocumentService.Close(id);

      // 清 store
      clearDocState(id);
      currentDoc.set(null);

      // 清 Tab 内部状态（按依赖顺序：先清 Tab，再清派生问题列表）
      outlineTab?.reset?.();
      labelsTab?.reset?.();
      attachmentsTab?.reset?.();

      // 主页面自身
      selected = new Set();
      tab = "thumbs";
      movePagesSel = "";
      showProblems = false;
      stopPlacement();

      notify("ok", "文档已关闭");
    } catch (e: any) {
      notify("err", `关闭失败：${e?.message ?? e}`);
    } finally {
      busy = false;
    }
  }
  /** 判断事件目标是否为可编辑控件（输入框 / 文本域 / contenteditable） */
  function isEditableTarget(t: EventTarget | null): boolean {
    if (!(t instanceof HTMLElement)) return false;
    const tag = t.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
    if (t.isContentEditable) return true;
    return false;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.repeat) return;

    const mod = e.metaKey || e.ctrlKey;
    const key = e.key.toLowerCase();

    // ---- 带修饰键：任何焦点位置都生效（含输入框内） ----
    if (mod && !e.altKey) {
      if (key === "s") {
        e.preventDefault();
        if (!doc || busy) return;
        if (e.shiftKey) void doSaveAs();
        else void doSave();
        return;
      }
      if (key === "o") {
        e.preventDefault();
        if (busy) return;
        void openFile();
        return;
      }
      if (key === "w") {
        e.preventDefault();
        if (busy) return;
        void doClose();
        return;
      }
      return;
    }

    // ---- 无修饰键：仅当焦点不在输入控件中才生效 ----
    if (isEditableTarget(e.target)) return;

    if (e.key === "Escape") {
      if (confirmState.open) {
        e.preventDefault();
        resolveConfirm(false);
        return;
      }
      if (showProblems) {
        e.preventDefault();
        showProblems = false;
        return;
      }
      if (placing) {
        e.preventDefault();
        void handleCancelPlace();
        return;
      }
    }
  }

  /** 界面就绪 */
  $effect(() => {
    void AppService.Ready();
  });
</script>

<svelte:window onkeydown={handleKeydown} />
<div class="flex h-screen w-screen flex-col overflow-hidden">
  <!-- 顶部工具栏（放置模式下暗化禁用） -->
  <header
    class="flex h-14 shrink-0 items-center gap-3 overflow-hidden border-b bg-card px-4 transition-opacity"
    class:dimmed={placing}
  >
    <div class="flex shrink-0 items-center gap-2">
      <UI.Button onclick={openFile} disabled={busy} title="打开文档 (Ctrl+O)">
        <FolderOpen class="h-4 w-4" /> 打开 PDF
      </UI.Button>
      <UI.Button variant="secondary" onclick={doImport} disabled={busy}>
        <FileInput class="h-4 w-4" />
        {doc ? "导入图片 / PDF" : "导入图片"}
      </UI.Button>
      <UI.Button
        variant="secondary"
        onclick={beginBlank}
        disabled={busy || !doc || placing}
      >
        <FilePlus2 class="h-4 w-4" /> 插入空白页
      </UI.Button>
    </div>
    <div
      class="flex min-w-0 flex-1 items-center justify-center gap-1.5 text-sm text-muted-foreground"
    >
      {#if doc}
        <!-- 固定位置：脏点（无论显示与否都占位，避免文件名位移） -->
        <span class="grid h-4 w-4 shrink-0 place-items-center">
          {#if isDirty}
            <CircleDotDashedIcon
              class="h-3.5 w-3.5 text-amber-600"
              aria-label="有未保存的改动"
            />
          {/if}
        </span>
        <!-- 固定宽度 + 左对齐 + 截断 -->
        <span
          class="min-w-0 max-w-[40ch] flex-1 truncate text-left"
          title={removeFileExtension(doc.fileName)}
          >{removeFileExtension(doc.fileName)}</span
        >
        <!-- 固定位置：关闭 -->
        <UI.Button
          variant="ghost"
          size="icon"
          onclick={doClose}
          disabled={busy}
          title="关闭文档 (Ctrl+W)"
          aria-label="关闭文档"
        >
          <X class="h-4 w-4" />
        </UI.Button>
      {:else}
        <span class="text-sm text-muted-foreground">未打开文档</span>
      {/if}
    </div>
    <div class="flex shrink-0 items-center gap-2" class:invisible={!doc}>
      <UI.Button
        variant={isDirty ? "default" : "outline"}
        onclick={doSave}
        disabled={busy || !isDirty || !doc}
        title="保存 (Ctrl+S)"
      >
        <Save class="h-4 w-4" /> 保存
      </UI.Button>
      <UI.Button
        variant="outline"
        onclick={doSaveAs}
        disabled={busy || !doc}
        title="另存为 (Ctrl+Shift+S)"
      >
        <Download class="h-4 w-4" /> 另存为
      </UI.Button>
    </div>
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

            const total = res.length;
            const sel = parsePageSel(pages, total);
            if (sel === null) {
              notify(
                "err",
                `页码格式无效：请输入如 1,3-5 的形式，范围须在 1-${total} 内，且区间起始不大于结束`,
              );
              return;
            }

            const del = new Set(sel);
            const next = res.filter((_, i) => !del.has(i + 1));
            if (!next.length) {
              notify("err", "不能删除全部页面");
              return;
            }

            const ok = await askConfirm({
              title: "删除页面",
              body: `将删除 ${sel.length} 页（${pages}）。此操作在保存前不会写入文件，可继续编辑但无法撤销。是否继续？`,
              confirmText: "删除",
              destructive: true,
            });
            if (!ok) return;

            replacePageRes(doc.id, next);
            replacePageDims(
              doc.id,
              dims.filter((_, i) => !del.has(i + 1)),
            );
            selected = new Set();
            notify("ok", `已删除 ${sel.length} 页`);
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
          <FileQuestionMark class="h-12 w-12" />
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
        class="flex items-center gap-2 rounded-md px-3 py-1.5 text-sm transition-colors disabled:pointer-events-none disabled:opacity-50
           {tab === t.id
          ? 'bg-primary/15 font-medium text-foreground'
          : 'text-muted-foreground hover:bg-accent hover:text-foreground'}"
        onclick={() => (tab = t.id)}
        disabled={busy}
      >
        <t.icon class="h-4 w-4 {tab === t.id ? 'text-primary' : ''}" />
        {t.label}
        {#if t.id === "thumbs" && doc}
          <span
            class="rounded-full bg-primary/15 px-1.5 py-px text-[10px] font-semibold tabular-nums text-primary"
          >
            {logicalCount(doc.id, doc.pageCount)}
          </span>
        {/if}
      </button>
    {/each}
    {#if allProblems.length}
      <div class="ml-auto">
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
      </div>
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
                <div class="min-w-0 flex-1">
                  <div class="flex min-w-0 items-center gap-2">
                    <span
                      class="min-w-0 truncate font-medium"
                      title={p.title}>{p.title}</span
                    >
                    <span
                      class="inline-flex w-11 shrink-0 items-center justify-center rounded-full bg-primary/15 px-1.5 py-px text-[10px] font-semibold tracking-wide text-primary"
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
{#if confirmState.open}
  <!-- svelte-ignore a11y_interactive_supports_focus -->
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div
    class="log-overlay"
    role="alertdialog"
    aria-modal="true"
    onclick={(e) => e.target === e.currentTarget && resolveConfirm(false)}
  >
    <div class="log-card">
      <div class="border-b border-border px-4 py-3">
        <h3 class="flex items-center gap-2 text-sm font-medium">
          <TriangleAlert class="h-4 w-4 text-amber-600" />
          {confirmState.title}
        </h3>
      </div>
      <div class="px-4 py-4 text-sm text-muted-foreground">
        {confirmState.body}
      </div>
      <div class="flex justify-end gap-2 border-t border-border px-4 py-3">
        <UI.Button variant="outline" onclick={() => resolveConfirm(false)}>
          取消
        </UI.Button>
        <UI.Button
          variant={confirmState.destructive ? "destructive" : "default"}
          onclick={() => resolveConfirm(true)}
        >
          {confirmState.confirmText}
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
</style>
