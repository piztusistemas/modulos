import { useMemo } from 'react';
import { marked } from 'marked';
import markedKatex from 'marked-katex-extension';
import DOMPurify from 'dompurify';

// output:'mathml' (non 'html', o por defecto de KaTeX) a propósito: o informe
// final (ReportBuilder.tsx) chega a renderHTMLToPDF (tao/report.go), que usa
// Chromium headless con page.SetContent - sen orixe de ficheiro nin servidor
// detrás, así que calquera CSS con @font-face:url(fonts/...) (o que fai
// katex.min.css) rompería silenciosamente. MathML puro non precisa CSS nin
// fontes propias - Chromium e WebKitGTK (o motor de Wails en Linux, usado no
// preview en vivo) renderízano de forma nativa.
marked.use(markedKatex({ throwOnError: false, output: 'mathml' }));
marked.setOptions({ gfm: true, breaks: true });

// renderMarkdown converts a Markdown string into sanitized HTML. It is shared by
// the live editor preview and the printed report so that what the student sees is
// exactly what gets signed.
export function renderMarkdown(md: string): string {
  const rawHtml = marked.parse(md || '', { async: false }) as string;
  return DOMPurify.sanitize(rawHtml, {
    ADD_ATTR: ['style', 'align', 'target'],
    // data: image URIs (pasted/embedded images) are allowed by default for <img>.
  });
}

export default function MarkdownPreview({ content, className }: { content: string; className?: string }) {
  const html = useMemo(() => renderMarkdown(content), [content]);
  return (
    <div
      className={`md-body ${className || ''}`}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}
