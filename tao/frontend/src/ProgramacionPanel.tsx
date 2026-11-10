import React, { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ClipboardList, X, RefreshCw, Loader2, AlertCircle, Settings, BookOpen, Target, ListChecks, Percent, ArrowLeft, CalendarClock } from 'lucide-react';
import { Subject, Unit, ProensInfo, ProensUnidadeInfo } from './types';
import { ImportProensInfo } from '../bindings/tao/app';

const WIDTH_KEY = 'tao-programacion-panel-width';
const WIDTH_DEFAULT = 440;
const WIDTH_MIN = 360;
const WIDTH_MAX_VW = 0.6; // nunca máis do 60% do ancho da xanela
const WIDTH_MAX_ABS = 760;

const anchoMaximo = () => Math.min(WIDTH_MAX_ABS, window.innerWidth * WIDTH_MAX_VW);

interface ProgramacionPanelProps {
  subject: Subject;
  proensUrl: string;
  // podeActualizar: só o profesorado editando a súa propia materia (non revisando
  // alumnado, non o propio alumnado) pode volver chamar a Proens dende este panel — ver
  // App.tsx. Sen isto, o panel limítase a amosar subject.programacionInfo, a caché xa
  // publicada polo profesorado (chegada do servidor no login): nunca chama a Proens.
  podeActualizar: boolean;
  onClose: () => void;
  onOpenSettings: () => void;
  // onImported garda o resultado dunha importación correcta na materia
  // (Subject.programacionInfo) E publícao no servidor (ver onProgramacionInfoExtraida en
  // App.tsx), para que calquera outro dispositivo o lea sen ter que rescrapear Proens.
  onImported: (info: ProensInfo) => void;
  // focusUnit, se está presente, abre o panel enfocado nesa UD (chip "UD N" do
  // calendario) en vez da vista xeral da programación: cabeceira coa UD e
  // trimestre, e só as seccións de currículo/avaliación desa UD.
  focusUnit?: Unit;
  // onUnfocus volve á vista xeral sen pechar o panel (botón "←" da cabeceira).
  onUnfocus?: () => void;
}

