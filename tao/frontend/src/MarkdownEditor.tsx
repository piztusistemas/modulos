import { useRef, useMemo, useState } from 'react';
import { Bold, Italic, Heading, List, ListOrdered, Image as ImageIcon, AlignLeft, AlignCenter, AlignRight, Code, Link as LinkIcon, Maximize2, Minimize2, NotebookPen } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import MarkdownPreview from './MarkdownPreview';
import { fold, unfold, imagesWeight, formatWeight, isBlockedImageFormat, MAX_IMAGE_DATA_URI } from './markdownImages';

interface Props {
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
  // fill makes the editor stretch to fill its parent's height (used in the
  // full-screen day editor); otherwise it falls back to a fixed minimum height.
  fill?: boolean;
  // detachable amosa un botón "Ampliar" na barra de ferramentas que leva este mesmo
  // editor (mesmo value/onChange - nada que sincronizar) a un panel grande por riba de
  // todo o resto, coa súa propia barra de ferramentas, para traballar con máis espazo;
  // "Acoplar" devólveo ao seu sitio. É unha decisión de sesión, non persiste.
  //
  // Non é unha xanela de sistema á parte (arrastrable a outro monitor): Tao vai sobre
  // Wails v2, que só permite unha xanela nativa por proceso — comprobado empiricamente
  // que window.open() non crea ningunha (WebKitGTK devolve null; Wails v2 non conecta o
  // sinal "create" que faría falla). Yang si ten xanela separada de verdade porque foi
  // migrado a Wails v3 (varias WebviewWindow no mesmo proceso, ver yang/xanelaresultado.go)
  // — replicar ese mecanismo en Tao suporía a mesma migración. Este panel ampliado é o
  // equivalente acadable sen ela.
  detachable?: boolean;
  // title: cabeceira do panel ampliado (por defecto un texto xenérico).
  title?: string;
}

