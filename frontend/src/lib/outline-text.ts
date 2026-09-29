import type { OutlineNode } from "../bindings/services";

/**
 * 目录树 ⇄ 缩进纯文本 转换。
 * 文本格式：每行一个节点；前导 \t 数量表示层级（相对上一行）；
 * 行内以 \t 分隔标题与页码（页码可省略，沿用父级页码）。
 */

interface FlatItem { depth: number; title: string; page: number; }

function flatten(nodes: OutlineNode[], depth: number, out: FlatItem[]) {
  for (const n of nodes) {
    out.push({ depth, title: n.title, page: n.page });
    flatten(n.kids ?? [], depth + 1, out);
  }
}

export function treeToText(nodes: OutlineNode[]): string {
  const flat: FlatItem[] = [];
  flatten(nodes, 0, flat);
  return flat
    .map((f) => "\t".repeat(f.depth) + f.title + "\t" + f.page)
    .join("\n");
}

/** 相对深度数组 → 严格树。深度跳级按 +1 处理。 */
export function textToTree(text: string): OutlineNode[] {
  const lines = text.split(/\r?\n/).filter((l) => l.trim() !== "");
  if (!lines.length) return [];

  const items: FlatItem[] = [];
  let lastDepth = 0;
  let defaultPage = 1;
  for (const line of lines) {
    const m = line.match(/^([\t ]*)/);
    const depth = m ? Math.floor(m[1].replace(/ /g, "\t").length / 1) : 0;
    const rest = line.slice(m ? m[1].length : 0);
    const parts = rest.split("\t").map((s) => s.trim()).filter((s) => s !== "");
    const title = parts[0] ?? "未命名";
    let page: number;
    if (parts.length >= 2 && /^\d+$/.test(parts[1])) {
      page = Math.max(1, parseInt(parts[1], 10));
    } else {
      // 页码缺省：同层沿用上一行的页码，子行沿用父页码
      page = items.length ? items[items.length - 1].page : 1;
    }
    const capped = depth > lastDepth + 1 ? lastDepth + 1 : depth;
    items.push({ depth: capped, title, page });
    lastDepth = capped;
    defaultPage = page;
  }
  void defaultPage;

  const root: OutlineNode[] = [];
  const stack: { depth: number; node: OutlineNode }[] = [];
  for (const it of items) {
    const node: OutlineNode = { title: it.title, page: it.page, kids: [] };
    while (stack.length && stack[stack.length - 1].depth >= it.depth) stack.pop();
    if (stack.length) {
      stack[stack.length - 1].node.kids!.push(node);
    } else {
      root.push(node);
    }
    stack.push({ depth: it.depth, node });
  }
  return root;
}

export function countNodes(nodes: OutlineNode[]): number {
  let n = 0;
  for (const x of nodes) { n += 1 + countNodes(x.kids ?? []); }
  return n;
}