// ProgramacionPanel é a columna lateral dereita, redimensionable arrastrando o
// seu bordo esquerdo, coa información da programación extraída de Proens
// (tao/proens_info.go, ImportProensInfo). Ten dúas vistas: a xeral (Introdución,
// Obxectivos e a súa contribución ás competencias, listaxe de unidades,
// Criterios de cualificación) e, cando `focusUnit` está presente, unha vista
// centrada nunha UD concreta (trimestre/sesións, Distribución de currículo por
// bloque e Avaliación da unidade), aberta dende os chips "UD N" do calendario.
// Non existía ningún patrón de columna redimensionable en Tao antes disto — o
// arrastre reaproveita o mesmo patrón mousemove/mouseup que xa usa o panel de
// comentarios (CommentPanel en App.tsx), pero aplicado ao ancho en vez de á
// posición.
export default function ProgramacionPanel({ subject, proensUrl, podeActualizar, onClose, onOpenSettings, onImported, focusUnit, onUnfocus }: ProgramacionPanelProps) {
  const { t } = useTranslation();

  const [width, setWidth] = useState(() => {
    const gardado = Number(localStorage.getItem(WIDTH_KEY));
    return gardado && gardado >= WIDTH_MIN ? Math.min(gardado, anchoMaximo()) : WIDTH_DEFAULT;
  });
  const resizingRef = useRef(false);

  const [info, setInfo] = useState<ProensInfo | undefined>(subject.programacionInfo);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  // importar chama a Proens en vivo — só dispoñible cando podeActualizar (profesorado
  // editando a súa propia materia). O resto (alumnado, revisión) nunca a executa: len
  // sempre subject.programacionInfo, a caché que o profesorado xa publicou no servidor.
  const importar = async () => {
    if (!proensUrl || !podeActualizar) return;
    setLoading(true);
    setError('');
    try {
      const result = await ImportProensInfo(proensUrl);
      setInfo(result);
      onImported(result);
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setLoading(false);
    }
  };

  // Ao abrir, e só se podeActualizar: se xa hai unha copia cacheada da materia, amósase
  // decontado (sen chamada de rede); se non e hai URL configurada, impórtase
  // automaticamente. Tamén se reimporta se a caché é dunha versión anterior sen
  // `unidades` (Subject.programacionInfo gardado antes de existir o desglose por UD).
  useEffect(() => {
    if (podeActualizar && (!subject.programacionInfo || !subject.programacionInfo.unidades) && proensUrl) {
      importar();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // subject.programacionInfo pode chegar máis tarde có primeiro render (p.ex. o alumnado
  // aínda cargando GetMyMaterias) — mantense `info` sincronizado sen depender só do
  // estado inicial.
  useEffect(() => {
    if (subject.programacionInfo && subject.programacionInfo !== info) {
      setInfo(subject.programacionInfo);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subject.programacionInfo]);

  // Arrastre do bordo esquerdo para redimensionar (mesmo patrón mousemove/mouseup
  // ca CommentPanel, aplicado a `width` en vez de a `x`/`y`).
  useEffect(() => {
    const mover = (e: MouseEvent) => {
      if (!resizingRef.current) return;
      const novo = Math.min(anchoMaximo(), Math.max(WIDTH_MIN, window.innerWidth - e.clientX));
      setWidth(novo);
    };
    const soltar = () => {
      if (!resizingRef.current) return;
      resizingRef.current = false;
      setWidth((w) => {
        localStorage.setItem(WIDTH_KEY, String(w));
        return w;
      });
    };
    window.addEventListener('mousemove', mover);
    window.addEventListener('mouseup', soltar);
    return () => {
      window.removeEventListener('mousemove', mover);
      window.removeEventListener('mouseup', soltar);
    };
  }, []);

  // UD enfocada (cruzamento por título, con fallback por posición igual có
  // que xa fai handleSave en App.tsx para pendingProgrammingData): a orde de
  // `info.unidades` é a mesma ca `subject.units` porque ambas as dúas veñen,
  // en última instancia, da mesma táboa 3.1 de Proens.
  // `info.unidades` pode faltar nunha caché vella de Subject.programacionInfo
  // gardada antes de que este campo existise — de aí o `?? []`, para que abra
  // (con EstadoVacio convidando a actualizar) en vez de petar.
  const udInfo: ProensUnidadeInfo | undefined = focusUnit && info
    ? (info.unidades ?? []).find((u) => u.titulo === focusUnit.title) ?? (info.unidades ?? [])[focusUnit.id - 1]
    : undefined;

  return (
    <div
      className="fixed right-0 top-0 h-full z-50 bg-white border-l border-stone-200 shadow-2xl flex flex-col print:hidden"
      style={{ width }}
    >
      {/* Handle de resize */}
      <div
        onMouseDown={() => { resizingRef.current = true; }}
        title={t('app.programacion.arrastrarTitulo')}
        className="absolute left-0 top-0 h-full w-1.5 -ml-0.5 cursor-col-resize hover:bg-indigo-300/60 active:bg-indigo-400/60 transition-colors z-10"
      />

      <div className="p-4 border-b border-stone-200 flex items-center justify-between gap-2 bg-stone-50 shrink-0">
        <h3 className="font-bold text-stone-900 flex items-center gap-2 min-w-0">
          {focusUnit && onUnfocus && (
            <button onClick={onUnfocus} title={t('app.programacion.volverVisionXeral')} className="p-1 -ml-1 text-stone-400 hover:text-stone-700 hover:bg-stone-200 rounded-lg transition-colors shrink-0">
              <ArrowLeft className="w-4 h-4" />
            </button>
          )}
          <ClipboardList className="w-5 h-5 text-indigo-600 shrink-0" />
          <span className="truncate">{focusUnit ? `UD ${focusUnit.id} — ${focusUnit.title}` : t('app.programacion.titulo')}</span>
        </h3>
        <div className="flex items-center gap-1 shrink-0">
          {/* Só o profesorado editando a súa propia materia pode volver chamar a Proens
              dende aquí — o alumnado e o modo revisión len só a caché xa publicada. */}
          {podeActualizar && (
            <button
              onClick={importar}
              disabled={loading || !proensUrl}
              title={t('app.programacion.actualizarTitulo')}
              className="p-2 text-stone-400 hover:text-stone-700 hover:bg-stone-200 rounded-lg transition-colors disabled:opacity-40"
            >
              <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
            </button>
          )}
          <button onClick={onClose} className="p-2 text-stone-400 hover:text-stone-600 hover:bg-stone-200 rounded-lg transition-colors">
            <X className="w-5 h-5" />
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-5 space-y-4">
        {/* Alumnado e modo revisión: nunca chaman a Proens, só len subject.programacionInfo
            (publicado polo profesorado no servidor) — se aínda non hai nada que ler,
            amósase un estado informativo sen ningunha acción (non hai nada que poidan facer
            eles dende aquí). */}
        {!podeActualizar && !info && (
          <EstadoVacio
            icon={<ClipboardList className="w-8 h-8 text-stone-300" />}
            titulo={t('app.programacion.senDatosAinda')}
            texto={t('app.programacion.senDatosAindaTexto')}
          />
        )}

        {podeActualizar && !proensUrl && (
          <EstadoVacio
            icon={<Settings className="w-8 h-8 text-stone-300" />}
            titulo={t('app.programacion.senUrl')}
            texto={t('app.programacion.senUrlTexto')}
            accion={<button onClick={onOpenSettings} className="mt-3 px-4 py-2 bg-stone-800 hover:bg-stone-900 text-white text-sm font-medium rounded-lg transition-colors">{t('app.programacion.abrirConfiguracion')}</button>}
          />
        )}

        {podeActualizar && proensUrl && loading && !info && (
          <EstadoVacio
            icon={<Loader2 className="w-8 h-8 text-indigo-400 animate-spin" />}
            titulo={t('app.programacion.cargando')}
            texto={t('app.programacion.cargandoTexto')}
          />
        )}

        {proensUrl && error && !info && (
          <EstadoVacio
            icon={<AlertCircle className="w-8 h-8 text-rose-400" />}
            titulo={t('app.programacion.erro')}
            texto={error}
            accion={<button onClick={importar} className="mt-3 px-4 py-2 bg-stone-800 hover:bg-stone-900 text-white text-sm font-medium rounded-lg transition-colors">{t('app.programacion.reintentar')}</button>}
          />
        )}

        {info && (
          <>
            {error && (
              <div className="flex items-center gap-2 text-xs text-rose-600 bg-rose-50 border border-rose-200 rounded-lg px-3 py-2">
                <AlertCircle className="w-3.5 h-3.5 shrink-0" /> {error}
              </div>
            )}
            {loading && (
              <div className="flex items-center gap-2 text-xs text-stone-500">
                <Loader2 className="w-3.5 h-3.5 animate-spin" /> {t('app.programacion.actualizando')}
              </div>
            )}

            {focusUnit ? (
              <>
                <div className="flex items-center gap-2 text-sm text-stone-500 bg-stone-50 border border-stone-200 rounded-lg px-3 py-2">
                  <CalendarClock className="w-4 h-4 shrink-0" />
                  {udInfo?.trimestre
                    ? t('app.programacion.trimestreESesions', { trimestre: udInfo.trimestre, sesions: focusUnit.sessions })
                    : t('app.programacion.sesions', { sesions: focusUnit.sessions })}
                </div>

                <SeccionCard icon={<ListChecks className="w-4 h-4" />} titulo={t('app.programacion.distribucionCurriculoUnidade')}>
                  {udInfo && (udInfo.bloques ?? []).length > 0 ? (
                    <div className="space-y-3">
                      {(udInfo.bloques ?? []).map((b, i) => (
                        <div key={i} className="bg-stone-50 rounded-lg p-3 border border-stone-100">
                          <p className="text-sm font-bold text-stone-700 mb-2">{b.bloque}</p>
                          {(b.criterios ?? []).length > 0 && (
                            <div className="mb-2">
                              <p className="text-[10px] font-bold text-stone-400 uppercase tracking-wide mb-1">{t('app.programacion.criterios')}</p>
                              <ul className="space-y-1 text-sm text-stone-600 leading-relaxed">
                                {(b.criterios ?? []).map((c, j) => (
                                  <li key={j}><span className="font-bold text-indigo-700">{c.codigo}</span>{c.codigo ? ' — ' : ''}{c.descricion}</li>
                                ))}
                              </ul>
                            </div>
                          )}
                          {(b.contidos ?? []).length > 0 && (
                            <div>
                              <p className="text-[10px] font-bold text-stone-400 uppercase tracking-wide mb-1">{t('app.programacion.contidos')}</p>
                              <ul className="space-y-1 text-sm text-stone-600 leading-relaxed">
                                {(b.contidos ?? []).map((c, j) => (
                                  <li key={j}><span className="font-bold text-indigo-700">{c.codigo}</span>{c.codigo ? ' — ' : ''}{c.descricion}</li>
                                ))}
                              </ul>
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  ) : <SemDatos texto={t('app.programacion.semDatosUnidade')} />}
                </SeccionCard>

                <SeccionCard icon={<Percent className="w-4 h-4" />} titulo={t('app.programacion.avaliacionUnidade')}>
                  {udInfo && (udInfo.avaliacion ?? []).length > 0 ? (
                    <div className="rounded-lg border border-stone-200 overflow-hidden">
                      {(udInfo.avaliacion ?? []).map((instr, i) => (
                        <div key={i} className={`px-3 py-2 text-sm ${i % 2 ? 'bg-stone-50' : 'bg-white'}`}>
                          <p className="text-stone-700 leading-relaxed">
                            <span className="font-bold text-indigo-700">{instr.criterio.codigo}</span>{instr.criterio.codigo ? ' — ' : ''}{instr.criterio.descricion}
                          </p>
                          <div className="flex items-center justify-between text-xs text-stone-500 mt-1">
                            <span>{instr.tipoInstrumento}{instr.graoMinimo ? ` · ${instr.graoMinimo}` : ''}</span>
                            {instr.peso && <span className="font-bold text-stone-800 shrink-0 ml-2">{instr.peso}%</span>}
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : <SemDatos texto={t('app.programacion.semDatosUnidade')} />}
                </SeccionCard>
              </>
            ) : (
              <>
                <SeccionCard icon={<BookOpen className="w-4 h-4" />} titulo={t('app.programacion.introducion')}>
                  {info.introducion.length > 0 ? (
                    <div className="space-y-2 text-sm text-stone-600 leading-relaxed">
                      {info.introducion.map((p, i) => <p key={i}>{p}</p>)}
                    </div>
                  ) : <SemDatos texto={t('app.programacion.semDatos')} />}
                </SeccionCard>

                <SeccionCard icon={<Target className="w-4 h-4" />} titulo={t('app.programacion.obxectivosCompetencias')}>
                  {info.obxectivos.length > 0 ? (
                    <div className="space-y-2">
                      {info.obxectivos.map((obx, i) => (
                        <div key={i} className="bg-stone-50 rounded-lg p-3 border border-stone-100">
                          <p className="text-sm text-stone-700 leading-relaxed">
                            <span className="font-bold text-indigo-700">{obx.codigo}</span>{obx.codigo ? ' — ' : ''}{obx.texto}
                          </p>
                          {obx.competencias.length > 0 && (
                            <div className="flex flex-wrap gap-1 mt-2">
                              {obx.competencias.map((c) => (
                                <span key={c} className="bg-indigo-50 text-indigo-700 rounded-full px-2 py-0.5 text-[11px] font-medium">{c}</span>
                              ))}
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  ) : <SemDatos texto={t('app.programacion.semDatos')} />}
                </SeccionCard>

                <SeccionCard icon={<ListChecks className="w-4 h-4" />} titulo={t('app.programacion.distribucionCurriculo')}>
                  {subject.units.length > 0 ? (
                    <div className="space-y-1.5">
                      {subject.units.map((u) => (
                        <div key={u.id} className="flex items-center gap-2 text-sm">
                          <span className={`w-2.5 h-2.5 rounded-full shrink-0 ${u.color}`} />
                          <span className="text-stone-700 flex-1 truncate">UD {u.id} · {u.title}</span>
                          <span className="text-stone-400 text-xs shrink-0">{u.sessions} {t('app.programacion.sesionsAbrev')}</span>
                        </div>
                      ))}
                    </div>
                  ) : <SemDatos texto={t('app.programacion.semUnidades')} />}
                </SeccionCard>

                <SeccionCard icon={<Percent className="w-4 h-4" />} titulo={t('app.programacion.criteriosCualificacion')}>
                  {info.criteriosPesos.length > 0 && (
                    <div className="mb-3 rounded-lg border border-stone-200 overflow-hidden">
                      {info.criteriosPesos.map((p, i) => (
                        <div key={i} className={`flex items-center justify-between px-3 py-1.5 text-sm ${i % 2 ? 'bg-stone-50' : 'bg-white'}`}>
                          <span className="text-stone-600">{p.instrumento}</span>
                          <span className="font-bold text-stone-800">{p.total}%</span>
                        </div>
                      ))}
                    </div>
                  )}
                  {info.criteriosTexto.length > 0 ? (
                    <div className="space-y-2 text-sm text-stone-600 leading-relaxed">
                      {info.criteriosTexto.map((p, i) => <p key={i}>{p}</p>)}
                    </div>
                  ) : (info.criteriosPesos.length === 0 && <SemDatos texto={t('app.programacion.semDatos')} />)}
                </SeccionCard>
              </>
            )}
          </>
        )}
      </div>
    </div>
  );
}

function SeccionCard({ icon, titulo, children }: { icon: React.ReactNode, titulo: string, children: React.ReactNode }) {
  return (
    <div className="bg-white rounded-xl border border-stone-200 shadow-sm p-4">
      <div className="flex items-center gap-2 text-stone-800 mb-3">
        <span className="text-indigo-600">{icon}</span>
        <h4 className="font-bold text-sm uppercase tracking-wide">{titulo}</h4>
      </div>
      {children}
    </div>
  );
}

function SemDatos({ texto }: { texto: string }) {
  return <p className="text-sm text-stone-400 italic">{texto}</p>;
}

function EstadoVacio({ icon, titulo, texto, accion }: { icon: React.ReactNode, titulo: string, texto: string, accion?: React.ReactNode }) {
  return (
    <div className="flex flex-col items-center text-center gap-2 py-10 px-4">
      {icon}
      <p className="font-bold text-stone-700">{titulo}</p>
      <p className="text-sm text-stone-500 max-w-xs">{texto}</p>
      {accion}
    </div>
  );
}
