<script module lang="ts">
  /**
   * 文本编辑器的注解描述。
   * `row` 为逻辑行号（0 起，含软换行行内的逻辑行）。
   */
  export interface EditorAnnotation {
    row: number;
    column?: number;
    text: string;
    type?: "warning" | "error" | "info";
  }
</script>

<script lang="ts">
  import { onMount, untrack } from "svelte";

  interface Props {
    /** 双向绑定文本内容 */
    value?: string;
    /** 注解列表（响应式） */
    annotations?: EditorAnnotation[];
    readonly?: boolean;
    placeholder?: string;
    tabSize?: number;
    class?: string;
    /** 内容变化回调 */
    onchange?: (value: string) => void;
  }

  let {
    value = $bindable(""),
    annotations: annProp = [],
    readonly = false,
    placeholder = "",
    tabSize = 4,
    class: className = "",
    onchange,
  }: Props = $props();

  // ---------- DOM ----------
  let textareaEl: HTMLTextAreaElement | undefined = $state();

  // ---------- 内容 ----------
  /** 逻辑行，用于镜像层渲染 */
  let lines = $state<string[]>(value.split("\n"));
  /** 当前文本（非响应式，作为历史记录的事实来源） */
  let lastText = value;

  // ---------- 选区（用于历史记录） ----------
  let selStart = 0;
  let selEnd = 0;

  // ---------- 注解 ----------
  let annOverride = $state<EditorAnnotation[] | null>(null);
  const anns = $derived(annOverride ?? annProp);
  const annMap = $derived.by(() => {
    const m = new Map<number, EditorAnnotation[]>();
    for (const a of anns) {
      const arr = m.get(a.row);
      if (arr) arr.push(a);
      else m.set(a.row, [a]);
    }
    return m;
  });

  // =====================================================================
  // 历史记录
  // ---------------------------------------------------------------------
  // 设计：
  //  * 撤销栈保存「编辑前快照」；重做栈保存「被撤销时的当前快照」。
  //  * 是否合并由「时间窗口 + 编辑类型 + 光标连续性」三要素共同判定，
  //    与 CodeMirror / VS Code 的 history 扩展思路一致：
  //      - 时间窗口内（默认 500ms）
  //      - 同一种编辑类型（insert / delete / indent / outdent / move / newline）
  //      - insert/delete 时要求光标在上一次编辑结束位置附近
  //  * 粘贴、剪切、输入法组合、撤销/重做后都会断开合并链，
  //    保证每次撤销对应一个语义明确的编辑动作。
  // =====================================================================

  interface Snapshot {
    text: string;
    selStart: number;
    selEnd: number;
  }

  const MAX_HISTORY = 500;
  /** 连续同类编辑的合并窗口（毫秒） */
  const MERGE_WINDOW_MS = 500;
  /** 光标位移容差：超过此距离视为不连续编辑 */
  const CURSOR_SLACK = 2;

  let undoStack: Snapshot[] = [];
  let redoStack: Snapshot[] = [];

  // 合并状态
  let lastEditAt = 0;
  let lastEditKind = "";
  let lastEditCursor = -1;
  /** 粘贴 / 剪切等需要强制断开合并 */
  let forceNewEntry = false;
  /** 输入法组合状态 */
  let composing = false;
  let composingBefore: Snapshot | null = null;

  function clearHistory() {
    undoStack.length = 0;
    redoStack.length = 0;
    resetMergeState();
    forceNewEntry = false;
    composing = false;
    composingBefore = null;
  }

  function resetMergeState() {
    lastEditAt = 0;
    lastEditKind = "";
    lastEditCursor = -1;
  }

  /**
   * 判断本次编辑是否与上一次编辑合并。
   * 合并 = 不压新快照（栈顶已经保存了「本次会话开始前」的状态）。
   */
  function shouldMerge(
    kind: string,
    cursorAfter: number,
    forceNew: boolean,
  ): boolean {
    if (forceNew) return false;
    if (undoStack.length === 0) return false;
    if (Date.now() - lastEditAt >= MERGE_WINDOW_MS) return false;
    if (kind !== lastEditKind) return false;

    // indent / outdent / move 属于整块操作，不做光标连续性要求
    const needsContiguity = kind === "insert" || kind === "delete";
    if (
      needsContiguity &&
      Math.abs(cursorAfter - lastEditCursor) > CURSOR_SLACK
    ) {
      return false;
    }
    return true;
  }

  /** 记录一次编辑：必要时把「编辑前状态」压入撤销栈 */
  function pushHistoryEntry(
    kind: string,
    oldText: string,
    cursorAfter: number,
    forceNew = false,
  ) {
    if (!shouldMerge(kind, cursorAfter, forceNew)) {
      undoStack.push({ text: oldText, selStart, selEnd });
      if (undoStack.length > MAX_HISTORY) undoStack.shift();
    }
    redoStack.length = 0;
    lastEditAt = Date.now();
    lastEditKind = kind;
    lastEditCursor = cursorAfter;
  }

  function doUndo() {
    if (undoStack.length === 0) return;
    const snap = undoStack.pop()!;
    redoStack.push({ text: lastText, selStart, selEnd });
    resetMergeState();
    applyText(snap.text, snap.selStart, snap.selEnd);
  }

  function doRedo() {
    if (redoStack.length === 0) return;
    const snap = redoStack.pop()!;
    undoStack.push({ text: lastText, selStart, selEnd });
    resetMergeState();
    applyText(snap.text, snap.selStart, snap.selEnd);
  }

  // =====================================================================
  // 内容应用
  // =====================================================================

  function applyText(text: string, s: number, e: number) {
    lastText = text;
    lines = text.split("\n");
    const ta = textareaEl;
    if (ta) {
      if (ta.value !== text) ta.value = text;
      const len = text.length;
      const ss = Math.min(Math.max(s, 0), len);
      const ee = Math.min(Math.max(e, 0), len);
      ta.setSelectionRange(ss, ee);
    }
    selStart = s;
    selEnd = e;
    lastEditCursor = e;
    value = text;
    onchange?.(text);
  }

  // =====================================================================
  // 对外 API
  // =====================================================================

  /** 替换注解（也可直接用 annotations prop） */
  export function setAnnotations(list: EditorAnnotation[]) {
    annOverride = list;
  }

  export function getValue(): string {
    return lastText;
  }

  /** 以编程方式写入内容（同步 value，并清空历史记录） */
  export function setValue(text: string) {
    clearHistory();
    applyText(text, 0, 0);
  }

  export function isFocused(): boolean {
    return !!textareaEl && document.activeElement === textareaEl;
  }

  export function focus() {
    textareaEl?.focus();
  }

  export function blur() {
    textareaEl?.blur();
  }

  /** 关闭文档时清空内部状态 */
  export function reset() {
    clearHistory();
    annOverride = null;
    lines = [""];
    lastText = "";
    selStart = 0;
    selEnd = 0;
    if (textareaEl) textareaEl.value = "";
    value = "";
  }

  // =====================================================================
  // 生命周期 / 外部同步
  // =====================================================================

  onMount(() => {
    if (textareaEl) textareaEl.value = value;
  });

  // 外部写入 value → 同步到编辑器，并清空历史记录（外部全量替换）
  $effect(() => {
    const v = value;
    const ta = textareaEl;
    if (!ta) return;
    const cur = untrack(() => lines.join("\n"));
    if (v === cur) return;
    lines = v.split("\n");
    lastText = v;
    ta.value = v;
    clearHistory();
  });

  // =====================================================================
  // 位置换算工具
  // =====================================================================

  function lineIndexAt(text: string, pos: number): number {
    let n = 0;
    const lim = Math.min(Math.max(pos, 0), text.length);
    for (let i = 0; i < lim; i++) if (text.charCodeAt(i) === 10) n++;
    return n;
  }

  function lineStartOffset(text: string, line: number): number {
    if (line <= 0) return 0;
    let n = 0;
    for (let i = 0; i < text.length; i++) {
      if (text.charCodeAt(i) === 10) {
        n++;
        if (n === line) return i + 1;
      }
    }
    return text.length;
  }

  function offsetOfLines(arr: string[], line: number): number {
    let off = 0;
    for (let i = 0; i < line && i < arr.length; i++) off += arr[i].length + 1;
    return off;
  }

  // =====================================================================
  // 事件处理
  // =====================================================================

  function trackSelection() {
    const ta = textareaEl;
    if (!ta) return;
    selStart = ta.selectionStart;
    selEnd = ta.selectionEnd;
  }

  function onInput() {
    const ta = textareaEl;
    if (!ta) return;

    const newText = ta.value;
    const oldText = lastText;
    if (newText === oldText) return;

    // 输入法组合期间不细分历史，等 compositionend 统一处理
    if (composing) {
      lastText = newText;
      lines = newText.split("\n");
      selStart = ta.selectionStart;
      selEnd = ta.selectionEnd;
      value = newText;
      onchange?.(newText);
      return;
    }

    const kind = newText.length < oldText.length ? "delete" : "insert";
    const cursorAfter = ta.selectionEnd;

    pushHistoryEntry(kind, oldText, cursorAfter, forceNewEntry);
    forceNewEntry = false;

    applyText(newText, ta.selectionStart, ta.selectionEnd);
  }

  function onBeforeInput() {
    // 在 DOM 变更前锁定选区（供历史记录使用）
    trackSelection();
  }

  function onPaste() {
    forceNewEntry = true;
  }

  function onCut() {
    forceNewEntry = true;
  }

  function onCompositionStart() {
    const ta = textareaEl;
    if (!ta) return;
    composing = true;
    composingBefore = {
      text: lastText,
      selStart: ta.selectionStart,
      selEnd: ta.selectionEnd,
    };
  }

  function onCompositionEnd() {
    composing = false;
    const before = composingBefore;
    composingBefore = null;
    if (before && before.text !== lastText) {
      undoStack.push(before);
      if (undoStack.length > MAX_HISTORY) undoStack.shift();
      redoStack.length = 0;
      resetMergeState();
    }
  }

  // =====================================================================
  // 通用插入 / 剪切当前行
  // =====================================================================

  /**
   * 在光标处插入文本（若有选区则替换选区）。
   * @param kind 历史记录中的编辑类型，默认 "insert"
   */
  function insertText(txt: string, kind = "insert") {
    const ta = textareaEl;
    if (!ta) return;

    const text = lastText;
    const s = ta.selectionStart;
    const e = ta.selectionEnd;
    if (s === e && txt.length === 0) return;

    const newText = text.slice(0, s) + txt + text.slice(e);
    const caret = s + txt.length;

    // 记录「编辑前」的选区
    selStart = s;
    selEnd = e;
    // forceNew：每次 Tab 都是独立的撤销点，不与其后的输入合并
    pushHistoryEntry(kind, text, caret, true);

    applyText(newText, caret, caret);
  }

  /** 复制文本到剪贴板（带降级方案） */
  function copyToClipboard(text: string) {
    const clip = navigator.clipboard;
    if (clip?.writeText) {
      clip.writeText(text).catch(() => fallbackCopy(text));
    } else {
      fallbackCopy(text);
    }
  }

  function fallbackCopy(text: string) {
    const tmp = document.createElement("textarea");
    tmp.value = text;
    tmp.setAttribute("readonly", "");
    tmp.style.position = "fixed";
    tmp.style.top = "-1000px";
    tmp.style.opacity = "0";
    document.body.appendChild(tmp);
    tmp.select();
    try {
      document.execCommand("copy");
    } catch {
      /* 忽略：复制失败不影响后续删除 */
    }
    document.body.removeChild(tmp);
    textareaEl?.focus();
  }

  /** 剪切当前行：复制该行到剪贴板，并将其从文本中删除 */
  function cutCurrentLine() {
    const ta = textareaEl;
    if (!ta) return;

    const text = lastText;
    const pos = ta.selectionStart;
    const arr = text.split("\n");
    const li = lineIndexAt(text, pos);
    if (li < 0 || li >= arr.length) return;

    const lineText = arr[li];
    const lineStart = offsetOfLines(arr, li);

    let newText: string;
    let caret: number;

    if (arr.length === 1) {
      // 只剩一行：直接清空
      newText = "";
      caret = 0;
    } else if (li < arr.length - 1) {
      // 非末行：连同其后的换行一起删除
      newText =
        text.slice(0, lineStart) + text.slice(lineStart + lineText.length + 1);
      caret = lineStart;
    } else {
      // 末行：连同其前的换行一起删除
      newText = text.slice(0, lineStart - 1);
      caret = lineStart - 1;
    }

    // 写入剪贴板：带上换行符，粘贴回去可还原为完整一行
    copyToClipboard(lineText + "\n");

    selStart = pos;
    selEnd = pos;
    pushHistoryEntry("cutline", text, caret, true);

    applyText(newText, caret, caret);
  }

  function onKeydown(ev: KeyboardEvent) {
    if (readonly) return;

    // 在任何处理之前先固定选区快照（用于历史记录）
    trackSelection();

    const mod = ev.ctrlKey || ev.metaKey;
    const k = ev.key;

    // 撤销 / 重做
    if (mod && !ev.shiftKey && !ev.altKey && k.toLowerCase() === "z") {
      ev.preventDefault();
      doUndo();
      return;
    }
    if (mod && ev.shiftKey && !ev.altKey && k.toLowerCase() === "z") {
      ev.preventDefault();
      doRedo();
      return;
    }
    if (mod && !ev.shiftKey && !ev.altKey && k.toLowerCase() === "y") {
      ev.preventDefault();
      doRedo();
      return;
    }

    // Ctrl/Cmd+X：无选区时剪切「当前行」；有选区时保留浏览器默认剪切
    if (mod && !ev.shiftKey && !ev.altKey && k.toLowerCase() === "x") {
      const ta = textareaEl;
      if (ta && ta.selectionStart === ta.selectionEnd) {
        ev.preventDefault();
        cutCurrentLine();
        return;
      }
      return;
    }

    if (k === "Enter" && !mod && !ev.altKey && !composing) {
      ev.preventDefault();
      const ta = textareaEl;
      if (!ta) return;

      const text = lastText;
      const s = ta.selectionStart;
      const e = ta.selectionEnd;

      const lineStart = lineStartOffset(text, lineIndexAt(text, s));
      // 抽取当前行行首的 \t 前缀作为继承缩进
      const prefixMatch = /^\t*/.exec(text.slice(lineStart, s));
      const indent = ev.shiftKey ? "" : (prefixMatch?.[0] ?? "");

      const inserted = "\n" + indent;
      const newText = text.slice(0, s) + inserted + text.slice(e);
      const newPos = s + inserted.length;

      selStart = s;
      selEnd = e;
      pushHistoryEntry("newline", text, newPos, /* forceNew */ false);
      applyText(newText, newPos, newPos);
      return;
    }
    // Tab：在光标处插入制表符 \t
    if (k === "Tab" && !mod && !ev.altKey) {
      ev.preventDefault();
      insertText("\t", "tab");
      return;
    }
    if (mod && !ev.shiftKey && k === "]") {
      ev.preventDefault();
      indent(1);
      return;
    }
    if (mod && !ev.shiftKey && k === "[") {
      ev.preventDefault();
      indent(-1);
      return;
    }

    // 上下移动当前行
    if (ev.altKey && !mod && k === "ArrowUp") {
      ev.preventDefault();
      moveLines(-1);
      return;
    }
    if (ev.altKey && !mod && k === "ArrowDown") {
      ev.preventDefault();
      moveLines(1);
      return;
    }
  }

  /** 缩进（dir=1）或反缩进（dir=-1）当前行 / 选中行块 */
  function indent(dir: 1 | -1) {
    const ta = textareaEl;
    if (!ta) return;

    const text = lastText;
    const s = ta.selectionStart;
    const e = ta.selectionEnd;
    const arr = text.split("\n");

    const sl = lineIndexAt(text, s);
    const rawEl = lineIndexAt(text, e);

    // 选区结束落在行首时，不把该行算入
    let el = rawEl;
    if (rawEl > sl && e > s && e > 0 && text.charCodeAt(e - 1) === 10) {
      el = rawEl - 1;
    }

    const delta = new Array<number>(arr.length).fill(0);
    let changed = false;

    for (let i = sl; i <= el; i++) {
      if (i < 0 || i >= arr.length) continue;
      if (dir === 1) {
        arr[i] = "\t" + arr[i];
        delta[i] = 1;
        changed = true;
      } else if (arr[i].charCodeAt(0) === 9) {
        arr[i] = arr[i].slice(1);
        delta[i] = -1;
        changed = true;
      }
    }
    if (!changed) return;

    const sCol = s - lineStartOffset(text, sl);
    const eCol = e - lineStartOffset(text, rawEl);

    const newS =
      offsetOfLines(arr, sl) +
      (sCol === 0 ? 0 : Math.min(sCol + delta[sl], arr[sl].length));
    const newE =
      offsetOfLines(arr, rawEl) +
      (eCol === 0 ? 0 : Math.min(eCol + delta[rawEl], arr[rawEl].length));

    // 记录历史：变更前选区为当前 s / e
    selStart = s;
    selEnd = e;
    pushHistoryEntry(dir === 1 ? "indent" : "outdent", text, newE);

    applyText(arr.join("\n"), newS, newE);
  }

  /** 将当前行 / 选中行块上移或下移一行 */
  function moveLines(dir: -1 | 1) {
    const ta = textareaEl;
    if (!ta) return;

    const text = lastText;
    const s = ta.selectionStart;
    const e = ta.selectionEnd;
    const arr = text.split("\n");

    const sl = lineIndexAt(text, s);
    const rawEl = lineIndexAt(text, e);
    let el = rawEl;
    if (rawEl > sl && e > s && e > 0 && text.charCodeAt(e - 1) === 10) {
      el = rawEl - 1;
    }

    if (dir === -1 && sl === 0) return;
    if (dir === 1 && el >= arr.length - 1) return;

    const block = arr.splice(sl, el - sl + 1);
    const insertAt = dir === -1 ? sl - 1 : sl + 1;
    arr.splice(insertAt, 0, ...block);

    const sCol = s - lineStartOffset(text, sl);
    const eCol = e - lineStartOffset(text, rawEl);

    const targetStart = insertAt;
    const targetEnd = Math.min(insertAt + (rawEl - sl), arr.length - 1);

    const newS =
      offsetOfLines(arr, targetStart) + Math.min(sCol, arr[targetStart].length);
    const newE =
      offsetOfLines(arr, targetEnd) + Math.min(eCol, arr[targetEnd].length);

    selStart = s;
    selEnd = e;
    pushHistoryEntry("move", text, newE);

    applyText(arr.join("\n"), newS, newE);
  }
