<script lang="ts">
  import { onDestroy } from "svelte";
  import { OutlineService, type OutlineNode, type DocInfo } from "@bindings";
  import ace from "ace-builds";
  import "ace-builds/src-noconflict/mode-text";
  import "ace-builds/src-noconflict/theme-tomorrow_night";
  import "ace-builds/src-noconflict/ext-searchbox"; // Ctrl+F / Ctrl+H 查找替换
  import {
    currentDoc,
    notify,
    labelBase,
    toViewPage,
    fromViewPage,
    outlineProblems,
    outlineDirty,
    type AppProblem,
    type BookmarkProblem,
  } from "../stores";
  import * as UI from "./ui";
  import TreeNode, { type EditNode } from "./TreeNode.svelte";
  import {
    Trash2,
    Loader,
    ListTree,
    Plus,
    FileQuestionMark,
    Type,
    TriangleAlert, // 清空确认弹窗还在用
  } from "lucide-svelte";
  // 移除 ScrollText / X / CheckCircle2 / ArrowDownUp

  let { pageCount = null as number | null } = $props();
  let doc = $derived($currentDoc);
  /** 校验基准：逻辑页数（含未保存的插入/删除） */
  let maxPage = $derived(pageCount ?? doc?.pageCount ?? 1);
  /** 页标签基准页偏移（纯前端派生）：显示页码 = 实际页码 - off；编辑回写时加回 off */
  let base = $derived($labelBase);
  let off = $derived(base.offset);
  let loadedID = $state(""); // 原 loadedPath
  let attemptedID = $state(""); // 原 attemptedPath
  let tree = $state<EditNode[]>([]);
  let busy = $state(false);
  let mode = $state<"tree" | "text">("tree");
  let textValue = $state("");
  let touched = $state(false);

  $effect(() => {
    outlineDirty.set(touched);
  });
  /**
   * outlines 单一事实来源：
   * - 树状编辑 → outlines 更新 → 文本渲染同步（此处序列化）；
   * - 文本编辑 → oninput 直接更新 outlines → 树状视图随 outlines 刷新。
   * 两个视图都只渲染 outlines，不存在独立状态与特例。
   */
  // svelte-ignore state_referenced_locally
  let lastMode = mode;
  let lastTreeJson = "";
  let lastSyncOff = -1;
  $effect(() => {
    const m = mode;
    const o = off;
    const snapshot = JSON.stringify(tree);
    if (m !== lastMode || o !== lastSyncOff) {
      lastMode = m;
      lastSyncOff = o;
      lastTreeJson = snapshot;
      textValue = serialize(tree);
      return;
    }
    if (m === "text" && snapshot !== lastTreeJson) {
      lastTreeJson = snapshot;
      textValue = serialize(tree);
    }
  });

  $effect(() => {
    const d = doc;
    if (!d) return;
    if (d.id === attemptedID) return;
    attemptedID = d.id;
    void load(d);
  });

  async function load(d: DocInfo) {
    busy = true;
    touched = false;
    try {
      if (!d.sourcePath) {
        // 尚未保存过：无源文件可读，从空开始（导入的书签随后由 addNodes 追加）
        tree = [];
        loadedID = d.id;
        return;
      }
      const nodes = (await OutlineService.Read(d.sourcePath)) ?? [];
      tree = toEdit(nodes);
      loadedID = d.id;
    } catch (e: any) {
      tree = [];
      loadedID = "";
      notify("err", `读取书签失败：${e?.message ?? e}`);
    } finally {
      busy = false;
    }
  }

  function toEdit(nodes: OutlineNode[]): EditNode[] {
    return nodes.map((n) => ({
      title: n.title,
      page: n.page,
      expanded: true,
      kids: n.kids ? toEdit(n.kids) : [],
    }));
  }

  /** 树 -> 缩进文本（每行：层级缩进 + 标题 + TAB + 页码）；页码按基准页偏移显示 */
  function serialize(nodes: EditNode[]): string {
    const lines: string[] = [];
    const walk = (list: EditNode[], depth: number) => {
      for (const n of list) {
        lines.push(
          "\t".repeat(depth) + `${n.title}\t${toViewPage(n.page, off)}`,
        );
        walk(n.kids ?? [], depth + 1);
      }
    };
    walk(nodes, 0);
    return lines.join("\n");
  }

  /** 缩进文本 -> 树；输入为视图页码（无零页：0 不存在），按规则换算回实际页码存储；缺省/非法取 1；层级由行首 TAB 数决定 */
  function parseText(text: string): EditNode[] {
    const root: EditNode[] = [];
    const stack: { depth: number; node: EditNode }[] = [];
    for (const raw of text.split("\n")) {
      if (!raw.trim()) continue;
      const depth = raw.length - raw.replace(/^\t+/, "").length;
      const body = raw.slice(depth);
      const ti = body.lastIndexOf("\t");
      let title = body;
      let page = 1; // 视图页码，缺省为基准区第 1 页
      if (ti >= 0) {
        title = body.slice(0, ti).trim() || "无标题";
        const n = parseInt(body.slice(ti + 1), 10);
        // 视图页码无 0 页；换算后须为合法实际页码（≥ 1）
        const phys = fromViewPage(n, off);
        if (phys !== null && phys >= 1) page = n;
      } else {
        title = body.trim() || "无标题";
      }
      const node: EditNode = {
        title,
        page: fromViewPage(page, off) ?? 1,
        expanded: true,
        kids: [],
      };
      while (stack.length && stack[stack.length - 1].depth >= depth)
        stack.pop();
      if (stack.length) stack[stack.length - 1].node.kids.push(node);
      else root.push(node);
      stack.push({ depth, node });
    }
    return root;
  }

  function toOutline(nodes: EditNode[]): OutlineNode[] {
    const walk = (list: EditNode[]): OutlineNode[] =>
      list.map((n) => ({
        title: n.title,
        page: Math.max(1, n.page),
        kids: n.kids?.length ? walk(n.kids) : undefined,
      }));
    return walk(nodes);
  }

  // ---------- Ace Editor（文本模式） ----------
  let aceEl: HTMLDivElement | undefined = $state();
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  let ed = $state<any>(null);
  /** ace → 数据 更新中，抑制外部回写 */
  let aceLock = false;
  let ro: ResizeObserver | null = null;

  onDestroy(() => {
    ro?.disconnect();
    ro = null;
    ed?.destroy();
    ed = null;
  });

  // 进入文本模式：容器挂载后创建实例
  $effect(() => {
    if (mode !== "text") return; // 没进过文本模式就不建
    if (!aceEl || ed) return; // 建过就不重建
    ed = ace.edit(aceEl, {
      value: textValue,
      mode: "ace/mode/text",
      theme: "ace/theme/tomorrow_night", // 暗色模式
      wrap: true,
      useSoftTabs: false,
      showPrintMargin: false,
      fontSize: "13px",
      useWorker: false,
    });
    ro = new ResizeObserver(() => {
      if (!ed || !aceEl) return;
      // 容器真正可见（尺寸 > 0）才处理；display:none → 尺寸 0 → 直接跳过
      if (aceEl.clientHeight === 0 || aceEl.clientWidth === 0) return;

      // 把视图同步到当前数据（数据在隐藏期间可能已变过多次）
      const next = serialize(tree);
      if (ed.getValue() !== next) {
        aceLock = true;
        ed.setValue(next, 1);
        aceLock = false;
      }
      // 尺寸从 0 变非 0，Ace 的字符度量缓存是旧的，必须强制重排 + 整屏重绘
      ed.resize(true);
      ed.renderer.updateFull(true);
    });
    ro.observe(aceEl);
    ed.on("change", () => {
      if (aceLock) return;
      textValue = ed.getValue();
      tree = parseText(textValue);
      touched = true;
    });
  });

  // 数据 → ace 回写（树状视图/导入/基准页切换等外部修改；内容一致或正在输入时零扰动）
  let lastWriteOff = -1;
  $effect(() => {
    const o = off;
    const offChanged = o !== lastWriteOff;
    lastWriteOff = o;
    if (!ed) return; // ← 只判断 ed，不判断 mode
    const next = serialize(tree);

    const invisible =
      !aceEl || aceEl.clientHeight === 0 || aceEl.clientWidth === 0;
    if (invisible) {
      if (ed.getValue() !== next) {
        aceLock = true;
        ed.setValue(next, 1);
        aceLock = false;
      }
      return;
    }

    if (!offChanged && ed.isFocused()) return;
    if (ed.getValue() !== next) {
      aceLock = true;
      ed.setValue(next, 1);
      aceLock = false;
    }
    if (offChanged) {
      ed.clearSelection();
      ed.resize(true);
      ed.renderer.updateFull(true);
    }
  });

  // 校验标注：直接使用 Ace Session 内置 annotations（装订线警告标记 + 行悬停提示）
  $effect(() => {
    const ps = problems;
    void tree; // 显式依赖，保证 tree 变了会重算
    if (!ed || mode !== "text") return;

    // 以 Ace 实际内容为准，避免与 textValue 的批处理窗口错位
    const tv: string = ed.getValue();
    const nodeRows: number[] = [];
    tv.split("\n").forEach((l, i) => {
      if (l.trim()) nodeRows.push(i); // ← 与 parseText 的 "哪些行算节点" 完全一致
    });

    ed.session.setAnnotations(
      ps
        .filter((p) => p.order >= 0 && p.order < nodeRows.length)
        .map((p) => ({
          row: nodeRows[p.order],
          column: 0,
          text: p.detail,
          type: "warning" as const,
        })),
    );
  });

  /**
   * 深度优先遍历出写入序列后校验：
   * - 越界：页码不在 [1, maxPage] → 自身不通过；
   * - 逆序：本项页码大于后一项页码 → 前一项不通过（pdfcpu 按写入顺序校验）。
   */
  function computeProblems(
    nodes: EditNode[],
    max: number,
  ): { problems: BookmarkProblem[]; invalid: Set<EditNode> } {
    const problems: BookmarkProblem[] = [];
    const invalid = new Set<EditNode>();
    const seq: EditNode[] = [];
    const walk = (ns: EditNode[]) => {
      for (const n of ns) {
        seq.push(n);
        walk(n.kids ?? []);
      }
    };
    walk(nodes);
    // suffixMin[i] = seq[i..] 中合法页码的最小值（「大于后续任意一项」判定）
    const suffixMin: number[] = new Array(seq.length + 1).fill(
      Number.POSITIVE_INFINITY,
    );
    for (let i = seq.length - 1; i >= 0; i--) {
      const p = Math.round(Number(seq[i].page));
      const valid =
        Number.isFinite(p) && p >= 1 && p <= max ? p : Number.POSITIVE_INFINITY;
      suffixMin[i] = Math.min(valid, suffixMin[i + 1]);
    }
    for (let i = 0; i < seq.length; i++) {
      const n = seq[i];
      const p = Math.round(Number(n.page));
      // 校验始终基于实际页码；带基准偏移时在提示中附视图页码，避免对照困惑
      const hint =
        off > 0 && Number.isFinite(p)
          ? `（视图显示 ${toViewPage(p, off)}）`
          : "";
      if (!Number.isFinite(p) || p < 1 || p > max) {
        invalid.add(n);
        problems.push({
          kind: "range",
          title: n.title || "(无标题)",
          page: p,
          detail: `页码 ${Number.isFinite(p) ? p : "无效"}${hint} 超出范围 [1, ${max}]`,
          order: i,
        });
        continue;
      }
      if (p > suffixMin[i + 1]) {
        invalid.add(n);
        problems.push({
          kind: "order",
          title: n.title || "(无标题)",
          page: p,
          detail: `页码 ${p}${hint} 大于后续书签页码 ${suffixMin[i + 1]}`,
          order: i,
        });
      }
    }
    return { problems, invalid };
  }

  let validation = $derived(computeProblems(tree, maxPage));
  let problems = $derived(validation.problems);
  let invalidSet = $derived(validation.invalid);

  // 把书签校验问题注册到全局，供 App 汇总展示
  $effect(() => {
    if (loadedID !== doc?.id) {
      outlineProblems.set([]);
      return;
    }
    outlineProblems.set(
      problems.map(
        (p): AppProblem => ({
          source: "outline",
          kind: p.kind,
          title: p.title,
          detail: p.detail,
          tab: "outline",
        }),
      ),
    );
  });

  // ---------- 供 App 调用的接口 ----------

  export function getTree(): OutlineNode[] | undefined {
    if (loadedID !== doc?.id) return undefined;
    return toOutline(tree);
  }

  export function addNodes(nodes: OutlineNode[]) {
    if (!nodes?.length) return;
    if (loadedID !== doc?.id) {
      notify("err", "书签未成功加载，导入内容中的书签未合并");
      return;
    }
    tree = [...tree, ...toEdit(nodes)];
    if (mode === "text") textValue = serialize(tree);
    touched = true;
  }

  export function retryOutline() {
    attemptedID = "";
  }

  /** 清空（待全局保存时生效）；应用内 confirm 弹窗二次确认 */
  let showClearConfirm = $state(false);
  function clearTree() {
    if (!tree.length) return;
    showClearConfirm = true;
  }
  function doClearTree() {
    touched = true;
    showClearConfirm = false;
    tree = [];
    if (mode === "text") textValue = "";
  }

  /** 根级添加书签 */
  function addRoot() {
    touched = true;
    tree = [...tree, { title: "新书签", page: 1, expanded: true, kids: [] }];
  }

  /** 按路径删除节点（TreeNode 回调） */
  function removeNode(list: EditNode[], path: number[]) {
    if (!path.length) return;
    const i = path[0];
    if (path.length === 1) {
      list.splice(i, 1);
      return;
    }
    const child = list[i];
    if (child) removeNode(child.kids, path.slice(1));
  }

  function onRemove(path: number[]) {
    touched = true;
    removeNode(tree, path);
    tree = [...tree];
  }

  function onChanged() {
    touched = true;

    tree = [...tree]; // 触发重渲染（深层变更已在代理上生效）
  }

  /** 在 path 指定节点的上方/下方插入同代节点 */
  function insertSibling(path: number[], offset: number) {
    if (!path.length) return;
    let list = tree;
    for (let d = 0; d < path.length - 1; d++) {
      const next = list[path[d]]?.kids;
      if (!next) return;
      list = next;
    }
    const idx = path[path.length - 1];
    const refPage = list[idx]?.page ?? 1;
    list.splice(idx + offset, 0, {
      title: "新书签",
      page: refPage,
      expanded: true,
      kids: [],
    });
    touched = true;
    tree = [...tree];
  }

  /** 是否有未保存的书签改动 */
  export function isDirty(): boolean {
    return touched;
  }

  /** 保存成功后由 App 调用，清除 dirty 标记 */
  export function markClean() {
    touched = false;
  }
  /** 关闭文档时清空内部状态 */
  export function reset() {
    ro?.disconnect();
    ro = null;
    ed?.destroy();
    ed = null;

    loadedID = "";
    attemptedID = "";
    tree = [];
    textValue = "";
    touched = false;
    busy = false;
    // 其他状态按需一起清（lastMode / lastTreeJson / lastSyncOff / lastWriteOff 等）
    lastMode = mode;
    lastTreeJson = "";
    lastSyncOff = -1;
    lastWriteOff = -1;
  }
