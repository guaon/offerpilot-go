import * as pdfjs from '/vendor/pdfjs/build/pdf.min.mjs';

pdfjs.GlobalWorkerOptions.workerSrc = '/vendor/pdfjs/build/pdf.worker.min.mjs';

const PDF_OPTIONS = {
  cMapUrl: '/vendor/pdfjs/cmaps/',
  cMapPacked: true,
  standardFontDataUrl: '/vendor/pdfjs/standard_fonts/',
  useSystemFonts: true,
};

export async function readPDF(file, options = {}) {
  const data = new Uint8Array(await file.arrayBuffer());
  const loadingTask = pdfjs.getDocument({ ...PDF_OPTIONS, data });
  try {
    const pdf = await loadingTask.promise;
    const pages = [];
    const pageImages = [];
    const renderLimit = Math.min(pdf.numPages, options.maxRenderedPages ?? 3);

    for (let index = 0; index < pdf.numPages; index++) {
      const page = await pdf.getPage(index + 1);
      const content = await page.getTextContent();
      pages.push(content.items
        .filter((item) => Object.prototype.hasOwnProperty.call(item, 'str'))
        .map((item) => `${item.str}${item.hasEOL ? '\n' : ''}`)
        .join(''));

      if (options.renderPages && index < renderLimit) {
        pageImages.push(await renderPage(page));
      }
    }

    const text = normalizePDFText(pages.join('\n\n'));
    if (!text) {
      throw new Error('无法提取 PDF 文本，文件可能是扫描件或已加密');
    }
    return { text, pages: pdf.numPages, pageImages, format: 'pdf' };
  } finally {
    await loadingTask.destroy();
  }
}

async function renderPage(page) {
  const baseViewport = page.getViewport({ scale: 1 });
  const scale = Math.min(1.5, 1100 / baseViewport.width);
  const viewport = page.getViewport({ scale });
  const canvas = document.createElement('canvas');
  canvas.width = Math.ceil(viewport.width);
  canvas.height = Math.ceil(viewport.height);
  const context = canvas.getContext('2d', { alpha: false });
  await page.render({ canvasContext: context, viewport, background: '#ffffff' }).promise;
  return canvas.toDataURL('image/jpeg', 0.82);
}

export function normalizePDFText(text) {
  return text
    .replace(/[\t\f\v ]+/g, ' ')
    .replace(/ *\n */g, '\n')
    .replace(/\n{3,}/g, '\n\n')
    .trim();
}
