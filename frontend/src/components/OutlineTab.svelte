<script lang="ts">
  import { onDestroy } from "svelte";
  import { OutlineService, type OutlineNode, type DocInfo } from "@bindings";
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
  import CodeEditor, { type EditorAnnotation } from "./CodeEditor.svelte";
  import {
    Trash2,
    Loader,
    ListTree,
    Plus,
    FileQuestionMark,
    Type,
    TriangleAlert,
  } from "lucide-svelte";

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

  /** 文本编辑器实例 */
  let codeEditor = $state<ReturnType<typeof CodeEditor> | undefined>();

  $effect(() => {
    outlineDirty.set(touched);
  });

  /**
   * outlines 单一事实来源：
   * - 树状编辑 → outlines 更新 → 文本渲染同步（此处序列化）；
   * - 文本编辑 → onchange 直接更新 outlines → 树状视图随 outlines 刷新。
   * 两个视图都只渲染 outlines，不存在独立状态与特例。
   */
  // svelte-ignore state_referenced_locally
  let lastMode = mode;
  let lastSyncOff = -1;
  $effect(() => {
    const m = mode;
    const o = off;
    const changed = m !== lastMode || o !== lastSyncOff;
    lastMode = m;
    lastSyncOff = o;
    if (!changed) return;
    // 进入文本模式 / 基准页变化：重新按当前树序列化
    if (m === "text") textValue = serialize(tree);
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

      let title = "";
      let physPage: number | null = null;

      if (ti >= 0) {
        title = body.slice(0, ti).trim() || "无标题";
        const parsedNum = parseInt(body.slice(ti + 1), 10);

        if (Number.isFinite(parsedNum)) {
          physPage = fromViewPage(parsedNum, off); // 换算实际页码（超出范围的也会照常算出来）
        }
      } else {
        title = body.trim() || "无标题";
        physPage = fromViewPage(1, off);
      }

      const node: EditNode = {
        title,
        page: physPage ?? 1,
        expanded: true,
        kids: [],
      };

      while (stack.length && stack[stack.length - 1].depth >= depth) {
        stack.pop();
      }

      if (stack.length) {
        stack[stack.length - 1].node.kids.push(node);
      } else {
        root.push(node);
      }

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

  /** 文本编辑器内容变化：同步 textValue 与 tree */
  function onEditorChange(v: string) {
    textValue = v;
    tree = parseText(v);
    touched = true;
  }

  /**
   * 深度优先遍历出写入序列后校验：
   * - 越界：页码不在 [1, maxPage] → 自身不通过；
   * - 逆序：本项页码大于后一项页码 → 前一项不通过（pdfcpu 按写入顺序校验）。
   */
  function computeProblems(
    nodes: EditNode[],
    max: number,
  ): { problems: BookmarkProblem[]; invalid: Map<EditNode, string> } {
    const problems: BookmarkProblem[] = [];
    const invalid = new Map<EditNode, string>();
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

      // 换算当前页码与范围上限/后续页码的显示页码
      const viewP = Number.isFinite(p) ? toViewPage(p, off) : "无效";
      const viewMax = toViewPage(max, off);

      if (!Number.isFinite(p) || p < 1 || p > max) {
        const detail = `页码 ${viewP} 超出有效范围 [${toViewPage(1, off)}, ${viewMax}]`;
        invalid.set(n, detail);
        problems.push({
          kind: "range",
          title: n.title || "(无标题)",
          page: p,
          detail,
          order: i,
        });
        continue;
      }
      if (p > suffixMin[i + 1]) {
        const viewNext = toViewPage(suffixMin[i + 1], off);
        const detail = `页码 ${viewP} 大于后续书签页码 ${viewNext}`;
        invalid.set(n, detail);
        problems.push({
          kind: "order",
          title: n.title || "(无标题)",
          page: p,
          detail,
          order: i,
        });
      }
    }
    return { problems, invalid };
  }

  let validation = $derived(computeProblems(tree, maxPage));
  let problems = $derived(validation.problems);
  let invalidSet = $derived(validation.invalid);

  /**
   * 文本编辑器的行注解：
   * - 逻辑行号 = 文本中「非空行」的序号（与 parseText 的节点顺序一致）；
   * - 每条校验问题对应到它所属的那一行。
   */
  let codeAnnotations = $derived.by((): EditorAnnotation[] => {
    const ps = problems;
    void tree; // 显式依赖
    const rows: number[] = [];
    textValue.split("\n").forEach((l, i) => {
      if (l.trim()) rows.push(i);
    });
    return ps
      .filter((p) => p.order >= 0 && p.order < rows.length)
      .map((p) => ({
        row: rows[p.order],
        column: 0,
        text: p.detail,
        type: "warning" as const,
      }));
  });

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
    codeEditor?.reset();
    loadedID = "";
    attemptedID = "";
    tree = [];
    textValue = "";
    touched = false;
    busy = false;
    lastMode = mode;
    lastSyncOff = -1;
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
            <CodeEditor
              bind:this={codeEditor}
              bind:value={textValue}
              annotations={codeAnnotations}
              onchange={onEditorChange}
              class="h-[calc(100vh-314px)] w-full overflow-hidden rounded-md border border-input"
            />
            <p class="mt-2 text-xs text-muted-foreground">
              每行一个节点，标题与页码使用制表符分隔；行首使用制表符缩进表示子节点。
              <span class="whitespace-nowrap"
                >Tab / Ctrl+] 缩进；Ctrl+[ 反缩进；Alt+↑ / ↓
                移动行；Ctrl+Z撤销。</span
              >
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
