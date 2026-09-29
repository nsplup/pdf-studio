<script lang="ts">
  import {
    OutlineService,
    type OutlineNode,
    type DocInfo,
  } from "../bindings/services";
  import { pickSaveAny } from "../lib/dialogs";
  import ace from "ace-builds";
  import "ace-builds/src-noconflict/mode-text";
  import "ace-builds/src-noconflict/theme-tomorrow_night";
  import "ace-builds/src-noconflict/ext-searchbox"; // Ctrl+F / Ctrl+H 查找替换
  import { currentDoc, applyDocUpdate, notify } from "../stores";
  import * as UI from "./ui";
  import TreeNode, { type EditNode } from "./TreeNode.svelte";
  import {
    Trash2,
    Loader2,
    ListTree,
    FileOutput,
    FileQuestion,
    Type,
    ScrollText,
    X,
    CheckCircle2,
    TriangleAlert,
    ArrowDownUp,
  } from "lucide-svelte";

  let { pageCount = null as number | null } = $props();
  let doc = $derived($currentDoc);
  /** 校验基准：逻辑页数（含未保存的插入/删除） */
  let maxPage = $derived(pageCount ?? doc?.pageCount ?? 1);
  let loadedPath = $state("");
  let tree = $state<EditNode[]>([]);
  let busy = $state(false);
  let mode = $state<"tree" | "text">("tree");
  let textValue = $state("");

  /**
   * outlines 单一事实来源：
   * - 树状编辑 → outlines 更新 → 文本渲染同步（此处序列化）；
   * - 文本编辑 → oninput 直接更新 outlines → 树状视图随 outlines 刷新。
   * 两个视图都只渲染 outlines，不存在独立状态与特例。
   */
  let lastMode = mode;
  let lastTreeJson = "";
  $effect(() => {
    const m = mode;
    const snapshot = JSON.stringify(tree);
    if (m !== lastMode) {
      lastMode = m;
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
    void doc;
    if (doc && doc.sourcePath !== loadedPath) {
      const path = doc.sourcePath;
      loadedPath = path;
      load(path);
    }
  });

  async function load(path: string) {
    busy = true;
    try {
      const nodes = (await OutlineService.Read(path)) ?? [];
      tree = toEdit(nodes);
    } catch {
      tree = [];
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

  /** 树 -> 缩进文本（每行：层级缩进 + 标题 + TAB + 页码） */
  function serialize(nodes: EditNode[]): string {
    const lines: string[] = [];
    const walk = (list: EditNode[], depth: number) => {
      for (const n of list) {
        lines.push("\t".repeat(depth) + `${n.title}\t${n.page}`);
        walk(n.kids ?? [], depth + 1);
      }
    };
    walk(nodes, 0);
    return lines.join("\n");
  }

  /** 缩进文本 -> 树；页码缺省/非法取 1；层级由行首 TAB 数决定 */
  function parseText(text: string): EditNode[] {
    const root: EditNode[] = [];
    const stack: { depth: number; node: EditNode }[] = [];
    for (const raw of text.split("\n")) {
      if (!raw.trim()) continue;
      const depth = raw.length - raw.replace(/^\t+/, "").length;
      const body = raw.slice(depth);
      const ti = body.lastIndexOf("\t");
      let title = body;
      let page = 1;
      if (ti >= 0) {
        title = body.slice(0, ti).trim() || "无标题";
        const n = parseInt(body.slice(ti + 1), 10);
        if (Number.isFinite(n) && n >= 1) page = n;
      } else {
        title = body.trim() || "无标题";
      }
      const node: EditNode = { title, page, expanded: true, kids: [] };
      while (stack.length && stack[stack.length - 1].depth >= depth) stack.pop();
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
  let ed: any = null;
  /** ace → 数据 更新中，抑制外部回写 */
  let aceLock = false;



  // 离开文本模式：销毁实例（容器随 {#if} 卸载）
  $effect(() => {
    if (mode !== "text" && ed) {
      ed.destroy();
      ed = null;
    }
  });

  // 进入文本模式：容器挂载后创建实例
  $effect(() => {
    if (mode !== "text" || !aceEl || ed) return;
    ed = ace.edit(aceEl, {
      value: textValue,
      mode: "ace/mode/text",
      theme: "ace/theme/tomorrow_night", // 暗色模式
      wrap: true,
      useSoftTabs: false,
      showPrintMargin: false,
      fontSize: "13px",
      useWorker: false,
      placeholder: "每行一条：缩进（TAB）表示层级，标题后跟 TAB 和页码",
    });
    ed.on("change", () => {
      if (aceLock) return;
      textValue = ed.getValue();
      tree = parseText(textValue);
    });
  });

  // 数据 → ace 回写（树状视图/导入等外部修改；内容一致或正在编辑时零扰动）
  $effect(() => {
    const next = serialize(tree);
    if (!ed || mode !== "text") return;
    if (ed.isFocused()) return; // 输入中不回写，避免归一化打断光标
    if (ed.getValue() !== next) {
      aceLock = true;
      ed.setValue(next, 1);
      aceLock = false;
    }
  });

  // 校验标注：直接使用 Ace Session 内置 annotations（装订线警告标记 + 行悬停提示）
  $effect(() => {
    const ps = problems;
    const tv = mode === "text" ? textValue : serialize(tree);
    if (!ed || mode !== "text") return;
    const validLines: number[] = [];
    tv.split("\n").forEach((l: string, i: number) => {
      if (/\t\d+\s*$/.test(l)) validLines.push(i);
    });
    ed.session.setAnnotations(
      ps
        .filter((p) => p.order >= 0 && p.order < validLines.length)
        .map((p) => ({ row: validLines[p.order], column: 0, text: p.detail, type: "warning" as const }))
    );
  });

  // ---------- 书签校验（pdfcpu 不接受逆序/越界书签，前端先行校验） ----------

  export interface BookmarkProblem {
    kind: "range" | "order";
    title: string;
    page: number;
    detail: string;
    /** 写入序列序号（0 基），文本模式下对应第 order 条带页码的行 */
    order: number;
  }

  /**
   * 深度优先遍历出写入序列后校验：
   * - 越界：页码不在 [1, maxPage] → 自身不通过；
   * - 逆序：本项页码大于后一项页码 → 前一项不通过（pdfcpu 按写入顺序校验）。
   */
  function computeProblems(nodes: EditNode[], max: number): { problems: BookmarkProblem[]; invalid: Set<EditNode> } {
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
    const suffixMin: number[] = new Array(seq.length + 1).fill(Number.POSITIVE_INFINITY);
    for (let i = seq.length - 1; i >= 0; i--) {
      const p = Math.round(Number(seq[i].page));
      const valid = Number.isFinite(p) && p >= 1 && p <= max ? p : Number.POSITIVE_INFINITY;
      suffixMin[i] = Math.min(valid, suffixMin[i + 1]);
    }
    for (let i = 0; i < seq.length; i++) {
      const n = seq[i];
      const p = Math.round(Number(n.page));
      if (!Number.isFinite(p) || p < 1 || p > max) {
        invalid.add(n);
        problems.push({ kind: "range", title: n.title || "(无标题)", page: p, detail: `页码 ${Number.isFinite(p) ? p : "无效"} 超出范围 [1, ${max}]`, order: i });
        continue;
      }
      if (p > suffixMin[i + 1]) {
        invalid.add(n);
        problems.push({ kind: "order", title: n.title || "(无标题)", page: p, detail: `页码 ${p} 大于后续书签页码 ${suffixMin[i + 1]}（逆序）`, order: i });
      }
    }
    return { problems, invalid };
  }

  let validation = $derived(computeProblems(tree, maxPage));
  let problems = $derived(validation.problems);
  let invalidSet = $derived(validation.invalid);

  let showLog = $state(false);


  /** 供 App 保存前调用：返回问题列表（空 = 通过） */
  export function validate(): BookmarkProblem[] {
    if (loadedPath !== doc?.sourcePath) return [];
    return problems;
  }

  // ---------- 供 App 调用的接口 ----------

  /** 当前书签树（未加载过返回 undefined = 保存时不修改书签） */
  export function getTree(): OutlineNode[] | undefined {
    if (loadedPath !== doc?.sourcePath) return undefined;
    return toOutline(tree);
  }

  /** 导入放置后追加合并的书签（页码已由后端平移） */
  export function addNodes(nodes: OutlineNode[]) {
    tree = [...tree, ...toEdit(nodes)];
    if (mode === "text") textValue = serialize(tree);
  }

  /** 清空（待全局保存时生效）；应用内 confirm 弹窗二次确认 */
  let showClearConfirm = $state(false);
  function clearTree() {
    if (!tree.length) return;
    showClearConfirm = true;
  }
  function doClearTree() {
    showClearConfirm = false;
    tree = [];
    if (mode === "text") textValue = "";
  }

  /** 根级添加书签 */
  function addRoot() {
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
    removeNode(tree, path);
    tree = [...tree];
  }

  function onChanged() {
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
    list.splice(idx + offset, 0, { title: "新书签", page: refPage, expanded: true, kids: [] });
    tree = [...tree];
  }

  /** 导出书签为缩进文本 */
  async function exportText() {
    if (!doc) return;
    const out = await pickSaveAny(
      doc.fileName.replace(/\.pdf$/i, "") + "-书签.txt",
      "导出书签文本"
    );
    if (!out) return;
    busy = true;
    try {
      await OutlineService.ExportText(doc.sourcePath, out);
      notify("ok", `已导出：${out.split(/[\\/]/).pop()}`);
    } catch (e: any) {
      notify("err", `导出失败：${e?.message ?? e}`);
    } finally {
      busy = false;
    }
  }
</script>

{#if doc}
  <div class="h-full px-4 py-4">
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
        <p class="mt-1 text-xs text-muted-foreground">
          更改将在顶部「保存 / 另存为」时写入 PDF
        </p>
      </UI.CardHeader>
      <UI.CardContent>
        {#if mode === "text"}
          <div
            bind:this={aceEl}
            class="ace-shell h-[calc(100vh-300px)] w-full overflow-hidden rounded-md border border-input"
          ></div>
          <p class="mt-2 text-xs text-muted-foreground">
            每行：标题 + TAB + 页码；行首 TAB 数表示层级。文本与树状视图完全同步于同一份书签数据（outlines）：任一侧修改立即更新数据并反映到另一侧。
          </p>
        {:else if busy && !tree.length}
          <div class="empty"><Loader2 class="h-5 w-5 animate-spin" /> 读取书签…</div>
        {:else}
          <div class="tree-wrap">
            {#each tree as node, i (i)}
              <TreeNode {node} path={[i]} {onChanged} {onRemove} {invalidSet}
                onInsertAbove={(p) => insertSibling(p, 0)}
                onInsertBelow={(p) => insertSibling(p, 1)} />
            {/each}
            {#if !tree.length}
              <div class="empty-sm">暂无书签，可在此处添加或导入后自动合并</div>
            {/if}
          </div>
        {/if}
      </UI.CardContent>
      <UI.CardFooter class="justify-between gap-3">
        <UI.Button
          variant="outline"
          class="text-destructive hover:text-destructive"
          onclick={clearTree}
          disabled={busy || !tree.length}
        >
          <Trash2 class="h-4 w-4" /> 清空书签（待保存）
        </UI.Button>
        <div class="flex items-center gap-2">
          <UI.Button
            variant={problems.length ? "secondary" : "outline"}
            onclick={() => (showLog = true)}
            disabled={busy}
          >
            <ScrollText class="h-4 w-4" />
            日志
            {#if problems.length}
              <span class="ml-0.5 rounded-full bg-amber-500/20 px-1.5 text-xs font-medium text-amber-600">
                {problems.length}
              </span>
            {/if}
          </UI.Button>
          <UI.Button variant="outline" onclick={exportText} disabled={busy}>
            <FileOutput class="h-4 w-4" /> 导出书签文本
          </UI.Button>
        </div>
      </UI.CardFooter>
    </UI.Card>
  </div>

  {#if showLog}
    <div class="log-overlay" role="dialog" aria-modal="true" onclick={(e) => e.target === e.currentTarget && (showLog = false)}>
      <div class="log-card">
        <div class="flex items-center justify-between border-b border-border px-4 py-3">
          <h3 class="flex items-center gap-2 text-sm font-medium">
            <ScrollText class="h-4 w-4" /> 书签校验日志
          </h3>
          <button class="pv-btn" onclick={() => (showLog = false)}><X class="h-4 w-4" /></button>
        </div>
        <div class="max-h-[50vh] overflow-y-auto px-4 py-3">
          {#if problems.length === 0}
            <div class="flex items-center gap-2 py-6 text-sm text-muted-foreground">
              <CheckCircle2 class="h-4 w-4 text-emerald-500" /> 未检测到问题，书签可直接保存。
            </div>
          {:else}
            <ul class="flex flex-col gap-2">
              {#each problems as p, i}
                <li class="flex items-start gap-2 px-3 py-2 text-sm">
                  {#if p.kind === "order"}
                    <ArrowDownUp class="mt-0.5 h-4 w-4 shrink-0 text-amber-600" />
                  {:else}
                    <TriangleAlert class="mt-0.5 h-4 w-4 shrink-0 text-amber-600" />
                  {/if}
                  <div>
                    <div class="font-medium">{p.title}</div>
                    <div class="text-xs text-muted-foreground">{p.detail}</div>
                  </div>
                </li>
              {/each}
            </ul>
            <p class="mt-3 text-xs text-muted-foreground">
              书签页码必须落在 1–{maxPage} 且在写入顺序上不递减（pdfcpu 限制）。请修正后保存。
            </p>
          {/if}
        </div>
        <div class="flex justify-end border-t border-border px-4 py-3">
          <UI.Button variant="outline" onclick={() => (showLog = false)}>关闭</UI.Button>
        </div>
      </div>
    </div>
  {/if}

  {#if showClearConfirm}
    <div class="log-overlay" role="alertdialog" aria-modal="true">
      <div class="log-card">
        <div class="border-b border-border px-4 py-3">
          <h3 class="flex items-center gap-2 text-sm font-medium">
            <TriangleAlert class="h-4 w-4 text-amber-600" /> 清空书签
          </h3>
        </div>
        <div class="px-4 py-4 text-sm text-muted-foreground">
          确定清空全部书签？（保存时不写入书签，其他修改不受影响）
        </div>
        <div class="flex justify-end gap-2 border-t border-border px-4 py-3">
          <UI.Button variant="outline" onclick={() => (showClearConfirm = false)}>取消</UI.Button>
          <UI.Button variant="destructive" onclick={doClearTree}>确认清空</UI.Button>
        </div>
      </div>
    </div>
  {/if}
{:else}
  <div class="empty"><FileQuestion class="h-10 w-10" /> 打开 PDF 后编辑书签</div>
{/if}

<style>
  .empty {
    display: flex; flex-direction: column; align-items: center; justify-content: center;
    gap: 10px; padding: 80px 0; color: hsl(var(--muted-foreground)); font-size: 14px;
  }
  .empty-sm {
    padding: 24px 0; text-align: center; font-size: 13px; color: hsl(var(--muted-foreground));
  }
  .tree-wrap {
    display: flex; flex-direction: column; gap: 2px;
  }
  /* Ace 编辑器容器：暗色主题 + 等宽字体 + 圆角裁切 */
  .ace-shell :global(.ace_editor) {
    font-family: var(--font-mono, ui-monospace, SFMono-Regular, Menlo, monospace) !important;
    border-radius: 0 0 5px 5px;
  }

  /* 滚动条：与外部滚动条（app.css ::-webkit-scrollbar）完全一致 */
  .ace-shell :global(.ace_scrollbar-v::-webkit-scrollbar),
  .ace-shell :global(.ace_scrollbar-h::-webkit-scrollbar) {
    width: 9px;
    height: 9px;
  }
  .ace-shell :global(.ace_scrollbar-v::-webkit-scrollbar-track),
  .ace-shell :global(.ace_scrollbar-h::-webkit-scrollbar-track) {
    background: transparent;
  }
  .ace-shell :global(.ace_scrollbar-v::-webkit-scrollbar-thumb),
  .ace-shell :global(.ace_scrollbar-h::-webkit-scrollbar-thumb) {
    background: hsl(var(--muted));
    border-radius: 6px;
    border: 2px solid hsl(var(--background));
  }
  .ace-shell :global(.ace_scrollbar-v::-webkit-scrollbar-thumb:hover),
  .ace-shell :global(.ace_scrollbar-h::-webkit-scrollbar-thumb:hover) {
    background: hsl(var(--secondary-foreground) / 0.3);
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
  vertical-align: -1px !important;   /* 微调基线对齐；偏上就改 -3px，偏下就改 -1px */
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
    position: fixed; inset: 0; z-index: 60;
    display: flex; align-items: center; justify-content: center;
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
    display: inline-flex; align-items: center; justify-content: center;
    width: 28px; height: 28px; border-radius: 6px;
    color: hsl(var(--muted-foreground));
  }
  .pv-btn:hover { background: hsl(var(--accent)); color: hsl(var(--foreground)); }
</style>
