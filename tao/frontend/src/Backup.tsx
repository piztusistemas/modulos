import { useState } from 'react';
import { Loader2, Save, Upload, RotateCcw, CheckCircle2, AlertTriangle, FolderDown } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { backupManifest, RestoreResult, SiteRestoreResult, AllCoursesRestoreResult, SiteBackupResult } from '../bindings/tao/models';
import { BackupSite, BackupCourses, LoadBackupArchive, RestoreCourse, RestoreSite, RestoreAllCourses } from '../bindings/tao/app';

export interface CourseOpt { id: string; name: string; }

// BackupRestore is the shared backup/restore UI, used both by the admin panel (role
// 'admin' → whole-site copy) and by the professor (role 'professor' → chosen courses).
// Restore is identical for both: load a .zip, pick a source course from it and a target
// course already created with its new calendar, and restore remapping by session ordinal.
export default function BackupRestore({ role, courses }: { role: 'admin' | 'professor'; courses: CourseOpt[] }) {
  const { t } = useTranslation();
  const admin = role === 'admin';

  // Backup state. O Director escolle entre copiar todo o sitio ou só uns cursos:
  // ata agora só podía o primeiro, e para arquivar un curso tiña que levar o sitio
  // enteiro.
  const [wholeSite, setWholeSite] = useState(true);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  // Marcado de serie: unha copia do sitio sen os usuarios non serve para devolver o
  // servidor ao seu estado (perderíanse traballos e mensaxes). Quen queira unha copia
  // só da estrutura pode desmarcalo.
  const [includeUsers, setIncludeUsers] = useState(true);
  // Contrasinal que levará o CSV de altas que acompaña á copia do sitio. Baleiro =
  // columna baleira, para enchela o día que faga falta restaurar.
  const [contrasinalAltas, setContrasinalAltas] = useState('');
  const [backingUp, setBackingUp] = useState(false);

  // Restore state.
  const [manifest, setManifest] = useState<backupManifest | null>(null);
  const [loadingArchive, setLoadingArchive] = useState(false);
  const [sourceId, setSourceId] = useState('');
  const [targetId, setTargetId] = useState('');
  const [withStudents, setWithStudents] = useState(true);
  const [restoring, setRestoring] = useState(false);
  // Restauración de TODOS os cursos da copia dunha vez (só Director).
  const [allUsers, setAllUsers] = useState(true);
  const [confirmAll, setConfirmAll] = useState(false);
  const [restoringAll, setRestoringAll] = useState(false);
  // Restauración de todo o sitio (só Director, e só cunha copia de tipo "site").
  const [siteUsers, setSiteUsers] = useState(true);
  const [confirmSite, setConfirmSite] = useState(false);
  const [restoringSite, setRestoringSite] = useState(false);

  const [msg, setMsg] = useState('');
  const [err, setErr] = useState('');

  const toggle = (id: string) => setSelected(p => { const n = new Set(p); n.has(id) ? n.delete(id) : n.add(id); return n; });

  const doBackup = async () => {
    setBackingUp(true); setMsg(''); setErr('');
    try {
      if (admin && wholeSite) {
        const r: SiteBackupResult = await BackupSite(includeUsers, contrasinalAltas);
        setMsg(r.zip
          ? t('backup.gardadaSitio', { zip: r.zip, csv: r.csv, altas: r.altas })
          : t('backup.cancelada'));
      } else {
        const path = await BackupCourses([...selected], includeUsers);
        setMsg(path ? t('backup.gardada', { path }) : t('backup.cancelada'));
      }
    } catch (e: any) { setErr(e?.message || String(e)); }
    finally { setBackingUp(false); }
  };

  const loadArchive = async () => {
    setLoadingArchive(true); setMsg(''); setErr('');
    try {
      const man = await LoadBackupArchive();
      const cursos = (man?.courses || []).length;
      const usuarios = (man?.users || []).length;
      // Unha copia sen cursos pero con usuarios segue a ser útil (mensaxes, traballos,
      // matrícula): acéptase igual, e xa se verá cales das tres opcións se ofrecen.
      if (man && (cursos || usuarios)) {
        setManifest(man);
        setSourceId((man.courses || [])[0] ?? '');
        let texto = t('backup.cargadaDetalle', { cursos, usuarios });
        if (!cursos) texto += ' ' + t('backup.cargadaSenCursos');
        setMsg(texto);
      } else {
        setManifest(null);
        setMsg(t('backup.semCargar'));
      }
    } catch (e: any) { setErr(e?.message || String(e)); }
    finally { setLoadingArchive(false); }
  };

  const doRestore = async () => {
    if (!sourceId || !targetId) { setErr(t('backup.escolleAmbos')); return; }
    setRestoring(true); setMsg(''); setErr('');
    try {
      const r: RestoreResult = await RestoreCourse(sourceId, targetId, withStudents);
      const parts = [
        t('backup.restaurado', { source: r.sourceMateria, target: r.targetMateria }),
        t('backup.sesionsMapeadas', { count: r.sessionsMapped }) +
          (r.droppedSessions ? t('backup.descartadas', { count: r.droppedSessions }) : '.'),
        t('backup.ficheirosTarefa', { count: r.taskFiles }),
      ];
      if (withStudents) {
        parts.push(t('backup.alumnadoRestaurado', { count: (r.studentsUpdated || []).length }));
        if ((r.studentsSkipped || []).length) parts.push(t('backup.omitidos', { lista: r.studentsSkipped.join(', ') }));
      }
      setMsg(parts.join(' '));
    } catch (e: any) { setErr(e?.message || String(e)); }
    finally { setRestoring(false); }
  };

  const doRestoreAll = async () => {
    setConfirmAll(false);
    setRestoringAll(true); setMsg(''); setErr('');
    try {
      const r: AllCoursesRestoreResult = await RestoreAllCourses(allUsers);
      const partes = [t('backup.todosRestaurados', {
        count: (r.courses || []).length, lista: (r.courses || []).join(', '), taskFiles: r.taskFiles,
      })];
      if (allUsers) partes.push(t('backup.todosAlumnado', { count: (r.students || []).length }));
      if ((r.skipped || []).length) partes.push(t('backup.todosOmitidos', { lista: r.skipped.join(', ') }));
      if ((r.failedCourse || []).length) partes.push(t('backup.todosFallidos', { lista: r.failedCourse.join(', ') }));
      setMsg(partes.join(' '));
    } catch (e: any) { setErr(e?.message || String(e)); }
    finally { setRestoringAll(false); }
  };

  const doRestoreSite = async () => {
    setConfirmSite(false);
    setRestoringSite(true); setMsg(''); setErr('');
    try {
      const r: SiteRestoreResult = await RestoreSite(siteUsers);
      let texto = t('backup.sitioRestaurado', {
        courses: r.courses, sharedFiles: r.sharedFiles, users: r.users, userFiles: r.userFiles,
      });
      if ((r.failed || []).length) texto += ' ' + t('backup.sitioFallos', { count: r.failed.length });
      setMsg(texto);
    } catch (e: any) { setErr(e?.message || String(e)); }
    finally { setRestoringSite(false); }
  };

  const escollendoCursos = !admin || !wholeSite;
  const canBackup = !escollendoCursos || selected.size > 0;
  const copiaDoSitio = manifest?.kind === 'site';

  return (
    <div className="flex flex-col gap-6">
      {msg && <p className="flex items-start gap-2 text-sm text-emerald-700 bg-emerald-50 border border-emerald-200 rounded-lg px-3 py-2"><CheckCircle2 className="w-4 h-4 mt-0.5 shrink-0" /><span className="break-all">{msg}</span></p>}
      {err && <p className="flex items-start gap-2 text-sm text-rose-700 bg-rose-50 border border-rose-200 rounded-lg px-3 py-2"><AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" /><span className="break-all">{err}</span></p>}

      {/* ---- Backup ---- */}
      <div className="flex flex-col gap-3">
        <h4 className="font-bold text-stone-800 flex items-center gap-2"><FolderDown className="w-4 h-4" /> {t('backup.titulo')}</h4>
        {admin ? (
          <>
            <p className="text-sm text-stone-500">{t('backup.descAdmin')}</p>
            <div className="flex flex-col gap-1 text-sm">
              <label className="flex items-center gap-2"><input type="radio" name="ambitoCopia" checked={wholeSite} onChange={() => setWholeSite(true)} className="accent-stone-900" /> {t('backup.modoSitio')}</label>
              <label className="flex items-center gap-2"><input type="radio" name="ambitoCopia" checked={!wholeSite} onChange={() => setWholeSite(false)} className="accent-stone-900" /> {t('backup.modoCursos')}</label>
            </div>
          </>
        ) : (
          <p className="text-sm text-stone-500">{t('backup.descProfesor')}</p>
        )}
        {escollendoCursos && (
          <>
            <div className="flex flex-col gap-1 max-h-44 overflow-y-auto border border-stone-200 rounded-lg p-2">
              {courses.length === 0 ? <p className="text-sm text-stone-400 p-1">{t('backup.semCursos')}</p> :
                courses.map(c => (
                  <label key={c.id} className="flex items-center gap-2 text-sm px-1 py-0.5">
                    <input type="checkbox" checked={selected.has(c.id)} onChange={() => toggle(c.id)} className="accent-stone-900" />
                    <span className="truncate">{c.name}</span>
                  </label>
                ))}
            </div>
          </>
        )}
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={includeUsers} onChange={e => setIncludeUsers(e.target.checked)} className="accent-stone-900" />
          {admin ? t('backup.incluirUsuariosAdmin') : t('backup.incluirAlumnado')}
        </label>
        {/* As contas non están nos ficheiros senón na base de datos de File Browser, así
            que ningunha restauración as devolve: a copia do sitio deixa ao seu carón o
            CSV que a alta masiva sabe ler. */}
        {admin && wholeSite && (
          <div className="flex flex-col gap-1 text-sm border border-stone-200 rounded-lg p-3 bg-stone-50">
            <span className="font-bold text-stone-700">{t('backup.csvAltasTitulo')}</span>
            <p className="text-stone-500">{t('backup.csvAltasDesc')}</p>
            <input type="text" value={contrasinalAltas} onChange={e => setContrasinalAltas(e.target.value)}
              placeholder={t('backup.csvAltasPlaceholder')}
              className="mt-1 w-full max-w-xs px-3 py-2 bg-white border border-stone-300 rounded-lg outline-none focus:ring-2 focus:ring-stone-400" />
            <p className="text-xs text-stone-400">{t('backup.csvAltasAviso')}</p>
          </div>
        )}
        <div>
          <button onClick={doBackup} disabled={backingUp || !canBackup}
            className="flex items-center gap-2 px-4 py-2 bg-stone-900 text-white rounded-lg text-sm font-medium hover:bg-stone-800 disabled:opacity-50">
            {backingUp ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />} {escollendoCursos ? t('backup.copiaCursos') : t('backup.copiaSitio')}
          </button>
        </div>
      </div>

      <hr className="border-stone-200" />

      {/* ---- Restore ---- */}
      <div className="flex flex-col gap-3">
        <h4 className="font-bold text-stone-800 flex items-center gap-2"><RotateCcw className="w-4 h-4" /> {admin ? t('backup.restaurarTituloAdmin') : t('backup.restaurarTitulo')}</h4>
        {/* O que se pode facer dise ANTES de cargar nada. Antes só se vía "Restaurar
            nun curso" e o resto aparecía despois de escoller o .zip, así que non
            había maneira de saber que existía a restauración completa. */}
        {admin ? (
          <div className="text-sm text-stone-500 flex flex-col gap-1">
            <p>{t('backup.restaurarIntroAdmin')}</p>
            <ul className="list-disc pl-5 flex flex-col gap-0.5">
              <li>{t('backup.restaurarOpcion1')}</li>
              <li>{t('backup.restaurarOpcion2')}</li>
              <li>{t('backup.restaurarOpcion3')}</li>
            </ul>
          </div>
        ) : (
          <p className="text-sm text-stone-500">{t('backup.restaurarDesc')}</p>
        )}
        <div>
          <button onClick={loadArchive} disabled={loadingArchive}
            className="flex items-center gap-2 px-4 py-2 bg-stone-100 border border-stone-300 text-stone-700 rounded-lg text-sm font-medium hover:bg-stone-200 disabled:opacity-50">
            {loadingArchive ? <Loader2 className="w-4 h-4 animate-spin" /> : <Upload className="w-4 h-4" />} {t('backup.cargarCopia')}
          </button>
        </div>

        {manifest && (manifest.courses || []).length > 0 && (
          <div className="flex flex-col gap-3 border border-stone-200 rounded-lg p-3 bg-stone-50">
            <h5 className="font-bold text-stone-800 text-sm flex items-center gap-2"><RotateCcw className="w-4 h-4" /> {t('backup.restaurarTitulo')}</h5>
            <p className="text-sm text-stone-600">{t('backup.restaurarDesc')}</p>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <label className="flex flex-col gap-1 text-sm">
                <span className="font-bold text-stone-700">{t('backup.cursoOrixe')}</span>
                <select value={sourceId} onChange={e => setSourceId(e.target.value)} className="bg-white border border-stone-300 rounded-lg p-2">
                  {(manifest.courses || []).map(c => <option key={c} value={c}>{c}</option>)}
                </select>
              </label>
              <label className="flex flex-col gap-1 text-sm">
                <span className="font-bold text-stone-700">{t('backup.cursoDestino')}</span>
                <select value={targetId} onChange={e => setTargetId(e.target.value)} className="bg-white border border-stone-300 rounded-lg p-2">
                  <option value="">{t('backup.escolle')}</option>
                  {courses.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
                </select>
              </label>
            </div>
            <div className="flex flex-col gap-1 text-sm">
              <label className="flex items-center gap-2"><input type="radio" name="withStudents" checked={withStudents} onChange={() => setWithStudents(true)} className="accent-stone-900" /> {t('backup.conDatosAlumnado')}</label>
              <label className="flex items-center gap-2"><input type="radio" name="withStudents" checked={!withStudents} onChange={() => setWithStudents(false)} className="accent-stone-900" /> {t('backup.soDatosCurso')}</label>
            </div>
            <div>
              <button onClick={doRestore} disabled={restoring || !targetId}
                className="flex items-center gap-2 px-4 py-2 bg-emerald-600 text-white rounded-lg text-sm font-medium hover:bg-emerald-700 disabled:opacity-50">
                {restoring ? <Loader2 className="w-4 h-4 animate-spin" /> : <RotateCcw className="w-4 h-4" />} {t('backup.restaurar')}
              </button>
            </div>
          </div>
        )}

        {/* Todos os cursos da copia dunha vez. Sen isto, a única saída para unha copia
            con moitos cursos era ir un por un, e só cara a cursos xa creados a man. */}
        {admin && manifest && (manifest.courses || []).length > 0 && (
          <div className="flex flex-col gap-3 border border-stone-300 rounded-lg p-3 bg-white">
            <h5 className="font-bold text-stone-800 text-sm flex items-center gap-2"><RotateCcw className="w-4 h-4" /> {t('backup.restaurarTodosTitulo')}</h5>
            <p className="text-sm text-stone-600">{t('backup.restaurarTodosDesc')}</p>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={allUsers} onChange={e => setAllUsers(e.target.checked)} className="accent-stone-900" />
              {t('backup.restaurarTodosUsuarios')}
            </label>
            {confirmAll ? (
              <div className="flex flex-col gap-2">
                <p className="flex items-start gap-2 text-sm text-stone-700"><AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" /><span>{t('backup.restaurarTodosAviso')}</span></p>
                <div className="flex gap-2">
                  <button onClick={doRestoreAll}
                    className="flex items-center gap-2 px-4 py-2 bg-emerald-600 text-white rounded-lg text-sm font-medium hover:bg-emerald-700">
                    <RotateCcw className="w-4 h-4" /> {t('backup.confirmar')}
                  </button>
                  <button onClick={() => setConfirmAll(false)}
                    className="px-4 py-2 text-sm font-medium text-stone-600 hover:bg-stone-100 rounded-lg">{t('backup.cancelar')}</button>
                </div>
              </div>
            ) : (
              <div>
                <button onClick={() => setConfirmAll(true)} disabled={restoringAll}
                  className="flex items-center gap-2 px-4 py-2 bg-emerald-600 text-white rounded-lg text-sm font-medium hover:bg-emerald-700 disabled:opacity-50">
                  {restoringAll ? <Loader2 className="w-4 h-4 animate-spin" /> : <RotateCcw className="w-4 h-4" />} {t('backup.restaurarTodosBoton', { count: (manifest.courses || []).length })}
                </button>
              </div>
            )}
          </div>
        )}

        {/* Restauración completa: só o Director, e só se a copia cargada é do sitio
            enteiro. Vai en dous pasos (botón → confirmación) porque escribe por riba
            de todo o servidor; iso si, non borra nada do que non veña na copia. */}
        {admin && copiaDoSitio && (
          <div className="flex flex-col gap-3 border border-amber-300 rounded-lg p-3 bg-amber-50">
            <h5 className="font-bold text-stone-800 text-sm flex items-center gap-2"><RotateCcw className="w-4 h-4" /> {t('backup.restaurarSitioTitulo')}</h5>
            <p className="text-sm text-stone-600">{t('backup.restaurarSitioDesc')}</p>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={siteUsers} onChange={e => setSiteUsers(e.target.checked)} className="accent-stone-900" />
              {t('backup.restaurarSitioUsuarios')}
            </label>
            {confirmSite ? (
              <div className="flex flex-col gap-2">
                <p className="flex items-start gap-2 text-sm text-amber-900"><AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" /><span>{t('backup.restaurarSitioAviso')}</span></p>
                <div className="flex gap-2">
                  <button onClick={doRestoreSite}
                    className="flex items-center gap-2 px-4 py-2 bg-amber-600 text-white rounded-lg text-sm font-medium hover:bg-amber-700">
                    <RotateCcw className="w-4 h-4" /> {t('backup.confirmar')}
                  </button>
                  <button onClick={() => setConfirmSite(false)}
                    className="px-4 py-2 text-sm font-medium text-stone-600 hover:bg-stone-100 rounded-lg">{t('backup.cancelar')}</button>
                </div>
              </div>
            ) : (
              <div>
                <button onClick={() => setConfirmSite(true)} disabled={restoringSite}
                  className="flex items-center gap-2 px-4 py-2 bg-amber-600 text-white rounded-lg text-sm font-medium hover:bg-amber-700 disabled:opacity-50">
                  {restoringSite ? <Loader2 className="w-4 h-4 animate-spin" /> : <RotateCcw className="w-4 h-4" />} {t('backup.restaurarSitioBoton')}
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
