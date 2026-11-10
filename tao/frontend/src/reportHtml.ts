// reportHtml.ts — construción do HTML do informe asinado (ReportBuilder.tsx, o
// alcance completo con selector de datas; e o atallo "quick" do DayModal, para unha
// soa sesión). Sacado a un módulo á parte para que os dous camiños produzan
// exactamente o mesmo documento sen duplicar a lóxica.
import { DayStatus, StudentProfile, Incident } from './types';
import { renderMarkdown } from './MarkdownPreview';
import { WorkFile } from '../bindings/tao/models';
import { INCIDENT_LABEL_KEYS, INCIDENT_SYMBOLS, INCIDENT_TYPES, countIncidents } from './incidents';

export interface ReportDayEntry {
  dateString: string;
  content: string;
  status: DayStatus;
  unitId?: number;
  unitTitle?: string;
  incidents?: Incident[];
}

// t: pásase a función de tradución tal cal (useTranslation().t) - esta función non é
// un hook, así que non pode chamalo ela mesma.
type T = (key: string, opts?: Record<string, any>) => string;

export function fmtLong(dateString: string): string {
  const d = new Date(dateString + 'T00:00:00');
  return d.toLocaleDateString('gl-ES', { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' });
}

export function fmtShort(dateString: string): string {
  const d = new Date(dateString + 'T00:00:00');
  return d.toLocaleDateString('gl-ES', { day: '2-digit', month: '2-digit', year: 'numeric' });
}

export function esc(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

// fmtSize: mesma escala que formatFileSize en App.tsx (non compartida, é un helper
// pequeno de máis para sacalo a un módulo á parte).
export function fmtSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

// O nome do asinante sácao Go do certificado; deixamos un placeholder que
// ExportSignedReport substitúe (debe coincidir con signerNamePlaceholder en Go).
const SIGNER = '@@ASINANTE@@';

export function buildReportHTML(params: {
  t: T;
  today: string;
  subjectName: string;
  academicYear: string;
  profile: StudentProfile;
  scopeDescription: string;
  entries: ReportDayEntry[];
  // ficheiros fonte subidos ao servidor por sesión (co seu checksum SHA-256) - ver
  // comentario en ListWorkFilesForReport (Go) e handleGenerate (ReportBuilder.tsx):
  // permítelle ao profesorado detectar coincidencias exactas de ficheiros entre
  // alumnado; a comparativa en si non se desenvolve por agora, só se deixa
  // constancia no informe.
  filesByDate: Record<string, WorkFile[]>;
  // aiSummary: resumo narrativo redactado coa axuda da IA (Malta, "Redacción por
  // IA" en ReportBuilder.tsx), xa revisado/editado polo usuario antes de asinar —
  // engádese coma introdución, o listado detallado de sesións séguese incluíndo
  // igual (ver deseño acordado: resumo + detalle, non un substitúe ao outro).
  aiSummary?: string;
}): string {
  const { t, today, subjectName, academicYear, profile, scopeDescription, entries, filesByDate, aiSummary } = params;

  const STATUS_LABELS: Record<DayStatus, string> = {
    lectivo: t('reportBuilder.statusLectivo'),
    exame: t('reportBuilder.statusExame'),
    folga: t('reportBuilder.statusFolga'),
    excursion: t('reportBuilder.statusExcursion'),
    ausencia: t('reportBuilder.statusAusencia'),
  };
  const signDate = fmtShort(today);
  const signStamp = new Date().toLocaleString('gl-ES', {
    day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit',
  });
  const sigText = t('reportBuilder.signedBy', { signer: SIGNER, date: signStamp });
  const udAbbrev = t('reportBuilder.udAbbrev');

  // Resumo de incidencias (positivo/negativo/expulsión/falta de orde) do alcance do
  // informe — punto 2 do pedido: deixar claro o número de cada tipo, non só onde
  // ocorreron. Omítese por completo se non hai ningunha (igual có resumo da IA).
  const incidentCounts = countIncidents(entries);
  const totalIncidents = INCIDENT_TYPES.reduce((sum, ty) => sum + incidentCounts[ty], 0);
  const incidentsSummaryHTML = totalIncidents === 0 ? '' : `
  <div class="incidents-summary">
    <div class="incidents-summary-title">${esc(t('reportBuilder.incidentsSummaryTitle'))}</div>
    <div class="incidents-summary-counts">
      ${INCIDENT_TYPES.map(ty => `<span class="badge incident-${ty}">${esc(INCIDENT_SYMBOLS[ty])} ${esc(t(INCIDENT_LABEL_KEYS[ty]))}: ${incidentCounts[ty]}</span>`).join('')}
    </div>
  </div>`;

  const practices = entries.map((e) => {
    const files = filesByDate[e.dateString] || [];
    const filesHTML = files.length === 0 ? '' : `
      <div class="files">
        <div class="files-title">${esc(t('reportBuilder.filesSectionTitle'))}</div>
        <table class="files-table">
          <thead><tr>
            <th>${esc(t('reportBuilder.fileNameLabel'))}</th>
            <th>${esc(t('reportBuilder.fileSizeLabel'))}</th>
            <th>${esc(t('reportBuilder.fileChecksumLabel'))}</th>
          </tr></thead>
          <tbody>
            ${files.map((f) => `<tr>
              <td>${esc(f.name)}</td>
              <td>${esc(fmtSize(f.size))}</td>
              <td class="checksum">${esc(f.checksum || '—')}</td>
            </tr>`).join('')}
          </tbody>
        </table>
      </div>`;
    return `
    <section class="practice">
      <div class="practice-head">
        <div class="date">${esc(fmtLong(e.dateString))}</div>
        <div class="meta">
          ${e.unitId ? `<span class="badge">${udAbbrev} ${e.unitId}${e.unitTitle ? ' · ' + esc(e.unitTitle) : ''}</span>` : ''}
          <span class="badge status">${esc(STATUS_LABELS[e.status])}</span>
          ${(e.incidents || []).map(inc => `<span class="badge incident-${inc.type}" title="${esc(inc.note || '')}">${esc(INCIDENT_SYMBOLS[inc.type])} ${esc(t(INCIDENT_LABEL_KEYS[inc.type]))}</span>`).join('')}
        </div>
      </div>
      <div class="md-body">${renderMarkdown(e.content)}</div>
      ${filesHTML}
    </section>`;
  }).join('\n');

  const emptyNote = entries.length === 0
    ? `<p class="empty">${esc(t('reportBuilder.noEntries'))}</p>` : '';

  return `<!doctype html>
<html lang="gl"><head><meta charset="utf-8">
<style>
  /* Marxe esquerda reducida (8mm) para reservar sitio á banda da firma; o contido
     desprázase á dereita con padding-left para non solaparse con ela. */
  @page { size: A4; margin: 16mm 16mm 16mm 8mm; }
  * { box-sizing: border-box; }
  body { font-family: -apple-system, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; color: #1c1917; margin: 0; padding-left: 18mm; font-size: 12px; line-height: 1.5; }

  /* Firma visible en vertical na marxe esquerda, DENTRO da zona imprimible (Chromium
     non pinta o que cae na marxe da páxina), ancorada na parte inferior e repetida en
     cada páxina. Con writing-mode: vertical-rl o eixe principal do flex é vertical,
     así que justify-content controla a posición vertical e align-items a horizontal.
     Como o elemento está rotado 180°, justify-content: flex-start (parte superior da
     disposición) queda visualmente na parte inferior; padding-top transládase, tras a
     rotación, a un oco sobre a base da páxina. */
  .page-signature {
    position: fixed; top: 0; bottom: 0; left: 0; width: 12mm;
    display: flex; align-items: center; justify-content: flex-start;
    padding-top: 8mm;
    writing-mode: vertical-rl; text-orientation: mixed; transform: rotate(180deg);
    font-size: 8px; letter-spacing: .3px; color: #57534e;
    border-right: 1px solid #d6d3d1;
  }

  .cover { break-after: page; padding-top: 8mm; }
  .cover h1 { font-size: 22px; margin: 0 0 4px; }
  .cover .subject { font-size: 15px; color: #44403c; margin-bottom: 24px; }
  .cover .scope { font-size: 16px; font-weight: 700; margin: 24px 0 8px; }
  .data-table { width: 100%; border-collapse: collapse; margin-top: 16px; }
  .data-table td { padding: 8px 10px; border: 1px solid #e7e5e4; }
  .data-table td.k { width: 34%; font-weight: 700; background: #fafaf9; color: #57534e; }
  .cert-line { margin-top: 24px; font-size: 11px; color: #57534e; display: flex; align-items: center; gap: 6px; }

  .ai-summary { break-inside: avoid-page; margin-bottom: 20px; padding: 12px 14px; background: #fafaf9; border: 1px solid #e7e5e4; border-radius: 6px; }
  .ai-summary-title { font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: .3px; color: #78716c; margin-bottom: 6px; }

  /* Resumo de incidencias (punto 2 do pedido): catro contadores, un por tipo. */
  .incidents-summary { break-inside: avoid-page; margin-bottom: 20px; padding: 12px 14px; background: #fafaf9; border: 1px solid #e7e5e4; border-radius: 6px; }
  .incidents-summary-title { font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: .3px; color: #78716c; margin-bottom: 6px; }
  .incidents-summary-counts { display: flex; gap: 6px; flex-wrap: wrap; }
  .badge.incident-positivo { background: #10b981; color: #fff; border-color: #10b981; }
  .badge.incident-negativo { background: #f43f5e; color: #fff; border-color: #f43f5e; }
  .badge.incident-expulsion { background: #1c1917; color: #fff; border-color: #1c1917; }
  .badge.incident-falta { background: #f59e0b; color: #fff; border-color: #f59e0b; }

  .practice { break-inside: avoid-page; margin-bottom: 18px; padding-bottom: 14px; border-bottom: 1px solid #e7e5e4; }
  .practice-head { display: flex; justify-content: space-between; align-items: baseline; gap: 12px; margin-bottom: 8px; }
  .practice-head .date { font-weight: 700; text-transform: capitalize; }
  .practice-head .meta { display: flex; gap: 6px; flex-wrap: wrap; }
  .badge { font-size: 9px; font-weight: 700; padding: 2px 7px; border-radius: 999px; background: #f5f5f4; color: #44403c; border: 1px solid #e7e5e4; white-space: nowrap; }
  .badge.status { background: #1c1917; color: #fff; border-color: #1c1917; }

  .md-body img { max-width: 100%; height: auto; border-radius: 6px; }
  .md-body h1, .md-body h2, .md-body h3 { margin: 12px 0 6px; }
  .md-body p { margin: 6px 0; }
  .md-body ul, .md-body ol { margin: 6px 0 6px 18px; }
  .md-body pre { background: #f5f5f4; padding: 10px; border-radius: 6px; overflow-x: auto; }
  .md-body code { background: #f5f5f4; padding: 1px 4px; border-radius: 4px; font-size: 92%; }
  .md-body table { border-collapse: collapse; }
  .md-body td, .md-body th { border: 1px solid #e7e5e4; padding: 4px 8px; }
  .empty { color: #78716c; font-style: italic; }

  /* Ficheiros fonte subidos ao servidor, co seu checksum: ver comentario en
     buildReportHTML/filesByDate máis arriba. */
  .files { margin-top: 10px; }
  .files-title { font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: .3px; color: #78716c; margin-bottom: 4px; }
  .files-table { width: 100%; border-collapse: collapse; font-size: 10px; }
  .files-table th, .files-table td { border: 1px solid #e7e5e4; padding: 3px 6px; text-align: left; }
  .files-table th { background: #fafaf9; color: #57534e; font-weight: 700; }
  .files-table td.checksum { font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace; font-size: 9px; word-break: break-all; }
</style></head>
<body>
  <div class="page-signature">${esc(sigText)}</div>

  <div class="cover">
    <h1>${esc(t('reportBuilder.reportTitle'))}</h1>
    <div class="subject">${esc(subjectName)} · ${esc(academicYear)}</div>
    <table class="data-table">
      <tr><td class="k">${esc(t('reportBuilder.studentLabel'))}</td><td>${SIGNER}</td></tr>
      ${profile.group ? `<tr><td class="k">${esc(t('reportBuilder.groupLabel'))}</td><td>${esc(profile.group)}</td></tr>` : ''}
      <tr><td class="k">${esc(t('reportBuilder.usernameLabel'))}</td><td>${esc(profile.username)}</td></tr>
      <tr><td class="k">${esc(t('reportBuilder.reportDateLabel'))}</td><td>${esc(signDate)}</td></tr>
    </table>
    <div class="scope">${esc(scopeDescription)}</div>
    <div class="cert-line">${esc(t('reportBuilder.certLine'))}</div>
  </div>

  ${incidentsSummaryHTML}
  ${aiSummary ? `<div class="ai-summary">
    <div class="ai-summary-title">${esc(t('reportBuilder.aiSummaryTitle'))}</div>
    <div class="md-body">${renderMarkdown(aiSummary)}</div>
  </div>` : ''}
  ${emptyNote}
  ${practices}
</body></html>`;
}