</script>

<div
  class="code-editor {className}"
  style="--tab-size: {tabSize}; --line-height: 20px;"
>
  <div class="ce-inner">
    <!-- 镜像层：逐逻辑行渲染，用于行高亮 / 装订线 / 软换行撑高 -->
    <div class="ce-mirror" aria-hidden="true">
      {#each lines as line, i (i)}
        {@const lineAnns = annMap.get(i)}
        <div class="ce-line" class:has-ann={!!lineAnns?.length}>
          <span class="ce-gutter">
            <span
              class="ce-icon"
              class:on={!!lineAnns?.length}
              title={lineAnns?.map((a) => a.text).join("\n")}
            ></span>
            <span class="ce-num">{i + 1}</span>
          </span>{line}
        </div>
      {/each}
    </div>

    <!-- 真实输入层：透明背景，文字可见 -->
    <textarea
      bind:this={textareaEl}
      class="ce-input"
      {readonly}
      {placeholder}
      spellcheck="false"
      autocomplete="off"
      autocorrect="off"
      autocapitalize="off"
      wrap="soft"
      oninput={onInput}
      onbeforeinput={onBeforeInput}
      onkeydown={onKeydown}
      onpaste={onPaste}
      oncut={onCut}
      oncompositionstart={onCompositionStart}
      oncompositionend={onCompositionEnd}
      onselect={trackSelection}
      onmouseup={trackSelection}
      onfocus={trackSelection}
    ></textarea>
  </div>
</div>

<style>
  .code-editor {
    /* ---- 布局变量：供镜像层 / 输入层共享，保证像素级对齐 ---- */
    --pad-y: 8px;
    --pad-right: 12px;
    --gutter-w: 3.5rem;
    --text-gap: 12px;
    --font-size: 14px;

    position: relative;
    overflow-y: auto;
    overflow-x: hidden;
    background: #1d1f21;
    color: #c5c8c6;
    font-family: monospace;
    font-size: var(--font-size, 14px);
    line-height: var(--line-height, 20px);
    letter-spacing: normal;
    tab-size: var(--tab-size, 4);
    -moz-tab-size: var(--tab-size, 4);
  }

  .ce-inner {
    position: relative;
    min-height: 100%;
  }

  /* ---------------- 镜像层（行高亮 / 装订线） ---------------- */

  .ce-mirror {
    position: relative;
    z-index: 1;
    /* 让文本区域的鼠标事件穿透到 textarea，仅图标重新启用 */
    pointer-events: none;
    padding: var(--pad-y) var(--pad-right)
      var(--pad-y) var(--gutter-w);
    color: transparent;
    user-select: none;
    white-space: pre-wrap;
    overflow-wrap: break-word;
    word-break: break-word;
    box-sizing: border-box;
  }

  .ce-line {
    position: relative;
    /* 给高亮装饰条与文本之间留出间距 */
    padding-left: var(--text-gap);
    min-height: var(--line-height, 20px);
    white-space: pre-wrap;
    overflow-wrap: break-word;
    word-break: break-word;
    tab-size: var(--tab-size, 4);
    -moz-tab-size: var(--tab-size, 4);
  }

  /* 有校验问题的行：整行浅色高亮 + 左侧装饰条 */
  .ce-line.has-ann {
    background: rgba(245, 158, 11, 0.11);
    box-shadow: inset 2px 0 0 0 rgba(245, 158, 11, 0.9);
  }

  .ce-gutter {
    position: absolute;
    left: calc(-1 * var(--gutter-w));
    top: 0;
    bottom: 0;
    width: var(--gutter-w);
    display: flex;
    align-items: flex-start;
    justify-content: flex-end;
    gap: 4px;
    padding-right: 6px;
    box-sizing: border-box;
    user-select: none;
  }

  /* 行号与图标使用同样的高度盒 + flex 居中，保证二者垂直对齐 */
  .ce-num {
    height: var(--line-height, 20px);
    display: flex;
    align-items: center;
    justify-content: flex-end;
    min-width: 1.5rem;
    color: #5c6370;
    font-size: var(--font-size, 14px);
    line-height: var(--line-height, 20px);
    text-align: right;
  }

  .ce-line.has-ann .ce-num {
    color: #f59e0b;
  }

  .ce-icon {
    display: none;
    width: var(--font-size, 14px);
    height: var(--line-height, 20px);
    flex: none;
    align-items: center;
    justify-content: center;
    background-repeat: no-repeat;
    background-position: center center;
    background-size: var(--font-size, 14px) var(--font-size, 14px);
  }

  .ce-icon.on {
    display: flex;
    /* 图标单独开启指针事件，用于悬停提示 */
    pointer-events: auto;
    cursor: help;
    background-image: url("data:image/svg+xml;base64,PHN2ZyB4bWxucz0naHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmcnIHZpZXdCb3g9JzAgMCAyNCAyNCcgZmlsbD0nbm9uZScgc3Ryb2tlPScjZjU5ZTBiJyBzdHJva2Utd2lkdGg9JzInIHN0cm9rZS1saW5lY2FwPSdyb3VuZCcgc3Ryb2tlLWxpbmVqb2luPSdyb3VuZCc+PHBhdGggZD0nbTIxLjczIDE4LTgtMTRhMiAyIDAgMCAwLTMuNDggMGwtOCAxNEEyIDIgMCAwIDAgNCAyMWgxNmEyIDIgMCAwIDAgMS43My0zJy8+PHBhdGggZD0nTTEyIDl2NCcvPjxwYXRoIGQ9J00xMiAxN2guMDEnLz48L3N2Zz4=");
  }

  /* ---------------- 输入层 ---------------- */

  .ce-input {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    margin: 0;
    /* 左右 padding 与镜像层保持相同的「文本起始位置」 */
    padding: var(--pad-y) var(--pad-right) var(--pad-y)
      calc(var(--gutter-w) + var(--text-gap));
    border: 0;
    outline: none;
    resize: none;
    overflow: hidden;
    background: transparent;
    color: #c5c8c6;
    font-family: monospace;
    font-size: var(--font-size, 14px);
    line-height: var(--line-height, 20px);
    letter-spacing: normal;
    word-spacing: normal;
    tab-size: var(--tab-size, 4);
    -moz-tab-size: var(--tab-size, 4);
    white-space: pre-wrap;
    overflow-wrap: break-word;
    word-break: break-word;
    caret-color: #c5c8c6;
    box-sizing: border-box;
  }

  .ce-input::selection {
    background: rgba(129, 162, 190, 0.4);
  }

  /* ---------------- 滚动条 ---------------- */

  .code-editor::-webkit-scrollbar {
    width: 5px;
    height: 5px;
  }
  .code-editor::-webkit-scrollbar-track {
    background: transparent;
  }
  .code-editor::-webkit-scrollbar-thumb {
    background: rgba(150, 152, 150, 0.28);
    border-radius: 5px;
    border-color: transparent;
  }
  .code-editor::-webkit-scrollbar-thumb:hover {
    background: rgba(150, 152, 150, 0.48);
  }
  .code-editor::-webkit-scrollbar-thumb:active {
    background: rgba(150, 152, 150, 0.62);
  }
  .code-editor::-webkit-scrollbar-corner {
    background: transparent;
  }
</style>