</script>

{#if doc}
  <div class="px-4 py-4">
    <UI.Card>
      <UI.CardHeader class="flex flex-col">
        <UI.CardTitle class="flex w-full items-center justify-between gap-2">
          <span class="flex items-center gap-2">
            <ListTree class="h-4 w-4 text-primary" /> 书签
          </span>
          <UI.Tabs bind:value={mode} class="ml-auto">
            <UI.TabsList>
              <UI.TabsTrigger value="tree" title="层级编辑">
                <ListTree class="h-3.5 w-3.5" /> 树状
              </UI.TabsTrigger>
              <UI.TabsTrigger value="text" title="缩进文本编辑">
                <Type class="h-3.5 w-3.5" /> 文本
              </UI.TabsTrigger>
            </UI.TabsList>
          </UI.Tabs>
        </UI.CardTitle>
      </UI.CardHeader>
      <UI.CardContent>
        {#if busy && !tree.length}
          <div class="empty">
            <Loader class="h-5 w-5 animate-spin" /> 正在读取书签
          </div>
        {:else if tree.length === 0 && loadedID !== doc?.id}
          <div class="empty-sm">
            读取书签失败，保存时将不修改书签。
            <button class="underline" onclick={retryOutline}>重试</button>
          </div>
        {:else}
          <div style:display={mode === "text" ? "block" : "none"}>
            <div
              bind:this={aceEl}
              class="ace-shell h-[calc(100vh-314px)] w-full overflow-hidden rounded-md border border-input"
            ></div>
            <p class="mt-2 text-xs text-muted-foreground">
              每行一个节点，标题与页码使用制表符分隔；行首使用制表符缩进表示子节点
            </p>
          </div>

          {#if mode === "tree"}
            <div class="tree-wrap h-[calc(100vh-290px)] overflow-y-auto">
              {#each tree as node, i (node)}
                <TreeNode
                  bind:node={tree[i]}
                  path={[i]}
                  {onChanged}
                  {onRemove}
                  {invalidSet}
                  pageOffset={off}
                  onInsertAbove={(p) => insertSibling(p, 0)}
                  onInsertBelow={(p) => insertSibling(p, 1)}
                />
              {/each}
              {#if tree.length === 0}
                <div class="empty-sm">
                  暂无书签，可在此处添加或导入后自动合并
                </div>
              {/if}
            </div>
          {/if}
        {/if}
      </UI.CardContent>
      <UI.CardFooter class="justify-between gap-3">
        <UI.Button
          variant="outline"
          class="text-destructive hover:text-destructive"
          onclick={clearTree}
          disabled={busy || !tree.length}
        >
          <Trash2 class="h-4 w-4" /> 清空书签
        </UI.Button>

        {#if mode === "tree"}
          <UI.Button
            variant="outline"
            onclick={addRoot}
            disabled={busy}
            title="在末尾添加一个根级书签"
          >
            <Plus class="h-4 w-4" /> 添加根书签
          </UI.Button>
        {/if}
      </UI.CardFooter>
    </UI.Card>
  </div>

  {#if showClearConfirm}
    <div class="log-overlay" role="alertdialog" aria-modal="true">
      <div class="log-card">
        <div class="border-b border-border px-4 py-3">
          <h3 class="flex items-center gap-2 text-sm font-medium">
            <TriangleAlert class="h-4 w-4 text-amber-600" /> 清空书签
          </h3>
        </div>
        <div class="px-4 py-4 text-sm text-muted-foreground">
          确定清空全部书签？
        </div>
        <div class="flex justify-end gap-2 border-t border-border px-4 py-3">
          <UI.Button
            variant="outline"
            onclick={() => (showClearConfirm = false)}>取消</UI.Button
          >
          <UI.Button variant="destructive" onclick={doClearTree}
            >确认清空</UI.Button
          >
        </div>
      </div>
    </div>
  {/if}
{:else}
  <div
    class="flex h-full flex-col items-center justify-center gap-3 text-muted-foreground"
  >
    <FileQuestionMark class="h-12 w-12" />
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
    padding: 24px 0;
    text-align: center;
    font-size: 13px;
    color: hsl(var(--muted-foreground));
  }
  .tree-wrap {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  /* Ace 编辑器容器：暗色主题 + 等宽字体 + 圆角裁切 */
  .ace-shell :global(.ace_editor) {
    font-family: var(
      --font-mono,
      ui-monospace,
      SFMono-Regular,
      Menlo,
      monospace
    ) !important;
    border-radius: 0 0 5px 5px;
  }

  /* 滚动条：与外部滚动条（app.css ::-webkit-scrollbar）保持一致的观感 */
  .ace-shell :global(.ace_scrollbar-v::-webkit-scrollbar),
  .ace-shell :global(.ace_scrollbar-h::-webkit-scrollbar) {
    width: 5px;
    height: 5px;
  }

  .ace-shell :global(.ace_scrollbar-v::-webkit-scrollbar-track),
  .ace-shell :global(.ace_scrollbar-h::-webkit-scrollbar-track) {
    background: transparent;
    border: none;
  }

  /* 滑块：去掉描边，避免 hover 时“内部高亮、外圈留底”的割裂感 */
  .ace-shell :global(.ace_scrollbar-v::-webkit-scrollbar-thumb),
  .ace-shell :global(.ace_scrollbar-h::-webkit-scrollbar-thumb) {
    background: rgba(150, 152, 150, 0.28); /* #969896，tomorrow_night 注释灰 */
    border: none;
    border-radius: 5px;
    background-clip: padding-box;
  }

  .ace-shell :global(.ace_scrollbar-v::-webkit-scrollbar-thumb:hover),
  .ace-shell :global(.ace_scrollbar-h::-webkit-scrollbar-thumb:hover) {
    background: rgba(150, 152, 150, 0.48); /* 整块变亮，而不是局部 */
  }

  .ace-shell :global(.ace_scrollbar-v::-webkit-scrollbar-thumb:active),
  .ace-shell :global(.ace_scrollbar-h::-webkit-scrollbar-thumb:active) {
    background: rgba(150, 152, 150, 0.62);
  }

  /* 去掉 WebKit 默认的角落补丁背景，避免右下出现浅色方块 */
  .ace-shell :global(.ace_scrollbar-v::-webkit-scrollbar-corner),
  .ace-shell :global(.ace_scrollbar-h::-webkit-scrollbar-corner) {
    background: transparent;
  }

  /* 警告图标：替换为高清 SVG（lucide triangle-alert） */
  .ace-shell :global(.ace_gutter-cell.ace_warning),
  .ace-shell :global(.ace_icon.ace_warning),
  .ace-shell :global(.ace_icon.ace_warning_fold) {
    background-image: url("data:image/svg+xml;base64,PHN2ZyB4bWxucz0naHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmcnIHZpZXdCb3g9JzAgMCAyNCAyNCcgZmlsbD0nbm9uZScgc3Ryb2tlPScjZjU5ZTBiJyBzdHJva2Utd2lkdGg9JzInIHN0cm9rZS1saW5lY2FwPSdyb3VuZCcgc3Ryb2tlLWxpbmVqb2luPSdyb3VuZCc+PHBhdGggZD0nbTIxLjczIDE4LTgtMTRhMiAyIDAgMCAwLTMuNDggMGwtOCAxNEEyIDIgMCAwIDAgNCAyMWgxNmEyIDIgMCAwIDAgMS43My0zJy8+PHBhdGggZD0nTTEyIDl2NCcvPjxwYXRoIGQ9J00xMiAxN2guMDEnLz48L3N2Zz4=") !important;
    background-size: 13px 13px;
    background-position: 2px center;
    background-repeat: no-repeat;
  }
  /* tooltip 内部的警告图标：脱离装订线的 2px 偏移，改为居中并垂直对齐文字 */
  :global(.ace_tooltip .ace_warning.ace_icon),
  :global(.ace_tooltip .ace_icon.ace_warning),
  :global(.ace_tooltip .ace_icon.ace_warning_fold) {
    display: inline-block !important;
    width: 14px !important;
    height: 14px !important;
    background-size: 14px 14px !important;
    background-position: center center !important;
    background-repeat: no-repeat !important;
    vertical-align: -1px !important; /* 微调基线对齐；偏上就改 -3px，偏下就改 -1px */
    margin: 0 4px 0 0 !important;
  }

  /* ========== 查找/替换面板：暗色底 + 只改按钮颜色 ========== */

  /* 面板容器 */
  .ace-shell :global(.ace_search) {
    background: #1d1f21 !important;
    border: 1px solid #3a3d3e !important;
    border-top: none !important;
    color: #c5c8c6 !important;
  }

  /* 输入框（查找 / 替换） */
  .ace-shell :global(.ace_search_field),
  .ace-shell :global(.ace_replace_field) {
    background: #14161a !important;
    border: 1px solid #3a3d3e !important;
    color: #c5c8c6 !important;
  }
  .ace-shell :global(.ace_search_field:focus),
  .ace-shell :global(.ace_replace_field:focus) {
    border-color: #81a2be !important;
  }
  .ace-shell :global(.ace_search_field::placeholder),
  .ace-shell :global(.ace_replace_field::placeholder) {
    color: #6b6f73 !important;
  }

  /* 箭头 < >、All / Replace、底部 - .* Aa \b S：只改颜色，其余不动 */
  .ace-shell :global(.ace_searchbtn),
  .ace-shell :global(.ace_replacebtn),
  .ace-shell :global(.ace_button) {
    background-color: #2d2f31 !important;
    border-color: #3a3d3e !important;
    color: #c5c8c6 !important;
  }
  /* 悬停：仅对未选中的按钮生效 */
  .ace-shell :global(.ace_searchbtn:hover),
  .ace-shell :global(.ace_replacebtn:hover),
  .ace-shell :global(.ace_button:hover:not(.checked)) {
    background-color: #3a3d3e !important;
    color: #e8e8e8 !important;
  }

  /* 选中态：用 checked */
  .ace-shell :global(.ace_button.checked),
  .ace-shell :global(.ace_search .ace_button.checked) {
    background-color: #81a2be !important;
    color: #1d1f21 !important;
    border-color: #81a2be !important;
  }

  /* 计数文字 */
  .ace-shell :global(.ace_search_counter) {
    color: #969896 !important;
  }

  /* 关闭按钮：唯一保留“换图标”的地方（位图 → 高清 SVG） */
  .ace-shell :global(.ace_searchbtn_close) {
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23c5c8c6' stroke-width='2.5' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='M18 6 6 18'/%3E%3Cpath d='m6 6 12 12'/%3E%3C/svg%3E") !important;
    background-position: center !important;
    background-size: 12px 12px !important;
    background-repeat: no-repeat !important;
  }
  .ace-shell :global(.ace_search_form.ace_nomatch) {
    border-radius: 3px !important;
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
