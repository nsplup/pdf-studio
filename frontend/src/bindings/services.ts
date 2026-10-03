/**
 * PDF Studio 服务层绑定（与 Go 服务方法一一对应）。
 * 命名规则：<module path>.<ServiceName>.<Method>，与 Wails v3 绑定生成器一致。
 */
import { Call } from "@wailsio/runtime";

const NS = "pdfstudio/internal/service";

function call<T>(service: string, method: string, ...args: unknown[]): Promise<T> {
  return Call.ByName(`${NS}.${service}.${method}`, ...args) as Promise<T>;
}

// ---------- AppService（生命周期） ----------

export const AppService = {
  /** 前端就绪：窗口显示 */
  Ready: () => call<void>("AppService", "Ready"),
};

// ---------- 数据模型 ----------

export interface PageDim { width: number; height: number; }

export interface DocInfo {
  id: string;
  sourcePath: string;
  fileName: string;
  pageCount: number;
  pages: PageDim[];
  hasThumbs: boolean;
  /** 页面内容资源 uuid 序列（内容寻址；前端据此拼装 /thumbs/<uuid>/<W>.png） */
  pageRes?: string[];
}

export interface OutlineNode {
  title: string;
  page: number;
  bold?: boolean;
  italic?: boolean;
  kids?: OutlineNode[];
}

export interface PageLabel {
  startPage: number; // 0-based 区间起始页
  prefix?: string;
  style?: string;    // "" | D | R | r | A | a
  startValue: number;
}

export interface AttachmentInfo {
  fileName: string;
  description?: string;
  modTime?: string | null;
}

/** 全局保存选项：nil/undefined 字段表示该类数据未修改，不写入 */
export interface SaveOptions {
  outline?: OutlineNode[] | undefined;
  labels?: PageLabel[] | undefined;
  /** 最终页面序列（uuid / blank-<w>x<h> 标记）；删除/移动/插入均为纯前端操作，保存时提交 */
  pageSeq?: string[] | undefined;
}

/** PlaceBuffer 结果：文档信息 + 导入来源 PDF 合并出的辅助数据 */
export interface PlaceResult {
  info: DocInfo;
  addedOutline: OutlineNode[];
  addedLabels: PageLabel[];
  addedAttachments: AttachmentInfo[];
}

// ---------- DocumentService（页面编辑） ----------

export const DocumentService = {
  Open: (path: string) => call<DocInfo>("DocumentService", "Open", path),
  CreateBlank: () => call<DocInfo>("DocumentService", "CreateBlank"),
  Info: (id: string) => call<DocInfo>("DocumentService", "Info", id),
  Close: (id: string) => call<void>("DocumentService", "Close", id),

  /** 生成缩略图：只渲染 fromPage（1-based）起的页面，之前的缓存文件保留 */
  GenerateThumbnails: (id: string, width: number, fromPage: number) =>
    call<string>("DocumentService", "GenerateThumbnails", id, width, fromPage),

  /** 同步渲染单页大图（预览浮层），返回 URL */
  PageThumbnail: (id: string, pageNr: number, width: number) =>
    call<string>("DocumentService", "PageThumbnail", id, pageNr, width),

  /** 页面内容资源：渲染每页 140px 基图并返回 uuid 有序列表（内容寻址缓存） */
  PageResources: (id: string) => call<string[]>("DocumentService", "PageResources", id),

  /** 在 atIndex 页前/后插入 count 张空白页（零重渲染，占位图填充） */
  InsertBlankPages: (id: string, atIndex: number, count: number, before: boolean) =>
    call<DocInfo>("DocumentService", "InsertBlankPages", id, atIndex, count, before),

  /** 批量导入图片/PDF 到待放置缓冲区；任务完成 result 为 {bufferID,pageCount,res,dims} */
  Import: (id: string, paths: string[]) =>
    call<string>("DocumentService", "Import", id, { paths }),
  /** 快速扫描导入文件的总页数（不解析内容），用于渲染占位符 */
  ScanImportCount: (paths: string[]) =>
    call<number>("DocumentService", "ScanImportCount", paths),
  /** 放置缓冲区内容（合并来源 PDF 的书签/页标签/附件） */
  PlaceBuffer: (id: string, atIndex: number, before: boolean) =>
    call<PlaceResult>("DocumentService", "PlaceBuffer", id, atIndex, before),

  /** 仅合并缓冲区辅助数据（书签/页标签/附件，页码平移）；页面顺序由前端 pageSeq 维护 */
  AbsorbBufferAux: (id: string, atIndex: number, before: boolean) =>
    call<PlaceResult>("DocumentService", "AbsorbBufferAux", id, atIndex, before),

  DiscardBuffer: (id: string) => call<void>("DocumentService", "DiscardBuffer", id),
  MovePages: (id: string, pages: string, atIndex: number, before: boolean) =>
    call<DocInfo>("DocumentService", "MovePages", id, pages, atIndex, before),
  DeletePages: (id: string, pages: string) =>
    call<DocInfo>("DocumentService", "DeletePages", id, pages),

  /** 全局保存：页面 + 书签 + 页标签 + 附件会话一并合并写出 */
  Save: (id: string, opts: SaveOptions | null) =>
    call<DocInfo>("DocumentService", "Save", id, opts),
  SaveAs: (id: string, path: string, opts: SaveOptions | null) =>
    call<DocInfo>("DocumentService", "SaveAs", id, path, opts),
};

// ---------- OutlineService（书签读取/导出） ----------

export const OutlineService = {
  /** 读取文件书签树（用于打开新 PDF 时初始化编辑树） */
  Read: (path: string) => call<OutlineNode[] | null>("OutlineService", "Read", path),
  /** 导出书签为缩进文本（每行一节点，\t 层级，\t 分隔标题与页码） */
  ExportText: (inPath: string, outPath: string) =>
    call<void>("OutlineService", "ExportText", inPath, outPath),
};

// ---------- MetaService（页标签 + 附件会话） ----------

export const MetaService = {
  ReadPageLabels: (path: string) =>
    call<PageLabel[] | null>("MetaService", "ReadPageLabels", path),

  /** 附件会话：列出（原文档附件 - 已移除 + 已添加） */
  ListDocAttachments: (id: string) =>
    call<AttachmentInfo[]>("MetaService", "ListDocAttachments", id),
  /** 添加附件（复制到会话，保存时写入 PDF） */
  AddDocAttachments: (id: string, paths: string[]) =>
    call<AttachmentInfo[]>("MetaService", "AddDocAttachments", id, paths),
  /** 移除附件（原文档附件记入移除集合，会话新增直接删除） */
  RemoveDocAttachments: (id: string, names: string[]) =>
    call<AttachmentInfo[]>("MetaService", "RemoveDocAttachments", id, names),
  /** 清空全部附件（原文档 + 新增），保存时生效 */
  ClearDocAttachments: (id: string) =>
    call<AttachmentInfo[]>("MetaService", "ClearDocAttachments", id),
};

// ---------- 任务事件 ----------

export interface TaskUpdate {
  taskId: string;
  kind: "progress" | "done" | "error";
  label?: string;
  percent: number;
  message: string;
  error?: string;
  result?: unknown;
}
