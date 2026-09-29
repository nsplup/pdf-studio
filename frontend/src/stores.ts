import { writable, get } from "svelte/store";
import { Events } from "@wailsio/runtime";
import type { TaskUpdate, DocInfo, PageDim } from "./bindings/services";

// ---------- 任务进度 ----------

export interface RunningTask {
  id: string;
  label: string;
  percent: number;
  message: string;
}

export const tasks = writable<RunningTask[]>([]);

// ---------- 通知 ----------

export interface Toast { kind: "ok" | "err" | "info"; text: string; }
export const toasts = writable<Toast[]>([]);

let toastSeq = 0;
export function notify(kind: Toast["kind"], text: string) {
  const id = ++toastSeq;
  toasts.update((l) => [...l.slice(-5), { kind, text, id } as any]);
  setTimeout(() => {
    toasts.update((l) => l.filter((t) => t.id !== id));
  }, 4500);
}

// ---------- 共享文档会话缓存 ----------
// 同一文件在所有功能页共享一个会话；页面编辑后的 DocInfo 全局同步。

export const currentDoc = writable<DocInfo | null>(null);

/** 页面内容资源序列：docID -> uuid[]（undefined = 未拉取，null = 拉取中）。内容寻址，无版本号 */
export const pageRes = writable<Record<string, string[] | null | undefined>>({});

/** 写入文档的页面资源序列 */
export function setPageRes(id: string, res: string[] | null) {
  pageRes.update((m) => ({ ...m, [id]: res }));
}

/** 纯前端页面操作：按索引删除/移动/插入后整体写回 */
export function replacePageRes(id: string, res: string[]) {
  pageRes.update((m) => ({ ...m, [id]: res }));
}

/** 页面逻辑尺寸（pt），与 pageRes 一一对应（含未保存的插入/删除） */
export const pageDims = writable<Record<string, PageDim[] | null | undefined>>({});

export function setPageDims(id: string, dims: PageDim[] | null) {
  pageDims.update((m) => ({ ...m, [id]: dims }));
}

export function replacePageDims(id: string, dims: PageDim[]) {
  pageDims.update((m) => ({ ...m, [id]: dims }));
}

/** 打开文档：命中缓存直接复用（跨功能页共享，切页不丢失） */
export async function openDocument(path: string): Promise<DocInfo> {
  const cur = get(currentDoc);
  if (cur && cur.sourcePath === path) return cur;
  const { DocumentService } = await import("./bindings/services");
  const doc = await DocumentService.Open(path);
  currentDoc.set(doc);
  return doc;
}

/** 页面编辑后统一刷新：更新共享 DocInfo（缩略图按页版本另行处理） */
export function applyDocUpdate(doc: DocInfo) {
  currentDoc.set(doc);
}

// ---------- 任务事件订阅（应用启动时调用一次） ----------

export function initTaskEvents() {
  Events.On("task:update", (ev: any) => {
    const u: TaskUpdate | undefined = ev?.data?.[0] ?? ev?.data;
    if (!u || !u.taskId) return;
    tasks.update((list) => {
      const i = list.findIndex((t) => t.id === u.taskId);
      if (u.kind === "done" || u.kind === "error") {
        if (i >= 0) list.splice(i, 1);
        return [...list];
      }
      const entry: RunningTask = {
        id: u.taskId,
        label: u.label ?? "任务",
        percent: u.percent ?? 0,
        message: u.message ?? "",
      };
      if (i >= 0) {
        list[i] = entry;
        return [...list];
      }
      return [...list, entry];
    });
    if (u.kind === "error") notify("err", `${u.label ?? "任务"}失败：${u.error ?? "未知错误"}`);
    if (u.kind === "done" || u.kind === "error") {
      const r = taskResolvers.get(u.taskId);
      if (r) {
        taskResolvers.delete(u.taskId);
        r(u.kind === "done", u.error, u.result);
      }
    }
  });
}

// ---------- 放置模式（移动到 / 导入放置） ----------
// kind: "move" 移动已选页 | "import" 放置导入缓冲区
export interface Placement {
  active: boolean;
  kind: "move" | "import" | "blank";
  buffer: { id: string; pageCount: number; res?: string[]; dims?: PageDim[] } | null;
}
export const placement = writable<Placement>({
  active: false,
  kind: "move",
  buffer: null,
});

export function startPlacement(kind: Placement["kind"], buffer: Placement["buffer"] = null) {
  placement.set({ active: true, kind, buffer });
}
export function stopPlacement() {
  placement.set({ active: false, kind: "move", buffer: null });
}

// ---------- 任务完成等待 ----------
const taskResolvers = new Map<string, (ok: boolean, err?: string, result?: any) => void>();

export function waitTask(taskId: string): Promise<any> {
  return new Promise((resolve, reject) => {
    taskResolvers.set(taskId, (ok, err, result) =>
      ok ? resolve(result) : reject(new Error(err ?? "任务失败"))
    );
  });
}
