import { useMemo, useState, useRef, useEffect, useCallback } from 'react';
import { X, FileSignature, Loader2, ShieldCheck, CheckCircle2, FolderOpen, GripHorizontal, CalendarDays, Sparkles, RotateCcw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Subject, StudentProfile } from './types';
import { generateMonthlySchedule } from './data';
import { ExportSignedReport, ListWorkFilesForReport, OpenFile, AbrirMaltaRedaccion } from '../bindings/tao/app';
import { Events } from '@wailsio/runtime';
import { WorkFile } from '../bindings/tao/models';
import { buildReportHTML, fmtLong, fmtShort, ReportDayEntry } from './reportHtml';

type ScopeType = 'session' | 'week' | 'range' | 'unit' | 'evaluation';
type DayEntry = ReportDayEntry;

// mondayOf returns the Monday (YYYY-MM-DD) of the week containing dateString.
function mondayOf(dateString: string): string {
  const d = new Date(dateString + 'T00:00:00');
  const dow = (d.getDay() + 6) % 7; // 0 = Monday
  d.setDate(d.getDate() - dow);
  return d.toISOString().split('T')[0];
}
function addDays(dateString: string, n: number): string {
  const d = new Date(dateString + 'T00:00:00');
  d.setDate(d.getDate() + n);
  return d.toISOString().split('T')[0];
}

