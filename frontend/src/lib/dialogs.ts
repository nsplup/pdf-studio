import { Dialogs } from "@wailsio/runtime";

const pdfFilter = [{ DisplayName: "PDF 文档 (*.pdf)", Pattern: "*.pdf" }];
const imgFilter = [
  { DisplayName: "图片 (*.jpg;*.jpeg;*.png;*.gif;*.tif;*.tiff;*.bmp;*.webp)", Pattern: "*.jpg;*.jpeg;*.png;*.gif;*.tif;*.tiff;*.bmp;*.webp" },
];

export async function pickPDF(multi = false): Promise<string[]> {
  const r = await Dialogs.OpenFile({
    CanChooseFiles: true,
    AllowsMultipleSelection: multi,
    Filters: pdfFilter,
    Title: multi ? "选择 PDF 文件" : "打开 PDF 文件",
  });
  if (!r) return [];
  return Array.isArray(r) ? r : [r];
}

export async function pickImages(multi = true): Promise<string[]> {
  const r = await Dialogs.OpenFile({
    CanChooseFiles: true,
    AllowsMultipleSelection: multi,
    Filters: imgFilter,
    Title: "选择图片",
  });
  if (!r) return [];
  return Array.isArray(r) ? r : [r];
}

export async function pickSavePDF(defaultName: string): Promise<string | null> {
  const r = await Dialogs.SaveFile({
    CanChooseFiles: true,
    CanCreateDirectories: true,
    Filename: defaultName,
    Filters: pdfFilter,
    Title: "保存为",
  });
  return r || null;
}

export async function pickSaveAny(defaultName: string, title: string): Promise<string | null> {
  const r = await Dialogs.SaveFile({
    CanChooseFiles: true,
    CanCreateDirectories: true,
    Filename: defaultName,
    Title: title,
  });
  return r || null;
}

export async function pickImport(multi = true): Promise<string[]> {
  const r = await Dialogs.OpenFile({
    CanChooseFiles: true,
    AllowsMultipleSelection: multi,
    Filters: [
      { DisplayName: "PDF 与图片", Pattern: "*.pdf;*.jpg;*.jpeg;*.png;*.gif;*.tif;*.tiff;*.bmp;*.webp" },
      ...pdfFilter,
      ...imgFilter,
    ],
    Title: "导入 PDF / 图片",
  });
  if (!r) return [];
  return Array.isArray(r) ? r : [r];
}
