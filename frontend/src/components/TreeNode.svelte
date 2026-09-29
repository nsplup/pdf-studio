<script lang="ts">
  import Self from "./TreeNode.svelte";
  import { ChevronDown, ChevronRight, Plus, ListStart, ListEnd, ArrowRight, Trash2, TriangleAlert } from "lucide-svelte";

  export interface EditNode {
    title: string;
    page: number;
    kids: EditNode[];
    expanded: boolean;
  }

  let {
    node,
    path = [],
    depth = 0,
    invalidSet = null as Set<EditNode> | null,
    onChanged,
    onRemove,
    onInsertAbove,
    onInsertBelow,
  }: {
    node: EditNode;
    path?: number[];
    depth?: number;
    /** 校验不通过（越界/逆序）的节点集合，命中则警戒色高亮 */
    invalidSet?: Set<EditNode> | null;
    onChanged: () => void;
    onRemove: (path: number[]) => void;
    onInsertAbove: (path: number[]) => void;
    onInsertBelow: (path: number[]) => void;
  } = $props();

  let isInvalid = $derived(!!invalidSet && invalidSet.has(node));

  function addChild() {
    node.kids.push({ title: "新书签", page: node.page, kids: [], expanded: true });
    node.expanded = true;
    onChanged();
  }
</script>

<div>
  <div class="tree-row flex items-center gap-1.5 py-0.5 pr-2" style="margin-left:{depth * 16}px">
    {#if node.kids.length}
      <button class="flex h-6 w-6 shrink-0 items-center justify-center rounded hover:bg-accent" onclick={() => { node.expanded = !node.expanded; onChanged(); }}>
        {#if node.expanded}<ChevronDown class="h-4 w-4 text-muted-foreground" />{:else}<ChevronRight class="h-4 w-4 text-muted-foreground" />{/if}
      </button>
    {:else}
      <span class="h-6 w-6 shrink-0"></span>
    {/if}
    <!-- 固定占位：有效行也保留槽位，缩进/对齐不因图标出现而变化 -->
    <span class="warn-slot" aria-hidden={!isInvalid}>
      {#if isInvalid}<TriangleAlert class="h-3.5 w-3.5 text-amber-600" />{/if}
    </span>
    <input
      class="h-7 min-w-0 flex-1 rounded-md border border-input bg-transparent px-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      bind:value={node.title}
      oninput={onChanged}
    />
    <ArrowRight class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
    <span class="shrink-0 text-xs text-muted-foreground">第</span>
    <input
      type="number" min="1"
      class="h-7 w-16 shrink-0 rounded-md border border-input bg-transparent px-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      bind:value={node.page}
      oninput={onChanged}
    />
    <span class="shrink-0 text-xs text-muted-foreground">页</span>
    <button class="tree-act" title="上方插入同代书签" onclick={() => onInsertAbove(path)}>
      <ListStart class="h-4 w-4" />
    </button>
    <button class="tree-act" title="下方插入同代书签" onclick={() => onInsertBelow(path)}>
      <ListEnd class="h-4 w-4" />
    </button>
    <button class="tree-act" title="添加子书签" onclick={addChild}>
      <Plus class="h-4 w-4" />
    </button>
    <button class="tree-act tree-danger" title="删除（含子节点）" onclick={() => onRemove(path)}>
      <Trash2 class="h-4 w-4" />
    </button>
  </div>
  {#if node.expanded}
    {#each node.kids as kid, i (i)}
      <Self node={kid} path={[...path, i]} depth={depth + 1} {invalidSet} {onChanged} {onRemove} {onInsertAbove} {onInsertBelow} />
    {/each}
  {/if}
</div>

<style>
  /* 警告图标槽位：恒定占位，与行缩进对齐 */
  .warn-slot {
    width: 16px;
    height: 16px;
    flex: none;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  /* 行悬停 / 输入聚焦高亮：低饱和主题色，柔和不刺眼 */
  .tree-row {
    border-radius: 6px;
    padding: 2px 6px 2px 0;
    transition: background-color 0.12s;
  }
  .tree-row:hover,
  .tree-row:focus-within {
    background: hsl(var(--primary) / 0.08);
    box-shadow: inset 0 0 0 1px hsl(var(--primary) / 0.35);
  }
  .tree-row:focus-within {
    box-shadow: inset 0 0 0 1.5px hsl(var(--primary) / 0.6);
  }
  .tree-row :global(input:focus-visible) {
    outline: none;
  }
  .tree-act {
    display: flex; align-items: center; justify-content: center;
    height: 26px; width: 26px; flex: none;
    border-radius: 6px; color: hsl(var(--muted-foreground));
    opacity: 0; transition: opacity 0.12s, background-color 0.12s;
  }
  .tree-row:hover .tree-act,
  .tree-row:focus-within .tree-act {
    opacity: 1;
  }
  .tree-act:hover {
    background: hsl(var(--primary) / 0.12);
    color: hsl(var(--primary));
  }
  .tree-danger:hover {
    background: hsl(var(--destructive) / 0.12);
    color: hsl(var(--destructive));
  }
</style>