export default function ReportBuilder({ subject, profile, title, onClose, onRequestPickDate, onCancelPickDate }: { subject: Subject; profile: StudentProfile; title?: string; onClose: () => void; onRequestPickDate: (onPick: (dateString: string) => void) => void; onCancelPickDate: () => void }) {
  const { t } = useTranslation();
  const displayTitle = title || t('reportBuilder.title');
  const [scope, setScope] = useState<ScopeType>('week');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // savedPath: o informe publícase no servidor (ver ExportSignedReport, Go), pero
  // tamén garda unha copia local de usar-e-tirar só para o botón "Abrir" — non é
  // "onde quedou gardado", é onde se pode ver decontado o que se acaba de xerar.
  const [savedPath, setSavedPath] = useState('');

  // ── Redacción por IA (Malta) ──────────────────────────────────────────
  // iaRedaccion activa o check; iaPrompt é a petición personalizada optativa
  // (só visible cando o check está activo). iaDraft garda o texto que devolve
  // Malta, EDITABLE, antes de asinar nada — nunca se asina directamente o que
  // escribe a IA (ver decisión de deseño: revisión previa obrigatoria).
  const [iaRedaccion, setIaRedaccion] = useState(false);
  const [iaPrompt, setIaPrompt] = useState('');
  const [iaBusy, setIaBusy] = useState(false);
  const [iaError, setIaError] = useState('');
  const [iaDraft, setIaDraft] = useState<string | null>(null);
  // iaRequestIdRef (non state: non precisa repintar) correlaciona o evento
  // "tao:malta-redaccion" coa última petición feita por ESTA xanela — pode
  // haber máis dun informe asinado aberto á vez.
  const iaRequestIdRef = useRef<string | null>(null);

  useEffect(() => {
    const off = Events.On('tao:malta-redaccion', (e) => {
      const payload = e.data;
      if (!payload || payload.requestId !== iaRequestIdRef.current) return;
      setIaBusy(false);
      if (!payload.ok) {
        setIaError(payload.erro || t('reportBuilder.iaRedaccionError'));
        return;
      }
      setIaError('');
      setIaDraft(payload.texto || '');
    });
    return () => { off(); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Arrastre da xanela pola súa cabeceira: o alumnado precisa ver o calendario de
  // detrás mentres escolle o alcance do informe. `offset` desprázaa desde o centro.
  const panelRef = useRef<HTMLDivElement>(null);
  const [offset, setOffset] = useState({ x: 0, y: 0 });
  const dragRef = useRef<{ px: number; py: number; ox: number; oy: number } | null>(null);

  // clamp mantén a xanela enteira dentro da vista: se puidese saír, podería perderse a
  // cabeceira e xa non habería de onde agarrala para traela de volta.
  const clamp = useCallback((x: number, y: number) => {
    const el = panelRef.current;
    if (!el) return { x, y };
    const { width, height } = el.getBoundingClientRect();
    const limitX = Math.max(0, (window.innerWidth - width) / 2);
    const limitY = Math.max(0, (window.innerHeight - height) / 2);
    return {
      x: Math.min(limitX, Math.max(-limitX, x)),
      y: Math.min(limitY, Math.max(-limitY, y)),
    };
  }, []);

  const onHeaderPointerDown = (e: React.PointerEvent) => {
    // O botón de pechar (e calquera outro da cabeceira) segue a ser clicable.
    if ((e.target as HTMLElement).closest('button')) return;
    dragRef.current = { px: e.clientX, py: e.clientY, ox: offset.x, oy: offset.y };
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  };

  const onHeaderPointerMove = (e: React.PointerEvent) => {
    const d = dragRef.current;
    if (!d) return;
    setOffset(clamp(d.ox + e.clientX - d.px, d.oy + e.clientY - d.py));
  };

  const endDrag = (e: React.PointerEvent) => {
    if (!dragRef.current) return;
    dragRef.current = null;
    (e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId);
  };

  // Ao redimensionar a xanela da app, o que antes estaba dentro pode quedar fóra.
  useEffect(() => {
    const onResize = () => setOffset((o) => clamp(o.x, o.y));
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, [clamp]);

  const today = new Date().toISOString().split('T')[0];
  const [sessionDate, setSessionDate] = useState(today);
  const [weekDate, setWeekDate] = useState(today);
  const [fromDate, setFromDate] = useState(subject.config.startDate);
  const [toDate, setToDate] = useState(today);
  const [unitId, setUnitId] = useState<number>(subject.units[0]?.id ?? 0);
  const [evalIndex, setEvalIndex] = useState(0);

  // Escoller a data pinchando no calendario xeral (detrás desta xanela, visible a
  // propósito - ver comentario de `offset` enriba) no canto de escribila á man. App
  // gárdaas nun único `datePicker` (só pode haber un pick pendente á vez), así que
  // aquí só facemos de ponte: activámolo con onRequestPickDate e ao chegar a data
  // aplicámola ao campo que a pediu.
  const [pickingField, setPickingField] = useState<null | 'sessionDate' | 'weekDate' | 'fromDate' | 'toDate'>(null);
  const dateFieldSetters: Record<'sessionDate' | 'weekDate' | 'fromDate' | 'toDate', (d: string) => void> = {
    sessionDate: setSessionDate, weekDate: setWeekDate, fromDate: setFromDate, toDate: setToDate,
  };
  const togglePick = (field: 'sessionDate' | 'weekDate' | 'fromDate' | 'toDate') => {
    if (pickingField === field) {
      onCancelPickDate();
      setPickingField(null);
      return;
    }
    setPickingField(field);
    onRequestPickDate((d) => {
      dateFieldSetters[field](d);
      setPickingField(null);
    });
  };
  // Esc cancela un pick pendente; e se a propia xanela do informe se pecha con un pick
  // aínda activo, hai que avisar a App tamén (senón o seguinte clic no calendario
  // desaparecería sen abrir o día, capturado por un picker xa sen dono).
  useEffect(() => {
    if (!pickingField) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') { onCancelPickDate(); setPickingField(null); } };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pickingField]);
  // Só ao DESMONTAR (deps [], non [onCancelPickDate]): App pásanos unha función nova en
  // cada renderizado seu (arrow inline), así que se a metésemos nas deps isto
  // cancelaría un pick en curso cada vez que App se volvese renderizar por calquera
  // outro motivo (p.ex. o poll de materias cada 8s) - non só ao pechar esta xanela. O
  // ref garante que se chame sempre á versión máis recente sen ese problema.
  const onCancelPickDateRef = useRef(onCancelPickDate);
  onCancelPickDateRef.current = onCancelPickDate;
  useEffect(() => () => onCancelPickDateRef.current(), []);

  const evaluations = subject.config.evaluations || [];

  // Map every scheduled date to its programmed unit, then collect logged days.
  const allEntries = useMemo<DayEntry[]>(() => {
    const { months } = generateMonthlySchedule(subject.config, subject.units, subject.logs);
    const unitByDate: Record<string, { id: number; title: string } | undefined> = {};
    for (const m of months) {
      for (const d of m.days) {
        if (d.unit) unitByDate[d.dateString] = { id: d.unit.id, title: d.unit.title };
      }
    }
    return Object.values(subject.logs)
      // Un día conta para o informe se ten contido escrito OU polo menos unha
      // incidencia — senón un positivo/negativo dado un día sen texto rexistrado
      // quedaría fóra do resumo de incidencias (punto 2 do pedido).
      .filter((l) => (l.content && l.content.trim()) || (l.incidents && l.incidents.length > 0))
      .map((l) => ({
        dateString: l.date,
        content: l.content,
        status: l.status,
        unitId: unitByDate[l.date]?.id,
        unitTitle: unitByDate[l.date]?.title,
        incidents: l.incidents,
      }))
      .sort((a, b) => a.dateString.localeCompare(b.dateString));
  }, [subject]);

  const filtered = useMemo<DayEntry[]>(() => {
    switch (scope) {
      case 'session':
        return allEntries.filter((e) => e.dateString === sessionDate);
      case 'week': {
        const start = mondayOf(weekDate);
        const end = addDays(start, 6);
        return allEntries.filter((e) => e.dateString >= start && e.dateString <= end);
      }
      case 'range':
        return allEntries.filter((e) => e.dateString >= fromDate && e.dateString <= toDate);
      case 'unit':
        return allEntries.filter((e) => e.unitId === unitId);
      case 'evaluation': {
        const ev = evaluations[evalIndex];
        if (!ev) return [];
        return allEntries.filter((e) => e.dateString >= ev.startDate && e.dateString <= ev.endDate);
      }
    }
  }, [scope, allEntries, sessionDate, weekDate, fromDate, toDate, unitId, evalIndex, evaluations]);

  const scopeDescription = (): string => {
    switch (scope) {
      case 'session': return t('reportBuilder.scopeSession', { date: fmtShort(sessionDate) });
      case 'week': { const s = mondayOf(weekDate); return t('reportBuilder.scopeWeek', { start: fmtShort(s), end: fmtShort(addDays(s, 6)) }); }
      case 'range': return t('reportBuilder.scopeRange', { from: fmtShort(fromDate), to: fmtShort(toDate) });
      case 'unit': { const u = subject.units.find((x) => x.id === unitId); return t('reportBuilder.scopeUnit', { id: unitId, title: u?.title || '' }); }
      case 'evaluation': return evaluations[evalIndex]?.name || t('reportBuilder.evaluationLabel');
    }
  };

  // handleRedactar pide a Malta un borrador do resumo narrativo (ver
  // redaccion_launch.go en Go) — non asina nin garda nada, só amosa o
  // borrador editable (iaDraft) para que o usuario o revise antes.
  const handleRedactar = async () => {
    setIaError('');
    setIaDraft(null);
    if (filtered.length === 0) { setIaError(t('reportBuilder.noEntries')); return; }
    setIaBusy(true);
    try {
      const requestId = await AbrirMaltaRedaccion(
        subject.name, subject.academicYear, scopeDescription(),
        filtered.map((e) => ({ data: e.dateString, status: e.status, content: e.content })),
        iaPrompt.trim(),
      );
      iaRequestIdRef.current = requestId;
    } catch (e: any) {
      setIaBusy(false);
      setIaError(e?.message || String(e));
    }
  };

  const handleGenerate = async () => {
    setError('');
    setSavedPath('');
    if (!profile.hasCert) { setError(t('reportBuilder.noCertError')); return; }
    if (filtered.length === 0) { setError(t('reportBuilder.noEntries')); return; }
    // Con "Redacción por IA" marcado, hai que ter xa un borrador revisado antes de
    // asinar — nunca se asina o que a IA escribe sen pasar por aquí primeiro (ver
    // botón "Xerar borrador coa IA" e o cadro de revisión, máis abaixo).
    if (iaRedaccion && !iaDraft?.trim()) { setError(t('reportBuilder.iaRedaccionPendente')); return; }
    setBusy(true);
    try {
      // Recolle, para cada sesión incluída, os ficheiros que se subiron ao servidor
      // co seu checksum (require descargar cada ficheiro para calculalo, ver
      // ListWorkFilesForReport en Go) — por iso só se fai aquí, ao xerar, e non
      // mentres se escolle o alcance. Un fallo puntual nunha sesión non debe impedir
      // xerar o resto do informe: esa sesión queda simplemente sen listaxe.
      const filesByDate: Record<string, WorkFile[]> = {};
      await Promise.all(filtered.map(async (e) => {
        try {
          filesByDate[e.dateString] = await ListWorkFilesForReport(subject.id, e.dateString);
        } catch {
          filesByDate[e.dateString] = [];
        }
      }));
      const html = buildReportHTML({
        t, today, subjectName: subject.name, academicYear: subject.academicYear,
        profile, scopeDescription: scopeDescription(), entries: filtered, filesByDate,
        aiSummary: iaRedaccion ? (iaDraft?.trim() || undefined) : undefined,
      });
      const name = `informe-${scope}-${today}.pdf`;
      // Alcance "Sesión": ademais de gardalo/asinalo, achega o PDF aos ficheiros de
      // traballo dese día (ver comentario en ExportSignedReport, Go) para que quede
      // dispoñible sen que o alumnado teña que subilo á man. Nos demais alcances non
      // hai un único día ao que achegalo.
      const path = await ExportSignedReport(html, name, subject.id, scope === 'session' ? sessionDate : '');
      setSavedPath(path || 'ok');
    } catch (err: any) {
      setError(typeof err === 'string' ? err : (err?.message || t('reportBuilder.generateError')));
    } finally {
      setBusy(false);
    }
  };

  const scopeTabs: { id: ScopeType; label: string }[] = [
    { id: 'session', label: t('reportBuilder.tabSession') },
    { id: 'week', label: t('reportBuilder.tabWeek') },
    { id: 'range', label: t('reportBuilder.tabRange') },
    { id: 'unit', label: t('reportBuilder.tabUnit') },
    { id: 'evaluation', label: t('reportBuilder.tabEvaluation') },
  ];

  const inputCls = 'p-2 bg-white border border-stone-300 rounded-lg shadow-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none';

  // PickDateButton: alternativa a escribir a data - activa datePicker en App e o
  // próximo clic no calendario xeral (visible detrás, ver comentario "Sen veo escuro"
  // máis abaixo) enche o campo. Resaltado en negro mentres é o campo activo.
  const PickDateButton = ({ field }: { field: 'sessionDate' | 'weekDate' | 'fromDate' | 'toDate' }) => (
    <button
      type="button"
      onClick={() => togglePick(field)}
      title={t('reportBuilder.pickFromCalendar')}
      className={`p-2 rounded-lg border transition-colors ${pickingField === field ? 'bg-stone-900 text-white border-stone-900' : 'bg-white border-stone-300 text-stone-500 hover:bg-stone-50'}`}
    >
      <CalendarDays className="w-4 h-4" />
    </button>
  );

  return (
    // Sen veo escuro: o calendario de detrás ten que quedar lexible mentres se escolle
    // o alcance. A capa segue collendo os clics (isto é un diálogo, non unha ferramenta
    // solta), pero xa non tapa nada.
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div
        ref={panelRef}
        style={{ transform: `translate(${offset.x}px, ${offset.y}px)` }}
        className="bg-white rounded-2xl shadow-2xl ring-1 ring-stone-900/10 w-full max-w-2xl overflow-hidden flex flex-col max-h-[90vh]"
      >
        <div
          onPointerDown={onHeaderPointerDown}
          onPointerMove={onHeaderPointerMove}
          onPointerUp={endDrag}
          onPointerCancel={endDrag}
          className="p-4 border-b border-stone-200 flex justify-between items-center bg-stone-50 cursor-grab active:cursor-grabbing select-none touch-none"
        >
          <div className="flex items-center gap-2">
            <FileSignature className="w-5 h-5 text-stone-700" />
            <h3 className="font-bold text-lg text-stone-900">{displayTitle}</h3>
          </div>
          <div className="flex items-center gap-1">
            <GripHorizontal className="w-4 h-4 text-stone-300" aria-hidden />
            <button onClick={onClose} className="p-2 text-stone-400 hover:text-stone-600 rounded-lg hover:bg-stone-200 transition-colors">
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        <div className="p-6 flex flex-col gap-5 overflow-y-auto">
          <div>
            <label className="text-sm font-bold text-stone-700 mb-1.5 block">{t('reportBuilder.scopeLabel')}</label>
            <div className="flex flex-wrap gap-1.5">
              {scopeTabs.map((tab) => (
                <button
                  key={tab.id}
                  onClick={() => setScope(tab.id)}
                  className={`px-3 py-1.5 rounded-lg text-sm font-medium transition-colors ${scope === tab.id ? 'bg-stone-900 text-white' : 'bg-stone-100 text-stone-600 hover:bg-stone-200'}`}
                >
                  {tab.label}
                </button>
              ))}
            </div>
          </div>

          <div className="flex flex-wrap gap-3 items-end">
            {scope === 'session' && (
              <label className="flex flex-col gap-1 text-sm font-medium text-stone-600">{t('reportBuilder.dateLabel')}
                <div className="flex items-center gap-1.5">
                  <input type="date" value={sessionDate} onChange={(e) => setSessionDate(e.target.value)} className={inputCls} />
                  <PickDateButton field="sessionDate" />
                </div>
              </label>
            )}
            {scope === 'week' && (
              <label className="flex flex-col gap-1 text-sm font-medium text-stone-600">{t('reportBuilder.weekContainingLabel')}
                <div className="flex items-center gap-1.5">
                  <input type="date" value={weekDate} onChange={(e) => setWeekDate(e.target.value)} className={inputCls} />
                  <PickDateButton field="weekDate" />
                </div>
              </label>
            )}
            {scope === 'range' && (
              <>
                <label className="flex flex-col gap-1 text-sm font-medium text-stone-600">{t('reportBuilder.fromLabel')}
                  <div className="flex items-center gap-1.5">
                    <input type="date" value={fromDate} onChange={(e) => setFromDate(e.target.value)} className={inputCls} />
                    <PickDateButton field="fromDate" />
                  </div>
                </label>
                <label className="flex flex-col gap-1 text-sm font-medium text-stone-600">{t('reportBuilder.toLabel')}
                  <div className="flex items-center gap-1.5">
                    <input type="date" value={toDate} onChange={(e) => setToDate(e.target.value)} className={inputCls} />
                    <PickDateButton field="toDate" />
                  </div>
                </label>
              </>
            )}
            {pickingField && (
              <p className="w-full text-xs text-stone-500 flex items-center gap-1.5">
                <CalendarDays className="w-3.5 h-3.5 shrink-0" /> {t('reportBuilder.pickingHint')}
              </p>
            )}
            {scope === 'unit' && (
              <label className="flex flex-col gap-1 text-sm font-medium text-stone-600">{t('reportBuilder.unitLabel')}
                <select value={unitId} onChange={(e) => setUnitId(Number(e.target.value))} className={inputCls}>
                  {subject.units.map((u) => <option key={u.id} value={u.id}>{t('reportBuilder.udAbbrev')} {u.id} · {u.title}</option>)}
                </select>
              </label>
            )}
            {scope === 'evaluation' && (
              <label className="flex flex-col gap-1 text-sm font-medium text-stone-600">{t('reportBuilder.evaluationLabel')}
                <select value={evalIndex} onChange={(e) => setEvalIndex(Number(e.target.value))} className={inputCls}>
                  {evaluations.length === 0 && <option value={0}>{t('reportBuilder.noEvaluationsConfigured')}</option>}
                  {evaluations.map((ev, i) => <option key={i} value={i}>{ev.name}</option>)}
                </select>
              </label>
            )}
          </div>

          <div className="text-sm text-stone-600 bg-stone-50 border border-stone-200 rounded-lg px-3 py-2">
            <b>{scopeDescription()}</b> {t('reportBuilder.includedCount', { count: filtered.length })}
          </div>

          {/* Redacción por IA (Malta): optativa. Marcala fai aparecer a caixa de
              petición personalizada e cambia o botón de xerar por "Xerar borrador
              coa IA" — o informe non se asina ata que o borrador se revise abaixo. */}
          <div className="border border-stone-200 rounded-lg overflow-hidden">
            <label className="flex items-center gap-2 px-3 py-2 bg-stone-50 cursor-pointer select-none text-sm font-medium text-stone-700">
              <input
                type="checkbox"
                checked={iaRedaccion}
                onChange={(e) => { setIaRedaccion(e.target.checked); setIaDraft(null); setIaError(''); }}
                className="w-4 h-4 rounded border-stone-300"
              />
              <Sparkles className="w-4 h-4 text-stone-500" />
              {t('reportBuilder.iaRedaccionLabel')}
            </label>
            {iaRedaccion && (
              <div className="px-3 py-3 flex flex-col gap-2 border-t border-stone-200">
                {iaDraft === null ? (
                  <>
                    <label className="flex flex-col gap-1 text-xs font-medium text-stone-600">
                      {t('reportBuilder.iaPromptLabel')}
                      <textarea
                        value={iaPrompt}
                        onChange={(e) => setIaPrompt(e.target.value)}
                        placeholder={t('reportBuilder.iaPromptPlaceholder')}
                        rows={2}
                        className="p-2 bg-white border border-stone-300 rounded-lg shadow-sm text-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none resize-none"
                      />
                    </label>
                    <button
                      type="button"
                      onClick={handleRedactar}
                      disabled={iaBusy || filtered.length === 0}
                      className="self-start flex items-center gap-1.5 px-3 py-1.5 bg-stone-800 hover:bg-stone-900 text-white rounded-lg text-xs font-bold transition-colors disabled:opacity-50"
                    >
                      {iaBusy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Sparkles className="w-3.5 h-3.5" />}
                      {iaBusy ? t('reportBuilder.iaRedaccionXerando') : t('reportBuilder.iaRedaccionXerar')}
                    </button>
                  </>
                ) : (
                  <>
                    <label className="flex flex-col gap-1 text-xs font-medium text-stone-600">
                      {t('reportBuilder.iaDraftLabel')}
                      <textarea
                        value={iaDraft}
                        onChange={(e) => setIaDraft(e.target.value)}
                        rows={6}
                        className="p-2 bg-white border border-stone-300 rounded-lg shadow-sm text-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none"
                      />
                    </label>
                    <button
                      type="button"
                      onClick={handleRedactar}
                      disabled={iaBusy}
                      className="self-start flex items-center gap-1.5 px-3 py-1.5 bg-stone-100 hover:bg-stone-200 text-stone-700 rounded-lg text-xs font-bold transition-colors disabled:opacity-50"
                    >
                      {iaBusy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RotateCcw className="w-3.5 h-3.5" />}
                      {t('reportBuilder.iaRedaccionRexenerar')}
                    </button>
                  </>
                )}
                {iaError && <p className="text-xs text-rose-600">{iaError}</p>}
              </div>
            )}
          </div>

          {/* Non se pide contrasinal: o certificado ábrese co da sesión, que é o mesmo
              co que se protexe ao emitilo e o que xa se escribiu ao entrar. */}
          <div className="text-sm text-stone-600 bg-stone-50 border border-stone-200 rounded-lg px-3 py-2 flex items-start gap-2">
            <ShieldCheck className="w-4 h-4 mt-0.5 shrink-0 text-stone-500" />
            <span>
              {t('reportBuilder.certSignIntro')}
              {profile.certSource === 'servidor' && <> ({t('reportBuilder.certSourceServerPre')}<b>{t('reportBuilder.certSourceServerBold')}</b>{t('reportBuilder.certSourceServerPost')})</>}
              {profile.certSource === 'local' && <> ({t('reportBuilder.certSourceLocalPre')}<b>{t('reportBuilder.certSourceLocalBold')}</b>)</>}
              {t('reportBuilder.certPasswordNote')}
            </span>
          </div>

          {error && <div className="text-sm text-rose-700 bg-rose-50 border border-rose-200 rounded-lg px-3 py-2">{error}</div>}
          {savedPath && (
            <div className="text-sm text-emerald-800 bg-emerald-50 border border-emerald-200 rounded-lg px-3 py-2 flex items-center gap-2">
              <CheckCircle2 className="w-4 h-4 shrink-0" />
              <span className="flex-1">{t('reportBuilder.savedIn')}</span>
              {savedPath !== 'ok' && (
                <button
                  onClick={() => OpenFile(savedPath).catch((e: any) => setError(e?.message || String(e)))}
                  className="flex items-center gap-1.5 px-3 py-1 bg-emerald-700 hover:bg-emerald-800 text-white rounded-lg text-xs font-bold transition-colors shrink-0"
                >
                  <FolderOpen className="w-3.5 h-3.5" /> {t('reportBuilder.openButton')}
                </button>
              )}
            </div>
          )}
        </div>

        <div className="p-4 border-t border-stone-200 bg-stone-50 flex justify-end gap-2">
          <button onClick={onClose} className="px-4 py-2 text-stone-600 font-medium hover:bg-stone-200 rounded-lg transition-colors">{t('reportBuilder.closeButton')}</button>
          <button
            onClick={handleGenerate}
            disabled={busy || filtered.length === 0 || (iaRedaccion && !iaDraft?.trim())}
            title={iaRedaccion && !iaDraft?.trim() ? t('reportBuilder.iaRedaccionPendente') : undefined}
            className="px-4 py-2 bg-stone-900 text-white font-medium hover:bg-stone-800 disabled:opacity-50 rounded-lg transition-colors flex items-center gap-2"
          >
            {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : <FileSignature className="w-4 h-4" />}
            {busy ? t('reportBuilder.generating') : t('reportBuilder.generateButton')}
          </button>
        </div>
      </div>
    </div>
  );
}
