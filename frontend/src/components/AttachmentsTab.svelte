<script lang="ts">
  import { MetaService, type AttachmentInfo } from "../bindings/services";
  import { Eraser } from "lucide-svelte";
  import { Dialogs } from "@wailsio/runtime";
  import { currentDoc, notify } from "../stores";
  import * as UI from "./ui";
  import {
    Paperclip,
    Loader2,
    Trash2,
    Plus,
    FileQuestion,
  } from "lucide-svelte";

  let doc = $derived($currentDoc);
  let loadedID = $state("");
  let attachments = $state<AttachmentInfo[]>([]);
  let busy = $state(false);

  $effect(() => {
    void doc;
    if (doc && doc.id !== loadedID) {
      const id = doc.id;
      loadedID = id;
      load(id);
    }
  });

  async function load(id: string) {
    busy = true;
    try {
      attachments = (await MetaService.ListDocAttachments(id)) ?? [];
    } catch (e: any) {
      attachments = [];
      notify("err", `读取附件失败：${e?.message ?? e}`);
    } finally {
      busy = false;
    }
  }

  /** 清空全部附件（待全局保存时生效） */
  async function clearAll() {
    if (!doc || busy) return;
    busy = true;
    try {
      attachments = (await MetaService.ClearDocAttachments(doc.id)) ?? [];
      notify("ok", "附件已清空");
    } catch (e: any) {
      notify("err", `清空失败：${e?.message ?? e}`);
    } finally {
      busy = false;
    }
  }

  /** 供 App 调用：导入放置合并附件后刷新 */
  export async function reload() {
    if (loadedID) await load(loadedID);
  }

  async function addFiles() {
    if (!doc || busy) return;
    const r = await Dialogs.OpenFile({
      CanChooseFiles: true,
      AllowsMultipleSelection: true,
      Title: "选择要添加的附件",
    });
    const paths = Array.isArray(r) ? r : r ? [r] : [];
    if (!paths.length) return;
    busy = true;
    try {
      attachments = (await MetaService.AddDocAttachments(doc.id, paths)) ?? [];
      notify("ok", `已添加 ${paths.length} 个附件`);
    } catch (e: any) {
      notify("err", `添加附件失败：${e?.message ?? e}`);
    } finally {
      busy = false;
    }
  }

  /** 移除单个附件（待全局保存时生效） */
  async function removeOne(name: string) {
    if (!doc || busy) return;
    busy = true;
    try {
      attachments =
        (await MetaService.RemoveDocAttachments(doc.id, [name])) ?? [];
      notify("ok", "已移除该附件");
    } catch (e: any) {
      notify("err", `移除失败：${e?.message ?? e}`);
    } finally {
      busy = false;
    }
  }
</script>

{#if doc}
  <div class="px-4 py-4">
    <UI.Card>
      <UI.CardHeader>
        <UI.CardTitle class="flex items-center gap-2">
          <Paperclip class="h-4 w-4 text-primary" /> 附件
        </UI.CardTitle>
      </UI.CardHeader>
      <UI.CardContent>
        {#if busy && !loadedID}
          <div class="empty">
            <Loader2 class="h-5 w-5 animate-spin" /> 读取附件…
          </div>
        {:else if attachments.length}
          <div class="flex flex-col gap-1.5">
            {#each attachments as a (a.fileName)}
              <div
                class="attach-row flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm"
              >
                <Paperclip class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                <span class="flex-1 truncate">{a.fileName}</span>
                {#if a.description}<span class="text-xs text-muted-foreground"
                    >{a.description}</span
                  >{/if}
                <button
                  class="attach-del"
                  title="移除该附件"
                  onclick={() => removeOne(a.fileName)}
                >
                  <Trash2 class="h-3.5 w-3.5" />
                </button>
              </div>
            {/each}
          </div>
        {:else}
          <div class="empty-sm">暂无附件</div>
        {/if}
      </UI.CardContent>
      <UI.CardFooter class="gap-3">
        <UI.Button variant="outline" onclick={addFiles} disabled={busy}>
          <Plus class="h-4 w-4" /> 添加附件
        </UI.Button>
        {#if attachments.length}
          <UI.Button
            variant="outline"
            class="text-destructive hover:text-destructive"
            onclick={clearAll}
            disabled={busy}
          >
            <Eraser class="h-4 w-4" /> 清空附件
          </UI.Button>
        {/if}
      </UI.CardFooter>
    </UI.Card>
  </div>
{:else}
  <div
    class="flex h-full flex-col items-center justify-center gap-3 text-muted-foreground"
  >
    <FileQuestion class="h-12 w-12" />
    <p class="text-lg">打开 PDF 开始编辑</p>
  </div>
{/if}

<style>
  .attach-row {
    transition: background-color 0.12s;
  }
  .attach-row:hover {
    background: hsl(var(--accent) / 0.6);
  }
  .attach-del {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    border-radius: 6px;
    color: hsl(var(--destructive));
    opacity: 0;
    transition: opacity 0.12s;
  }
  .attach-row:hover .attach-del {
    opacity: 1;
  }
  .attach-del:hover {
    background: hsl(var(--destructive) / 0.12);
  }
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
    padding: 20px 0;
    font-size: 13px;
    color: hsl(var(--muted-foreground));
  }
</style>