// MarkdownEditor is a two-pane (source + live preview) editor with a formatting
// toolbar. It stores plain Markdown; images are embedded as data: URIs so they
// travel with the backup and end up in the signed PDF.
//
// The textarea works entirely in "display" text, where each image is folded down to a
// short (imaxe:N) mark (see markdownImages.ts): a pasted photo is tens of thousands of
// characters, and leaving them inline buries what the student wrote. Only the two edges
// convert — what is read from `value` and what is handed back to `onChange` — so what
// gets stored, previewed and signed is unchanged.
export default function MarkdownEditor({ value, onChange, disabled, fill, detachable, title }: Props) {
  const { t } = useTranslation();
  const taRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const [expanded, setExpanded] = useState(false);
  // imageError: por que se rexeitou a última imaxe (formato ou tamaño); bórrase coa seguinte que entre.
  const [imageError, setImageError] = useState('');

  const { display, images } = useMemo(() => fold(value), [value]);

  // emit turns edited display text back into the real Markdown.
  const emit = (nextDisplay: string) => onChange(unfold(nextDisplay, images));

  // wrapSelection surrounds the current selection with before/after, or inserts a
  // placeholder when nothing is selected.
  const wrapSelection = (before: string, after: string, placeholder = '') => {
    const ta = taRef.current;
    if (!ta) return;
    const start = ta.selectionStart;
    const end = ta.selectionEnd;
    const selected = display.slice(start, end) || placeholder;
    emit(display.slice(0, start) + before + selected + after + display.slice(end));
    // Restore a sensible caret/selection after React re-renders.
    requestAnimationFrame(() => {
      ta.focus();
      ta.selectionStart = start + before.length;
      ta.selectionEnd = start + before.length + selected.length;
    });
  };

  // insertBlock inserts a block of text at the caret on its own lines.
  const insertBlock = (text: string) => {
    const ta = taRef.current;
    if (!ta) return;
    const start = ta.selectionStart;
    const prefix = display.slice(0, start);
    const needsNl = prefix.length > 0 && !prefix.endsWith('\n') ? '\n' : '';
    emit(prefix + needsNl + text + display.slice(start));
    requestAnimationFrame(() => {
      ta.focus();
      const pos = start + needsNl.length + text.length;
      ta.selectionStart = ta.selectionEnd = pos;
    });
  };

  const alignBlock = (align: 'left' | 'center' | 'right') => {
    const ta = taRef.current;
    if (!ta) return;
    const start = ta.selectionStart;
    const end = ta.selectionEnd;
    const selected = display.slice(start, end) || t('markdownEditor.textoAlineado');
    const block = `\n<div style="text-align:${align}">\n\n${selected}\n\n</div>\n`;
    emit(display.slice(0, start) + block + display.slice(end));
    requestAnimationFrame(() => ta.focus());
  };

  // handleImageFile drops the whole data URI in at the caret. It needs no folding of
  // its own: unfold leaves a real data URI untouched (it is not a mark), so it reaches
  // `value`, and the next render folds it down to (imaxe:N) like any other image.
  //
  // PSD/XCF rexéitanse antes de ler nada; o tamaño compróbase sobre o data URI xa en
  // base64, que é o que de verdade se garda (~4/3 do ficheiro orixinal).
  const handleImageFile = (file: File) => {
    if (isBlockedImageFormat(file)) {
      setImageError(t('markdownEditor.erroFormatoImaxe', { name: file.name }));
      return;
    }
    const reader = new FileReader();
    reader.onload = () => {
      const dataUri = reader.result as string;
      if (dataUri.length > MAX_IMAGE_DATA_URI) {
        setImageError(t('markdownEditor.erroTamanoImaxe', { name: file.name, size: formatWeight(dataUri.length), max: formatWeight(MAX_IMAGE_DATA_URI) }));
        return;
      }
      setImageError('');
      insertBlock(`![${file.name}](${dataUri})\n`);
    };
    reader.readAsDataURL(file);
  };

  const onPaste = (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const item = Array.from(e.clipboardData.items).find(i => i.type.startsWith('image/'));
    if (item) {
      const file = item.getAsFile();
      if (file) {
        e.preventDefault();
        handleImageFile(file);
      }
    }
  };

  const Btn = ({ onClick, title, children }: { onClick: () => void; title: string; children: React.ReactNode }) => (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={title}
      className="p-2 rounded-lg text-stone-600 hover:bg-stone-200 disabled:opacity-40 transition-colors"
    >
      {children}
    </button>
  );

  // effectiveFill: dentro do panel ampliado sempre se estira a ocupar todo o alto,
  // independentemente do `fill` que pedise quen chama.
  const effectiveFill = fill || expanded;

  const toolbar = (
    <div className="flex flex-wrap items-center gap-0.5 p-1 bg-stone-100 rounded-lg border border-stone-200">
      <Btn onClick={() => wrapSelection('**', '**', t('markdownEditor.placeholderNegrina'))} title={t('markdownEditor.btnNegrina')}><Bold className="w-4 h-4" /></Btn>
      <Btn onClick={() => wrapSelection('*', '*', t('markdownEditor.placeholderCursiva'))} title={t('markdownEditor.btnCursiva')}><Italic className="w-4 h-4" /></Btn>
      <Btn onClick={() => insertBlock(t('markdownEditor.blockTitulo'))} title={t('markdownEditor.btnTitulo')}><Heading className="w-4 h-4" /></Btn>
      <Btn onClick={() => wrapSelection('`', '`', t('markdownEditor.placeholderCodigo'))} title={t('markdownEditor.btnCodigo')}><Code className="w-4 h-4" /></Btn>
      <span className="w-px h-5 bg-stone-300 mx-1" />
      <Btn onClick={() => insertBlock(t('markdownEditor.blockLista'))} title={t('markdownEditor.btnLista')}><List className="w-4 h-4" /></Btn>
      <Btn onClick={() => insertBlock(t('markdownEditor.blockListaNumerada'))} title={t('markdownEditor.btnListaNumerada')}><ListOrdered className="w-4 h-4" /></Btn>
      <Btn onClick={() => wrapSelection('[', '](https://)', t('markdownEditor.placeholderLigazon'))} title={t('markdownEditor.btnLigazon')}><LinkIcon className="w-4 h-4" /></Btn>
      <span className="w-px h-5 bg-stone-300 mx-1" />
      <Btn onClick={() => alignBlock('left')} title={t('markdownEditor.btnAlinarEsquerda')}><AlignLeft className="w-4 h-4" /></Btn>
      <Btn onClick={() => alignBlock('center')} title={t('markdownEditor.btnCentrar')}><AlignCenter className="w-4 h-4" /></Btn>
      <Btn onClick={() => alignBlock('right')} title={t('markdownEditor.btnAlinarDereita')}><AlignRight className="w-4 h-4" /></Btn>
      <span className="w-px h-5 bg-stone-300 mx-1" />
      <Btn onClick={() => fileRef.current?.click()} title={t('markdownEditor.btnInserirImaxe')}><ImageIcon className="w-4 h-4" /></Btn>
      <input
        ref={fileRef}
        type="file"
        accept="image/*"
        className="hidden"
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) handleImageFile(f);
          e.target.value = '';
        }}
      />
      {/* Ampliar: só se non está xa ampliado (dentro do panel non ten sentido volver
          premelo - "Acoplar" vive na cabeceira dese panel, ver máis abaixo). */}
      {detachable && !expanded && (
        <>
          <span className="w-px h-5 bg-stone-300 mx-1" />
          <Btn onClick={() => setExpanded(true)} title={t('markdownEditor.btnAmpliar')}><Maximize2 className="w-4 h-4" /></Btn>
        </>
      )}
    </div>
  );

  const editorGrid = (
    <div className={`grid grid-cols-1 lg:grid-cols-2 gap-3 ${effectiveFill ? 'flex-1 min-h-[260px]' : ''}`}>
      <div className={`flex flex-col gap-1 ${effectiveFill ? 'min-h-0' : ''}`}>
        <div className="flex items-center justify-between gap-2">
          <span className="text-xs font-bold text-stone-500 uppercase tracking-wider">{t('markdownEditor.labelMarkdown')}</span>
          {images.length > 0 && (
            <span
              className="text-xs text-stone-400"
              title={t('markdownEditor.tooltipImaxes')}
            >
              {t('markdownEditor.numImaxes', { count: images.length })} · {formatWeight(imagesWeight(images))}
            </span>
          )}
        </div>
        {imageError && (
          <div className="text-xs text-red-700 bg-red-50 border border-red-200 rounded-lg px-2 py-1">{imageError}</div>
        )}
        <textarea
          ref={taRef}
          value={display}
          disabled={disabled}
          onChange={(e) => emit(e.target.value)}
          onPaste={onPaste}
          placeholder={t('markdownEditor.placeholderTextarea')}
          className={`w-full p-3 bg-white border border-stone-300 rounded-lg shadow-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none font-mono text-sm leading-relaxed disabled:bg-stone-50 ${effectiveFill ? 'flex-1 min-h-[240px] resize-none' : 'min-h-[320px] resize-y'}`}
        />
      </div>
      <div className={`flex flex-col gap-1 ${effectiveFill ? 'min-h-0' : ''}`}>
        <span className="text-xs font-bold text-stone-500 uppercase tracking-wider">{t('markdownEditor.labelVistaPrevia')}</span>
        <div className={`w-full p-4 bg-white border border-stone-200 rounded-lg shadow-sm overflow-auto ${effectiveFill ? 'flex-1 min-h-[240px]' : 'min-h-[320px]'}`}>
          <MarkdownPreview content={value} />
        </div>
      </div>
    </div>
  );

  return (
    <>
      <div className={`flex flex-col gap-2 ${fill ? 'h-full min-h-[320px]' : ''}`}>
        {expanded ? (
          // Mentres está ampliado, non se duplica o editor aquí (evita dúas
          // superficies editables á vez, ver panel máis abaixo) - só un aviso co
          // camiño de volta, por se o profesorado/alumnado perde de vista o panel.
          <div className={`flex-1 min-h-[120px] flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-stone-300 bg-stone-50 text-sm text-stone-500 p-4 text-center`}>
            <NotebookPen className="w-5 h-5 text-stone-400" />
            <span>{t('markdownEditor.ampliadoAviso')}</span>
            <button type="button" onClick={() => setExpanded(false)} className="text-stone-700 font-medium underline hover:no-underline">
              {t('markdownEditor.btnAcoplar')}
            </button>
          </div>
        ) : (
          <>
            {toolbar}
            {editorGrid}
          </>
        )}
      </div>

      {expanded && (
        <div className="fixed inset-4 md:inset-10 z-50 bg-white rounded-2xl shadow-2xl ring-1 ring-stone-900/10 flex flex-col overflow-hidden">
          <div className="p-3 border-b border-stone-200 flex items-center justify-between bg-stone-50 shrink-0">
            <h3 className="font-bold text-base text-stone-900 flex items-center gap-2">
              <NotebookPen className="w-4 h-4 text-stone-500" /> {title || t('markdownEditor.tituloPanelAmpliado')}
            </h3>
            <button
              onClick={() => setExpanded(false)}
              className="p-2 text-stone-400 hover:text-stone-600 rounded-lg hover:bg-stone-200 transition-colors"
              title={t('markdownEditor.btnAcoplar')}
            >
              <Minimize2 className="w-5 h-5" />
            </button>
          </div>
          <div className="p-4 flex-1 min-h-0 flex flex-col gap-2">
            {toolbar}
            {editorGrid}
          </div>
        </div>
      )}
    </>
  );
}
