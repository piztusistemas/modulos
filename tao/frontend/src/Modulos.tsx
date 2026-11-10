import React, { useEffect, useState } from 'react';
import { X, Package, Loader2, Download, RefreshCw, Play, FolderOpen, AlertCircle, Info } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  ModulosDispoTao, SetModuloTao, InstalarModuloTao, ActualizarModuloTao,
  AbrirModuloTao, AbrirCartafolModulosTao,
} from '../bindings/tao/app';
import { Events } from '@wailsio/runtime';
import { ModuloInfoTao } from '../bindings/tao/models';

interface Props {
  onClose: () => void;
}

// ModulosTao ("⚙ → Módulos") — clon, en pequeno, do panel de módulos de
// Piztu (⚙ Aula → Módulos, piztu/frontend/src/main.js): lista os módulos
// externos DE Tao (subcartafoles de RutaModulosTao(), cada un cun
// modulo.json), permite instalalos/actualizalos dende
// github.com/piztusistemas/tao_modulos, e activalos/desactivalos á vontade. Un
// módulo inactivo compórtase coma se non estivese instalado (AbrirModuloTao
// devolve erro) — mesmo criterio ca Piztu.
export default function ModulosTao({ onClose }: Props) {
  const { t } = useTranslation();
  const [modulos, setModulos] = useState<ModuloInfoTao[]>([]);
  const [cargando, setCargando] = useState(true);
  const [ocupado, setOcupado] = useState<Record<string, boolean>>({});
  const [erros, setErros] = useState<Record<string, string>>({});
  const [sobre, setSobre] = useState<ModuloInfoTao | null>(null);

  const cargar = async () => {
    setCargando(true);
    try {
      setModulos(await ModulosDispoTao());
    } catch (e: any) {
      console.error('Erro cargando módulos:', e);
    } finally {
      setCargando(false);
    }
  };

  useEffect(() => {
    cargar();
    const off = Events.On('modulos_tao_cambiado', () => cargar());
    return () => { off(); };
  }, []);

  const executar = async (id: string, accion: () => Promise<any>) => {
    setOcupado(o => ({ ...o, [id]: true }));
    setErros(e => ({ ...e, [id]: '' }));
    try {
      await accion();
      await cargar();
    } catch (e: any) {
      setErros(er => ({ ...er, [id]: e?.message || String(e) }));
    } finally {
      setOcupado(o => ({ ...o, [id]: false }));
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-2xl max-h-[85vh] overflow-hidden flex flex-col">
        <div className="p-4 border-b border-stone-200 flex justify-between items-center bg-stone-50">
          <h3 className="font-bold text-lg text-stone-900 flex items-center gap-2">
            <Package className="w-5 h-5" /> {t('modulos.titulo', 'Módulos de Tao')}
          </h3>
          <button onClick={onClose} className="p-2 text-stone-400 hover:text-stone-600 rounded-lg hover:bg-stone-200 transition-colors">
            <X className="w-5 h-5" />
          </button>
        </div>

        <div className="p-6 overflow-y-auto flex-1">
          <p className="text-sm text-stone-500 mb-4">
            {t('modulos.descricion', 'Amplía Tao con módulos de terceiros, publicados en github.com/piztusistemas/tao_modulos. Un módulo desactivado non pode falar coa API de Tao nin lanzarse.')}
          </p>

          {cargando && modulos.length === 0 && (
            <div className="flex items-center justify-center py-10 text-stone-400">
              <Loader2 className="w-6 h-6 animate-spin" />
            </div>
          )}

          {!cargando && modulos.length === 0 && (
            <p className="text-sm text-stone-400 italic text-center py-10">
              {t('modulos.baleiro', 'Non hai módulos instalados nin dispoñibles agora mesmo.')}
            </p>
          )}

          <div className="flex flex-col gap-3">
            {modulos.map(m => (
              <div key={m.id} className="border border-stone-200 rounded-xl p-4 flex flex-col gap-2">
                <div className="flex items-center justify-between gap-3">
                  <div className="flex items-center gap-3 min-w-0">
                    {m.iconaImaxeDataURI ? (
                      <img src={m.iconaImaxeDataURI} alt="" className="w-8 h-8 rounded-lg object-contain shrink-0" />
                    ) : (
                      <span className="text-2xl shrink-0">{m.icona || '🧩'}</span>
                    )}
                    <div className="min-w-0">
                      <p className="font-bold text-stone-800 truncate">{m.nome}</p>
                      <p className="text-xs text-stone-400">
                        {m.instalado
                          ? t('modulos.version', 'v{{v}}', { v: m.version })
                          : t('modulos.dispoñibleVersion', 'dispoñible: v{{v}}', { v: m.versionNova })}
                      </p>
                    </div>
                  </div>

                  <div className="flex items-center gap-2 shrink-0">
                    {m.instalado && (
                      <button
                        onClick={() => setSobre(m)}
                        title={t('modulos.sobre.boton', 'Sobre {{nome}}', { nome: m.nome })}
                        className="p-1.5 text-stone-400 hover:text-stone-700 hover:bg-stone-100 rounded-lg transition-colors"
                      >
                        <Info className="w-4 h-4" />
                      </button>
                    )}
                    {!m.instalado && (
                      <button
                        onClick={() => executar(m.id, () => InstalarModuloTao(m.id))}
                        disabled={!!ocupado[m.id]}
                        className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-stone-800 hover:bg-stone-900 text-white rounded-lg font-medium transition-colors disabled:opacity-50"
                      >
                        {ocupado[m.id] ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Download className="w-3.5 h-3.5" />}
                        {t('modulos.instalar', 'Instalar')}
                      </button>
                    )}
                    {m.instalado && m.actualizable && (
                      <button
                        onClick={() => executar(m.id, () => ActualizarModuloTao(m.id))}
                        disabled={!!ocupado[m.id]}
                        title={t('modulos.actualizarTitulo', 'Actualizar a v{{v}}', { v: m.versionNova })}
                        className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-amber-500 hover:bg-amber-600 text-white rounded-lg font-medium transition-colors disabled:opacity-50"
                      >
                        {ocupado[m.id] ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RefreshCw className="w-3.5 h-3.5" />}
                        {t('modulos.actualizar', 'Actualizar')}
                      </button>
                    )}
                    {m.instalado && (
                      <>
                        <label className="flex items-center gap-2 text-sm text-stone-600 cursor-pointer select-none">
                          {m.activo ? t('modulos.activo', 'Activo') : t('modulos.inactivo', 'Inactivo')}
                          <input
                            type="checkbox"
                            checked={m.activo}
                            disabled={!!ocupado[m.id]}
                            onChange={() => executar(m.id, () => SetModuloTao(m.id, !m.activo))}
                            className="toggle"
                          />
                        </label>
                        {m.activo && (
                          <button
                            onClick={() => executar(m.id, () => AbrirModuloTao(m.id))}
                            disabled={!!ocupado[m.id]}
                            className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg font-medium transition-colors disabled:opacity-50"
                          >
                            <Play className="w-3.5 h-3.5" /> {t('modulos.abrir', 'Abrir')}
                          </button>
                        )}
                      </>
                    )}
                  </div>
                </div>

                {m.permisos && m.permisos.length > 0 && (
                  <div className="flex flex-wrap gap-1">
                    {m.permisos.map(p => (
                      <span key={p} className="bg-stone-100 text-stone-500 rounded-full px-2 py-0.5 text-[11px] font-mono">{p}</span>
                    ))}
                  </div>
                )}

                {erros[m.id] && (
                  <p className="flex items-center gap-1.5 text-xs text-rose-600">
                    <AlertCircle className="w-3.5 h-3.5 shrink-0" /> {erros[m.id]}
                  </p>
                )}
              </div>
            ))}
          </div>
        </div>

        <div className="p-4 border-t border-stone-200 bg-stone-50 flex justify-between items-center">
          <button
            onClick={() => AbrirCartafolModulosTao().catch((e: any) => console.error(e))}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm text-stone-600 hover:text-stone-900 hover:bg-stone-200 rounded-lg transition-colors"
          >
            <FolderOpen className="w-4 h-4" /> {t('modulos.abrirCartafol', 'Abrir cartafol de módulos')}
          </button>
          <button
            onClick={cargar}
            disabled={cargando}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm text-stone-600 hover:text-stone-900 hover:bg-stone-200 rounded-lg transition-colors"
          >
            <RefreshCw className={`w-4 h-4 ${cargando ? 'animate-spin' : ''}`} /> {t('modulos.comprobar', 'Comprobar actualizacións')}
          </button>
        </div>
      </div>

      {sobre && (
        <div className="fixed inset-0 bg-black/50 z-[60] flex items-center justify-center p-4">
          <div className="bg-white rounded-2xl shadow-xl w-full max-w-sm overflow-hidden flex flex-col">
            <div className="flex items-center justify-between px-4 py-3 border-b border-stone-200">
              <span className="font-bold text-stone-800">
                {t('modulos.sobre.titulo', 'Sobre {{nome}}', { nome: sobre.nome })}
              </span>
              <button onClick={() => setSobre(null)} className="p-1 rounded hover:bg-stone-100 text-stone-500">
                <X className="w-4 h-4" />
              </button>
            </div>
            <div className="flex flex-col items-center gap-1.5 px-6 py-5 text-center text-sm text-stone-600">
              {sobre.iconaImaxeDataURI ? (
                <img src={sobre.iconaImaxeDataURI} alt="" className="w-14 h-14 object-contain mb-1" />
              ) : (
                <span className="text-4xl mb-1">{sobre.icona || '🧩'}</span>
              )}
              <p className="text-stone-800 font-medium">
                {t('modulos.version', 'v{{v}}', { v: sobre.version })}
              </p>
              {sobre.descricion && <p className="mt-1">{sobre.descricion}</p>}
              {sobre.autor && <p className="mt-2">{t('modulos.sobre.autor', 'Autor: {{autor}}', { autor: sobre.autor })}</p>}
              {sobre.web && (
                <p><a href={sobre.web} target="_blank" rel="noreferrer" className="text-piztu-on underline">{sobre.web}</a></p>
              )}
              {sobre.email && <p>{t('modulos.sobre.contacto', 'Contacto: {{email}}', { email: sobre.email })}</p>}
              <p className="font-semibold mt-2">{t('sobre.licenza', 'Licenza GNU General Public License v3 (GPLv3)')}</p>
              <p className="text-xs text-stone-500 whitespace-pre-line">
                {t('sobre.licdesc', 'Software libre: pódese usar, estudar, modificar e redistribuír libremente. Calquera traballo derivado que se redistribúa ten que publicarse tamén baixo GPLv3, co código fonte dispoñible (copyleft). Texto completo no ficheiro LICENSE.')}
              </p>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
