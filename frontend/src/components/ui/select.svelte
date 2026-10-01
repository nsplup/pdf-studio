<script lang="ts">
  import { cn } from "../../lib/utils";
  import { ChevronDown, Check } from "lucide-svelte";

  let {
    value = $bindable(""),
    options = [],
    placeholder = "请选择",
    class: className = "",
    ...rest
  }: {
    value?: string;
    options?: { value: string; label: string }[];
    placeholder?: string;
    class?: string;
  } & Record<string, unknown> = $props();

  let open = $state(false);
  let root: HTMLElement | undefined = $state();
  let trigger: HTMLElement | undefined = $state();
  /** 弹层用 fixed 定位（跟随触发器矩形），避免被表格滚动容器/卡片裁切 */
  let pop = $state({ left: 0, top: 0, width: 0 });

  const current = $derived(options.find((o) => o.value === value));

  function syncPop() {
    if (!trigger) return;
    const r = trigger.getBoundingClientRect();
    const w = Math.max(r.width, 176);
    let left = r.left;
    // 防止超出视口右缘
    if (left + w > window.innerWidth - 8) left = window.innerWidth - 8 - w;
    pop = { left, top: r.bottom + 4, width: w };
  }

  function pick(v: string) {
    value = v;
    open = false;
  }

  function onDocClick(e: MouseEvent) {
    if (root && !root.contains(e.target as Node)) open = false;
  }
  $effect(() => {
    document.addEventListener("mousedown", onDocClick);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", close, true);
    return () => {
      document.removeEventListener("mousedown", onDocClick);
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", close, true);
    };
  });
  function close() {
    open = false;
  }
  /** 打开时计算弹层位置（fixed） */
  $effect(() => {
    if (open) syncPop();
  });
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  bind:this={root}
  class="relative"
  onkeydown={(e) => {
    if (e.key === "Escape") open = false;
  }}
>
  <button
    bind:this={trigger}
    type="button"
    role="combobox"
    aria-expanded={open}
    class={cn(
      "flex h-9 w-full items-center justify-between gap-1 whitespace-nowrap rounded-md border border-input",
      "bg-background px-3 py-2 text-sm shadow-sm",
      "hover:bg-accent/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1",
      "disabled:cursor-not-allowed disabled:opacity-50",
      open && "ring-2 ring-ring ring-offset-1",
      className,
    )}
    onclick={() => (open = !open)}
    {...rest}
  >
    <span class="truncate" class:text-muted-foreground={!current}
      >{current?.label ?? placeholder}</span
    >
    <ChevronDown
      class="h-4 w-4 shrink-0 opacity-50 transition-transform {open
        ? 'rotate-180'
        : ''}"
    />
  </button>

  {#if open}
    <div
      role="listbox"
      style="position:fixed; left:{pop.left}px; top:{pop.top}px; width:{pop.width}px; z-index:9999"
      class="max-h-72 overflow-auto rounded-md border border-border bg-popover text-popover-foreground shadow-md"
    >
      {#each options as o (o.value)}
        <button
          type="button"
          role="option"
          aria-selected={o.value === value}
          class="flex w-full items-center gap-2 px-2 py-1.5 text-left text-sm whitespace-nowrap
                 {o.value === value
            ? 'bg-accent/60 text-accent-foreground'
            : 'hover:bg-accent'}"
          onclick={() => pick(o.value)}
        >
          <Check
            class="h-3.5 w-3.5 {o.value === value
              ? 'opacity-100'
              : 'opacity-0'}"
          />
          {o.label}
        </button>
      {/each}
    </div>
  {/if}
</div>
