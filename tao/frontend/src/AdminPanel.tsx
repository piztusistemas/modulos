import { useState, useEffect, useCallback, useMemo } from 'react';
import {
  ShieldCheck, Server, Building2, KeyRound, UserPlus, Upload, Users,
  Trash2, Loader2, LogOut, RefreshCw, CheckCircle2, AlertTriangle, Lock, GraduationCap,
  MessageSquare, Megaphone, Archive, BookOpen, Eraser, Search, Pencil,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  GetAdminConfig, SaveAdminConfig, GenerateCA, GetCAInfo,
  ListUsers, CreateUser, CreateUsersFromCSV, DeleteUser, DeleteUsersFromCSV, ChangeUserPassword, SetProfessor, ChangeUserGroup,
  GetMessagingConfig, SaveMessagingConfig, AdminListMaterias, AdminDeleteMateria, AdminSaveMateria,
  ApplyPrivacyRuleToAllStudents, ListarOrfos, PurgarOrfos,
} from '../bindings/tao/app';
import { NewUser, AdminConfig, CAInfo, AdminUser, MessagingConfig, MateriaDef, InformeOrfos } from '../bindings/tao/models';
import BackupRestore from './Backup';
import { gruposDe } from './grupos';
import { planCambioGrupo, migracions, normalizarGrupo } from './cambioGrupo';
import PasswordInput from './PasswordInput';

type Banner = { kind: 'ok' | 'err'; text: string } | null;

const emptyNewUser = (): NewUser =>
  ({ username: '', password: '', firstname: '', lastname: '', email: '', city: '', department: '', group: '' } as NewUser);

export default function AdminPanel({ onLogout }: { onLogout: () => void }) {
  const { t } = useTranslation();
  const [cfg, setCfg] = useState<AdminConfig | null>(null);
  const [caInfo, setCaInfo] = useState<CAInfo | null>(null);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [msgCfg, setMsgCfg] = useState<MessagingConfig | null>(null);
  const [materias, setMaterias] = useState<MateriaDef[]>([]);
  const [orfos, setOrfos] = useState<InformeOrfos | null>(null);
  const [banner, setBanner] = useState<Banner>(null);
  const [busy, setBusy] = useState<string>(''); // id of the running action, '' when idle
  // In-app confirmation dialog: the webview's native confirm() is unreliable in Wails,
  // so destructive actions go through this React modal instead. confirmLabel turns it
  // into a non-destructive confirmation (no red "Eliminar" button).
  const [confirmReq, setConfirmReq] = useState<{ text: string; onYes: () => void; confirmLabel?: string } | null>(null);
  const askConfirm = (text: string, onYes: () => void, confirmLabel?: string) => setConfirmReq({ text, onYes, confirmLabel });

  const flash = (kind: 'ok' | 'err', text: string) => setBanner({ kind, text });

  const refreshUsers = useCallback(async () => {
    try {
      setUsers((await ListUsers()) || []);
    } catch (e: any) {
      flash('err', t('adminPanel.erroListarUsuarios', { error: errText(e) }));
    }
  }, []);

  const refreshMaterias = useCallback(async () => {
    try {
      setMaterias((await AdminListMaterias()) || []);
    } catch (e: any) {
      flash('err', t('adminPanel.erroListarCursos', { error: errText(e) }));
    }
  }, []);

  useEffect(() => {
    (async () => {
      try {
        setCfg(await GetAdminConfig());
        setCaInfo(await GetCAInfo());
        setMsgCfg(await GetMessagingConfig());
        refreshUsers(); // load the roster so the messaging controls list groups/users
        refreshMaterias();
      } catch (e: any) {
        flash('err', errText(e));
      }
    })();
  }, [refreshUsers, refreshMaterias]);

  useEffect(() => { if (banner) { const t = setTimeout(() => setBanner(null), 6000); return () => clearTimeout(t); } }, [banner]);

  const run = async (id: string, fn: () => Promise<void>) => {
    setBusy(id);
    try { await fn(); } catch (e: any) { flash('err', errText(e)); } finally { setBusy(''); }
  };

  return (
    <div className="min-h-screen bg-stone-100 font-sans text-stone-900 pb-16">
      {/* Header */}
      <header className="bg-stone-900 text-white sticky top-0 z-30 shadow-md">
        <div className="max-w-5xl mx-auto px-4 sm:px-6 py-4 flex items-center justify-between gap-4">
          <div className="flex items-center gap-3">
            <div className="p-2 bg-white/10 rounded-xl"><ShieldCheck className="w-6 h-6" /></div>
            <div>
              <h1 className="text-lg font-bold leading-tight">{t('adminPanel.administracion')}</h1>
              <p className="text-xs text-stone-300">{t('adminPanel.subtitulo')}</p>
            </div>
          </div>
          <button onClick={onLogout} className="flex items-center gap-2 px-3 py-2 bg-white/10 hover:bg-white/20 rounded-lg text-sm font-medium transition-colors">
            <LogOut className="w-4 h-4" /> {t('adminPanel.sair')}
          </button>
        </div>
      </header>

      {confirmReq && (
        <ConfirmModal
          text={confirmReq.text}
          confirmLabel={confirmReq.confirmLabel}
          onCancel={() => setConfirmReq(null)}
          onConfirm={() => { const f = confirmReq.onYes; setConfirmReq(null); f(); }}
        />
      )}

      <main className="max-w-5xl mx-auto px-4 sm:px-6 py-6 flex flex-col gap-6">
        {banner && (
          <div className={`flex items-start gap-2 rounded-lg px-4 py-3 text-sm border ${
            banner.kind === 'ok' ? 'bg-emerald-50 border-emerald-200 text-emerald-800' : 'bg-rose-50 border-rose-200 text-rose-800'}`}>
            {banner.kind === 'ok' ? <CheckCircle2 className="w-4 h-4 mt-0.5 shrink-0" /> : <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />}
            <span className="whitespace-pre-wrap">{banner.text}</span>
          </div>
        )}

        {cfg && (
          <ConfigSection
            cfg={cfg} setCfg={setCfg} caInfo={caInfo} busy={busy}
            onSave={() => run('save', async () => { await SaveAdminConfig(cfg); flash('ok', t('adminPanel.configuracionGardada')); })}
            onGenerateCA={() => run('ca', async () => {
              await SaveAdminConfig(cfg);
              setCaInfo(await GenerateCA());
              flash('ok', t('adminPanel.caXeradaMensaxe'));
            })}
          />
        )}

        <SingleUserSection
          busy={busy}
          onCreate={(u, reset) => run('create', async () => {
            await CreateUser(u);
            flash('ok', t('adminPanel.usuarioCreado', { username: u.username }));
            reset();
            refreshUsers();
          })}
        />

        <CsvSection
          busy={busy}
          onImport={(text) => run('csv', async () => {
            const res = await CreateUsersFromCSV(text);
            const errs = (res.errors || []);
            flash(errs.length ? 'err' : 'ok',
              t('adminPanel.usuariosCreadosResumo', { created: res.created, total: res.total }) + (errs.length ? t('adminPanel.listaErros', { lista: errs.join('\n• ') }) : ''));
            refreshUsers();
          })}
        />

        <CsvDeleteSection
          busy={busy}
          onDelete={(text, count) => askConfirm(
            t('adminPanel.confirmarEliminarUsuariosCsv', { count }),
            () => run('csvdel', async () => {
              const res = await DeleteUsersFromCSV(text);
              const errs = (res.errors || []);
              flash(errs.length ? 'err' : 'ok',
                t('adminPanel.usuariosEliminadosResumo', { created: res.created, total: res.total }) + (errs.length ? t('adminPanel.listaAvisos', { lista: errs.join('\n• ') }) : ''));
              refreshUsers();
            }),
          )}
        />

        <UsersSection
          users={users} busy={busy} onRefresh={refreshUsers} askConfirm={askConfirm}
          onDelete={(name) => run('del:' + name, async () => {
            await DeleteUser(name);
            flash('ok', t('adminPanel.usuarioEliminado', { name }));
            refreshUsers();
          })}
          onChangePassword={(name, pass) => run('pwd:' + name, async () => {
            await ChangeUserPassword(name, pass);
            flash('ok', t('adminPanel.contrasinalCambiado', { name }));
          })}
          onToggleProfessor={(name, isProf) => run('prof:' + name, async () => {
            await SetProfessor(name, isProf);
            flash('ok', isProf ? t('adminPanel.agoraProfesor', { name }) : t('adminPanel.rolProfesorRetirado', { name }));
            refreshUsers();
          })}
          onChangeGroup={(u, novoCampo) => {
            const name = u.username, vello = u.group || '', novo = normalizarGrupo(novoCampo);
            if (!novo || novo.toLowerCase() === normalizarGrupo(vello).toLowerCase()) return;
            run('grp:' + name, async () => {
              // As materias reléense agora: o plan ten que saír da programación vixente.
              const todas = (await AdminListMaterias()) || [];
              setMaterias(todas);
              const plan = planCambioGrupo(todas, vello, novo);
              const lista = (xs: string[]) => xs.map(x => '\n• ' + x).join('');
              let texto = t('adminPanel.confirmarCambioGrupo', { name, vello: vello || '—', novo });
              if (plan.pares.length) {
                texto += '\n\n' + t('adminPanel.cambioGrupoCopiara') + lista(plan.pares.map(p =>
                  `${p.from.name} (${(p.from.groups || []).join(', ')} → ${(p.to.groups || []).join(', ')})`));
              }
              if (plan.senPar.length) {
                texto += '\n\n' + t('adminPanel.cambioGrupoSenPar') + lista(plan.senPar.map(m => m.name));
              }
              texto += '\n\n' + t('adminPanel.cambioGrupoAviso');
              askConfirm(texto, () => run('grp:' + name, async () => {
                const res = await ChangeUserGroup(name, novo, migracions(plan));
                const avisos = res?.avisos || [];
                flash(avisos.length ? 'err' : 'ok',
                  t('adminPanel.grupoCambiado', {
                    name, novo, materias: res?.materias ?? 0, rexistros: res?.rexistros ?? 0, ficheiros: res?.ficheiros ?? 0,
                  }) + (avisos.length ? t('adminPanel.listaAvisos', { lista: avisos.join('\n• ') }) : ''));
                refreshUsers();
              }), t('adminPanel.cambiarGrupoBoton'));
            });
          }}
        />

        <PurgaOrfosSection
          informe={orfos} busy={busy} askConfirm={askConfirm}
          onScan={() => run('orfos-scan', async () => {
            const inf = await ListarOrfos();
            setOrfos(inf);
          })}
          onPurge={(incluirCartafoles) => run('orfos-purga', async () => {
            const inf = await PurgarOrfos(incluirCartafoles);
            setOrfos(null);
            flash('ok', t('adminPanel.orfosPurgados', { count: inf?.total ?? 0 }));
            refreshUsers();
          })}
        />

        {msgCfg && (
          <MessagingSection
            cfg={msgCfg} setCfg={setMsgCfg} users={users} busy={busy}
            onSave={() => run('msgcfg', async () => { await SaveMessagingConfig(msgCfg); flash('ok', t('adminPanel.configMensaxeriaGardada')); })}
          />
        )}

        <CoursesBackupSection
          materias={materias} busy={busy} onRefresh={refreshMaterias} askConfirm={askConfirm}
          onDelete={(id) => run('mat:' + id, async () => {
            await AdminDeleteMateria(id);
            flash('ok', t('adminPanel.cursoEliminado', { id }));
            refreshMaterias();
          })}
          onReassignOwner={(m, owner) => run('mat-owner:' + m.id, async () => {
            await AdminSaveMateria(MateriaDef.createFrom({ ...m, owner }));
            flash('ok', t('adminPanel.propietarioReasignado', { name: m.name }));
            refreshMaterias();
          })}
        />

        <PrivacySection busy={busy} onApply={() => run('privacy-rule', async () => {
          const count = await ApplyPrivacyRuleToAllStudents();
          flash('ok', t('adminPanel.regraPrivacidadeAplicada', { count }));
        })} />
      </main>
    </div>
  );
}

// ---- Cursos e copias de seguridade (admin) ------------------------------------
// Un administrador ve TODAS as materias (calquera profesor) e pode reasignar o dono
// (MateriaDef.Owner, tao/profesor.go) ou eliminala — a diferenza dun profesor
// calquera, que só ve/edita as súas propias (ListMyMaterias, App.tsx). Editar o
// contido (calendario, unidades) non está dispoñible aquí de momento: só propiedade
// e borrado, coma xa era o caso para o borrado antes deste cambio.
function CoursesBackupSection({ materias, busy, onRefresh, onDelete, onReassignOwner, askConfirm }: {
  materias: MateriaDef[]; busy: string; onRefresh: () => void;
  onDelete: (id: string) => void; onReassignOwner: (m: MateriaDef, owner: string) => void;
  askConfirm: (text: string, onYes: () => void) => void;
}) {
  const { t } = useTranslation();
  const [ownerEdits, setOwnerEdits] = useState<Record<string, string>>({});
  return (
    <Card icon={<Archive className="w-5 h-5" />} title={t('adminPanel.cursosECopiasTitulo')}
      action={<button onClick={onRefresh} disabled={!!busy} className="flex items-center gap-1.5 text-sm text-stone-600 hover:text-stone-900 disabled:opacity-60"><RefreshCw className="w-4 h-4" /> {t('adminPanel.actualizar')}</button>}>
      <div className="flex flex-col gap-6">
        <div className="flex flex-col gap-2">
          <h4 className="font-bold text-stone-800 flex items-center gap-2"><BookOpen className="w-4 h-4" /> {t('adminPanel.cursosPublicados')}</h4>
          {materias.length === 0 ? <p className="text-sm text-stone-400">{t('adminPanel.semCursosPublicados')}</p> : (
            <div className="border border-stone-200 rounded-lg divide-y divide-stone-100">
              {materias.map(m => {
                const editing = ownerEdits[m.id] ?? m.owner ?? '';
                const busyOwner = busy === 'mat-owner:' + m.id;
                return (
                  <div key={m.id} className="p-2 flex items-center gap-2">
                    <BookOpen className="w-4 h-4 text-stone-400 shrink-0" />
                    <span className="flex-1 min-w-0 truncate text-sm text-stone-800">{m.name} <span className="text-stone-400">({m.academicYear})</span></span>
                    <input
                      type="text"
                      value={editing}
                      onChange={(e) => setOwnerEdits(prev => ({ ...prev, [m.id]: e.target.value }))}
                      placeholder={t('adminPanel.propietarioSenAsignar')}
                      title={t('adminPanel.propietarioTitulo')}
                      className="w-36 shrink-0 px-2 py-1 text-xs rounded-lg border border-stone-200"
                    />
                    <button
                      onClick={() => onReassignOwner(m, editing.trim())}
                      disabled={busyOwner || editing.trim() === (m.owner || '')}
                      title={t('adminPanel.reasignarPropietarioTooltip')}
                      className="p-1.5 text-stone-400 hover:text-emerald-600 rounded hover:bg-emerald-50 transition-colors disabled:opacity-30"
                    >
                      {busyOwner ? <Loader2 className="w-4 h-4 animate-spin" /> : <CheckCircle2 className="w-4 h-4" />}
                    </button>
                    <button onClick={() => askConfirm(t('adminPanel.confirmarEliminarCurso', { name: m.name }), () => onDelete(m.id))}
                      disabled={busy === 'mat:' + m.id}
                      className="p-1.5 text-stone-400 hover:text-rose-600 rounded hover:bg-rose-50 transition-colors disabled:opacity-50" title={t('adminPanel.eliminarCursoTooltip')}>
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
                );
              })}
            </div>
          )}
        </div>
        <hr className="border-stone-200" />
        <BackupRestore role="admin" courses={materias.map(m => ({ id: m.id, name: `${m.name} (${m.academicYear})` }))} />
      </div>
    </Card>
  );
}

// ---- Privacidade das notas privadas (regra de File Browser) --------------------
// Aplica a regra de denegación de notas-privadas/ (privacidade.go) a todas as contas
// de alumnado xa existentes — as novas xa a reciben soas ao crearse (fbCreateUser).
// Acción manual e explícita a propósito: o admin decide cando executala, non se
// dispara soa. Ver o aviso de privacidade no código Go (privacidade.go) sobre o que
// isto garante e o que NON se chegou a verificar contra un servidor real.
function PrivacySection({ busy, onApply }: { busy: string; onApply: () => void }) {
  const { t } = useTranslation();
  const applying = busy === 'privacy-rule';
  return (
    <Card icon={<Lock className="w-5 h-5" />} title={t('adminPanel.privacidadeTitulo')}>
      <p className="text-sm text-stone-600 mb-3">{t('adminPanel.privacidadeDescricion')}</p>
      <button onClick={onApply} disabled={applying}
        className="flex items-center gap-2 px-4 py-2 bg-stone-900 text-white font-medium hover:bg-stone-800 rounded-lg transition-colors disabled:opacity-50">
        {applying ? <Loader2 className="w-4 h-4 animate-spin" /> : <Lock className="w-4 h-4" />}
        {t('adminPanel.privacidadeBoton')}
      </button>
    </Card>
  );
}

// ---- Purga de rexistros orfos ---------------------------------------------------
// Unha conta borrada fóra de Tao (web de File Browser, CLI...) deixa a súa ficha no
// roster compartido, así que o alumnado segue aparecendo na lista do profesorado. Esta
// sección busca eses rexistros sen conta e permite eliminalos. Sempre en dous pasos:
// primeiro amósase o que se atopou, e só despois se pode purgar.
function PurgaOrfosSection({ informe, busy, askConfirm, onScan, onPurge }: {
  informe: InformeOrfos | null; busy: string;
  askConfirm: (text: string, onYes: () => void) => void;
  onScan: () => void; onPurge: (incluirCartafoles: boolean) => void;
}) {
  const { t } = useTranslation();
  const [incluirCartafoles, setIncluirCartafoles] = useState(false);
  const scanning = busy === 'orfos-scan';
  const purging = busy === 'orfos-purga';

  // Cada grupo é un sitio distinto onde queda a ficha; amosámolos por separado para que
  // se vexa que se vai tocar antes de premer nada.
  const grupos: { titulo: string; vals: string[] }[] = informe ? ([
    { titulo: t('adminPanel.orfosRoster'), vals: informe.roster || [] },
    { titulo: t('adminPanel.orfosUuid'), vals: informe.uuid || [] },
    { titulo: t('adminPanel.orfosProfesores'), vals: informe.profesores || [] },
    { titulo: t('adminPanel.orfosPerfisLocais'), vals: informe.perfisLocais || [] },
    { titulo: t('adminPanel.orfosSesions'), vals: informe.sesions || [] },
    { titulo: t('adminPanel.orfosCartafoles'), vals: informe.cartafoles || [] },
  ].filter(g => g.vals.length > 0)) : [];
  // O total do backend inclúe os cartafoles; a purga só os borra se está marcada a
  // opción, así que o número que se confirma ten que reflectir o que se vai facer.
  const aEliminar = informe ? informe.total - (incluirCartafoles ? 0 : (informe.cartafoles || []).length) : 0;

  return (
    <Card icon={<Eraser className="w-5 h-5" />} title={t('adminPanel.orfosTitulo')}
      action={<button onClick={onScan} disabled={!!busy}
        className="flex items-center gap-1.5 text-sm text-stone-600 hover:text-stone-900 disabled:opacity-60">
        {scanning ? <Loader2 className="w-4 h-4 animate-spin" /> : <Search className="w-4 h-4" />} {t('adminPanel.orfosBotonBuscar')}
      </button>}>
      <p className="text-sm text-stone-600 mb-3">{t('adminPanel.orfosDescricion')}</p>

      {informe && (
        <div className="flex flex-col gap-3">
          <p className="text-sm text-stone-500">{t('adminPanel.orfosResumo', { contas: informe.contas, total: informe.total })}</p>

          {informe.total === 0 ? (
            <p className="flex items-center gap-2 text-sm text-emerald-700"><CheckCircle2 className="w-4 h-4" /> {t('adminPanel.orfosNada')}</p>
          ) : (
            <>
              {grupos.map(g => (
                <div key={g.titulo}>
                  <p className="text-xs uppercase font-semibold text-stone-400 mb-1">{g.titulo}</p>
                  <div className="flex flex-wrap gap-1.5">
                    {g.vals.map(v => <span key={v} className="text-xs bg-stone-100 text-stone-700 rounded px-2 py-0.5">{v}</span>)}
                  </div>
                </div>
              ))}

              <label className="flex items-start gap-2 text-sm text-stone-700 mt-1">
                <input type="checkbox" checked={incluirCartafoles} onChange={e => setIncluirCartafoles(e.target.checked)} className="mt-0.5" />
                <span>{t('adminPanel.orfosIncluirCartafoles')}</span>
              </label>
              {!incluirCartafoles && <p className="text-xs text-stone-400 -mt-2">{t('adminPanel.orfosAviso')}</p>}

              <button
                onClick={() => askConfirm(t('adminPanel.confirmarPurgarOrfos', { count: aEliminar }), () => onPurge(incluirCartafoles))}
                disabled={!!busy || aEliminar === 0}
                className="self-start flex items-center gap-2 px-4 py-2 bg-rose-600 text-white font-medium hover:bg-rose-700 rounded-lg transition-colors disabled:opacity-50">
                {purging ? <Loader2 className="w-4 h-4 animate-spin" /> : <Trash2 className="w-4 h-4" />}
                {t('adminPanel.orfosBotonPurgar')}
              </button>
            </>
          )}
        </div>
      )}
    </Card>
  );
}

// ---- Mensaxería: activar/desactivar muro e 1:1, e bloquear grupos/persoas -------
function MessagingSection({ cfg, setCfg, users, busy, onSave }: {
  cfg: MessagingConfig; setCfg: (c: MessagingConfig) => void;
  users: AdminUser[]; busy: string; onSave: () => void;
}) {
  const { t } = useTranslation();
  // Distinct groups present in the roster, and the students (non-admin accounts).
  const groups = Array.from(new Set(users.flatMap(u => gruposDe(u.group)))).sort();
  const students = users.filter(u => !u.isAdmin && !u.isProfessor).sort((a, b) => a.username.localeCompare(b.username));

  const toggle = (list: string[] | undefined, val: string): string[] => {
    const cur = list || [];
    return cur.some(x => x.toLowerCase() === val.toLowerCase())
      ? cur.filter(x => x.toLowerCase() !== val.toLowerCase())
      : [...cur, val];
  };
  const has = (list: string[] | undefined, val: string) => (list || []).some(x => x.toLowerCase() === val.toLowerCase());
  const set = (patch: Partial<MessagingConfig>) => setCfg(MessagingConfig.createFrom({ ...cfg, ...patch }));

  return (
    <Card icon={<MessageSquare className="w-5 h-5" />} title={t('adminPanel.mensaxeriaTitulo')}
      action={<button onClick={onSave} disabled={!!busy} className="flex items-center gap-2 px-4 py-2 text-sm font-medium bg-stone-900 text-white rounded-lg hover:bg-stone-800 disabled:opacity-60">{busy === 'msgcfg' ? <Loader2 className="w-4 h-4 animate-spin" /> : null} {t('adminPanel.gardarMensaxeria')}</button>}>
      <p className="text-sm text-stone-500 mb-4">{t('adminPanel.mensaxeriaDescricion')}</p>

      <div className="flex flex-col gap-3 mb-5">
        <label className="flex items-center gap-3 text-sm">
          <input type="checkbox" checked={!!cfg.wallEnabled} onChange={e => set({ wallEnabled: e.target.checked })} className="accent-stone-900 w-4 h-4" />
          <Megaphone className="w-4 h-4 text-stone-400" />
          <span><b>{t('adminPanel.muroGrupoTitulo')}</b> — {t('adminPanel.muroGrupoDesc')}</span>
        </label>
        <label className="flex items-center gap-3 text-sm">
          <input type="checkbox" checked={!!cfg.peerEnabled} onChange={e => set({ peerEnabled: e.target.checked })} className="accent-stone-900 w-4 h-4" />
          <Users className="w-4 h-4 text-stone-400" />
          <span><b>{t('adminPanel.mensaxesPrivadasTitulo')}</b> — {t('adminPanel.mensaxesPrivadasDesc')}</span>
        </label>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-5">
        <div>
          <h4 className="text-sm font-bold text-stone-700 mb-2">{t('adminPanel.gruposDesactivados')}</h4>
          {groups.length === 0 ? <p className="text-sm text-stone-400">{t('adminPanel.semGrupos')}</p> : (
            <div className="flex flex-col gap-1 max-h-52 overflow-y-auto border border-stone-200 rounded-lg p-2">
              {groups.map(g => (
                <label key={g} className="flex items-center gap-2 text-sm px-1 py-0.5">
                  <input type="checkbox" checked={has(cfg.disabledGroups, g)} onChange={() => set({ disabledGroups: toggle(cfg.disabledGroups, g) })} className="accent-rose-600" />
                  {g}
                </label>
              ))}
            </div>
          )}
        </div>
        <div>
          <h4 className="text-sm font-bold text-stone-700 mb-2">{t('adminPanel.persoasDesactivadas')}</h4>
          {students.length === 0 ? <p className="text-sm text-stone-400">{t('adminPanel.semUsuariosActualiza')}</p> : (
            <div className="flex flex-col gap-1 max-h-52 overflow-y-auto border border-stone-200 rounded-lg p-2">
              {students.map(u => (
                <label key={u.username} className="flex items-center gap-2 text-sm px-1 py-0.5">
                  <input type="checkbox" checked={has(cfg.disabledUsers, u.username)} onChange={() => set({ disabledUsers: toggle(cfg.disabledUsers, u.username) })} className="accent-rose-600" />
                  <span className="truncate">{u.username}{u.group ? ` · ${u.group}` : ''}</span>
                </label>
              ))}
            </div>
          )}
        </div>
      </div>
    </Card>
  );
}

// ---- Configuración: servidor, centro/departamento, CA, credenciais admin FB ----

function ConfigSection({ cfg, setCfg, caInfo, busy, onSave, onGenerateCA }: {
  cfg: AdminConfig; setCfg: (c: AdminConfig) => void; caInfo: CAInfo | null;
  busy: string; onSave: () => void; onGenerateCA: () => void;
}) {
  const { t } = useTranslation();
  const set = (k: keyof AdminConfig, v: string) => setCfg({ ...cfg, [k]: v } as AdminConfig);
  return (
    <Card icon={<Server className="w-5 h-5" />} title={t('adminPanel.servidorECaTitulo')}>
      <div className="grid sm:grid-cols-2 gap-4">
        <Field label={t('adminPanel.servidorFicheirosLabel')} className="sm:col-span-2">
          <input className={inputCls} value={cfg.serverURL} onChange={e => set('serverURL', e.target.value)} placeholder="http://127.0.0.1:8080/arquivos" />
        </Field>
        <Field label={t('adminPanel.nomeCentroLabel')}>
          <input className={inputCls} value={cfg.center} onChange={e => set('center', e.target.value)} placeholder={t('adminPanel.nomeCentroLabel')} />
        </Field>
        <Field label={t('adminPanel.departamentoLabel')}>
          <input className={inputCls} value={cfg.department} onChange={e => set('department', e.target.value)} placeholder={t('adminPanel.departamentoPlaceholder')} />
        </Field>
      </div>

      <div className="mt-4 pt-4 border-t border-stone-100">
        <div className="flex items-center gap-2 text-sm font-bold text-stone-700 mb-2">
          <KeyRound className="w-4 h-4" /> {t('adminPanel.adminFileBrowserTitulo')}
        </div>
        <p className="text-xs text-stone-400 mb-3">{t('adminPanel.adminFileBrowserDesc')}</p>
        <div className="grid sm:grid-cols-2 gap-4">
          <Field label={t('adminPanel.usuarioAdminLabel')}>
            <input className={inputCls} value={cfg.fbAdminUser} onChange={e => set('fbAdminUser', e.target.value)} autoCapitalize="none" />
          </Field>
          <Field label={t('adminPanel.contrasinalAdminLabel')}>
            <PasswordInput className={inputCls} value={cfg.fbAdminPass} onChange={e => set('fbAdminPass', e.target.value)} placeholder="••••••••" />
          </Field>
        </div>
      </div>

      <div className="mt-4 pt-4 border-t border-stone-100 flex flex-wrap items-center gap-3">
        <button onClick={onSave} disabled={!!busy}
          className="flex items-center gap-2 px-4 py-2 bg-stone-900 text-white rounded-lg text-sm font-medium hover:bg-stone-800 disabled:opacity-60">
          {busy === 'save' ? <Loader2 className="w-4 h-4 animate-spin" /> : <CheckCircle2 className="w-4 h-4" />} {t('adminPanel.gardarConfiguracion')}
        </button>
        <button onClick={onGenerateCA} disabled={!!busy}
          className="flex items-center gap-2 px-4 py-2 bg-amber-600 text-white rounded-lg text-sm font-medium hover:bg-amber-700 disabled:opacity-60">
          {busy === 'ca' ? <Loader2 className="w-4 h-4 animate-spin" /> : <ShieldCheck className="w-4 h-4" />} {t('adminPanel.xerarCa')}
        </button>
        <div className="text-xs text-stone-500">
          {caInfo?.exists
            ? <span className="inline-flex items-center gap-1 text-emerald-700"><ShieldCheck className="w-3.5 h-3.5" /> {t('adminPanel.caEstado', { estado: caInfo.local ? t('adminPanel.caXerada') : t('adminPanel.caEmbebida'), subject: caInfo.subject })}</span>
            : <span className="text-amber-600">{t('adminPanel.semCaDisponible')}</span>}
        </div>
      </div>
      {caInfo?.exists && (
        <p className="mt-2 text-xs text-amber-600 flex items-center gap-1">
          <AlertTriangle className="w-3.5 h-3.5" /> {t('adminPanel.avisoXerarCaNova')}
        </p>
      )}
    </Card>
  );
}

// ---- Alta individual -----------------------------------------------------------

function SingleUserSection({ busy, onCreate }: { busy: string; onCreate: (u: NewUser, reset: () => void) => void }) {
  const { t } = useTranslation();
  const [u, setU] = useState<NewUser>(emptyNewUser());
  const set = (k: keyof NewUser, v: string) => setU({ ...u, [k]: v } as NewUser);
  const reset = () => setU(emptyNewUser());
  return (
    <Card icon={<UserPlus className="w-5 h-5" />} title={t('adminPanel.darDeAltaTitulo')}>
      <form onSubmit={e => { e.preventDefault(); onCreate(u, reset); }} className="grid sm:grid-cols-2 gap-4">
        <Field label={t('adminPanel.usuarioLabel')}><input className={inputCls} value={u.username} onChange={e => set('username', e.target.value)} autoCapitalize="none" required /></Field>
        <Field label={t('adminPanel.contrasinalLabel')}><input className={inputCls} value={u.password} onChange={e => set('password', e.target.value)} minLength={12} required /></Field>
        <Field label={t('adminPanel.nomeLabel')}><input className={inputCls} value={u.firstname} onChange={e => set('firstname', e.target.value)} required /></Field>
        <Field label={t('adminPanel.apelidosLabel')}><input className={inputCls} value={u.lastname} onChange={e => set('lastname', e.target.value)} required /></Field>
        <Field label={t('adminPanel.correoLabel')}><input className={inputCls} value={u.email} onChange={e => set('email', e.target.value)} /></Field>
        <Field label={t('adminPanel.localidadeLabel')}><input className={inputCls} value={u.city} onChange={e => set('city', e.target.value)} /></Field>
        <Field label={t('adminPanel.grupoLabel')}><input className={inputCls} value={u.group} onChange={e => set('group', e.target.value)} /></Field>
        <div className="flex items-end">
          <button type="submit" disabled={!!busy}
            className="flex items-center gap-2 px-4 py-2 bg-emerald-600 text-white rounded-lg text-sm font-medium hover:bg-emerald-700 disabled:opacity-60">
            {busy === 'create' ? <Loader2 className="w-4 h-4 animate-spin" /> : <UserPlus className="w-4 h-4" />} {t('adminPanel.crearEMatricular')}
          </button>
        </div>
      </form>
      <p className="mt-3 text-xs text-stone-400">{t('adminPanel.crearContaDesc')}</p>
    </Card>
  );
}

// ---- Importar CSV --------------------------------------------------------------

function CsvSection({ busy, onImport }: { busy: string; onImport: (text: string) => void }) {
  const { t } = useTranslation();
  const [fileName, setFileName] = useState('');
  const [text, setText] = useState('');
  const [rows, setRows] = useState(0);
  const onFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    e.target.value = ''; // permite reimportar o mesmo ficheiro
    if (!f) return;
    const content = await f.text();
    setFileName(f.name);
    setText(content);
    const dataLines = content.split(/\r?\n/).filter(l => l.trim()); // liñas non baleiras
    setRows(Math.max(0, dataLines.length - 1)); // menos a cabeceira
  };
  return (
    <Card icon={<Upload className="w-5 h-5" />} title={t('adminPanel.importarCsvTitulo')}>
      <p className="text-xs text-stone-500 mb-3">
        {t('adminPanel.columnasLabel')} <code className="text-stone-700">username, password, firstname, lastname, email, city, departament, profile_field_grupo</code>.
        {' '}{t('adminPanel.importarCsvDesc')}
      </p>
      <div className="flex flex-wrap items-center gap-3">
        <label className={`inline-flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium cursor-pointer ${busy ? 'bg-stone-100 text-stone-400 pointer-events-none' : 'bg-stone-100 text-stone-700 hover:bg-stone-200'}`}>
          <Upload className="w-4 h-4" /> {t('adminPanel.escollerFicheiroCsv')}
          <input type="file" accept=".csv,text/csv" onChange={onFile} className="hidden" disabled={!!busy} />
        </label>
        {fileName && <span className="text-xs text-stone-500">{fileName} · {t('adminPanel.usuariosParenteses', { count: rows })}</span>}
        <button
          onClick={() => onImport(text)}
          disabled={!text || !!busy}
          className="flex items-center gap-2 px-4 py-2 bg-emerald-600 text-white rounded-lg text-sm font-medium hover:bg-emerald-700 disabled:opacity-50">
          {busy === 'csv' ? <Loader2 className="w-4 h-4 animate-spin" /> : <UserPlus className="w-4 h-4" />}
          {rows > 0 ? t('adminPanel.crearEMatricularCount', { count: rows }) : t('adminPanel.crearEMatricular')}
        </button>
      </div>
    </Card>
  );
}

// ---- Eliminar usuarios desde CSV -----------------------------------------------

function CsvDeleteSection({ busy, onDelete }: { busy: string; onDelete: (text: string, count: number) => void }) {
  const { t } = useTranslation();
  const [fileName, setFileName] = useState('');
  const [text, setText] = useState('');
  const [rows, setRows] = useState(0);
  const onFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    e.target.value = ''; // permite reutilizar o mesmo ficheiro
    if (!f) return;
    const content = await f.text();
    setFileName(f.name);
    setText(content);
    const dataLines = content.split(/\r?\n/).filter(l => l.trim()); // liñas non baleiras
    setRows(Math.max(0, dataLines.length - 1)); // menos a cabeceira
  };
  return (
    <Card icon={<Trash2 className="w-5 h-5" />} title={t('adminPanel.eliminarUsuariosCsvTitulo')}>
      <p className="text-xs text-stone-500 mb-3">
        {t('adminPanel.eliminarCsvDescIntro')} <code className="text-stone-700">username</code>{t('adminPanel.eliminarCsvDescIntroFin')}
        {' '}{t('adminPanel.eliminarCsvDescCorpo1')} <strong>{t('adminPanel.eliminarCsvDescCartafol')}</strong> <code className="text-stone-700">users/&lt;usuario&gt;</code>{t('adminPanel.eliminarCsvDescCorpo2')}
      </p>
      <div className="flex flex-wrap items-center gap-3">
        <label className={`inline-flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium cursor-pointer ${busy ? 'bg-stone-100 text-stone-400 pointer-events-none' : 'bg-stone-100 text-stone-700 hover:bg-stone-200'}`}>
          <Upload className="w-4 h-4" /> {t('adminPanel.escollerFicheiroCsv')}
          <input type="file" accept=".csv,text/csv" onChange={onFile} className="hidden" disabled={!!busy} />
        </label>
        {fileName && <span className="text-xs text-stone-500">{fileName} · {t('adminPanel.usuariosParenteses', { count: rows })}</span>}
        <button
          onClick={() => onDelete(text, rows)}
          disabled={!text || !!busy}
          className="flex items-center gap-2 px-4 py-2 bg-rose-600 text-white rounded-lg text-sm font-medium hover:bg-rose-700 disabled:opacity-50">
          {busy === 'csvdel' ? <Loader2 className="w-4 h-4 animate-spin" /> : <Trash2 className="w-4 h-4" />}
          {rows > 0 ? t('adminPanel.eliminarDoServidorCount', { count: rows }) : t('adminPanel.eliminarDoServidor')}
        </button>
      </div>
    </Card>
  );
}

// ---- Táboa de usuarios ---------------------------------------------------------

// normalizarBusca pon en minúsculas e quita os acentos, para que buscar "perez"
// atope "Pérez" (e "bac1a" atope "Bac1A"). Úsase o rango combinante de Unicode
// en vez de \p{Diacritic} porque este webview non garante o segundo.
function normalizarBusca(s: string): string {
  return (s || '').normalize('NFD').replace(/[\u0300-\u036f]/g, '').toLowerCase();
}

// coincideBusca di se un usuario casa coa consulta. A consulta pártese en
// termos: "ana bac1a" esixe os dous, para poder afinar cando hai varias Anas.
function coincideBusca(u: AdminUser, consulta: string): boolean {
  const termos = normalizarBusca(consulta).split(/\s+/).filter(Boolean);
  if (termos.length === 0) return true;
  const campos = normalizarBusca([u.username, u.name, u.surname, u.group].filter(Boolean).join(' '));
  return termos.every(termo => campos.includes(termo));
}

function UsersSection({ users, busy, onRefresh, onDelete, onChangePassword, onToggleProfessor, onChangeGroup, askConfirm }: {
  users: AdminUser[]; busy: string; onRefresh: () => void;
  onDelete: (name: string) => void; onChangePassword: (name: string, pass: string) => void;
  onToggleProfessor: (name: string, isProf: boolean) => void;
  onChangeGroup: (u: AdminUser, novo: string) => void;
  askConfirm: (text: string, onYes: () => void) => void;
}) {
  const { t } = useTranslation();
  // Busca local: a lista enteira xa está en memoria (AdminListarUsuarios), así
  // que filtrar aquí non custa unha ida ao servidor por cada tecla.
  const [busca, setBusca] = useState('');
  const visibles = useMemo(() => users.filter(u => coincideBusca(u, busca)), [users, busca]);
  return (
    <Card icon={<Users className="w-5 h-5" />} title={t('adminPanel.usuariosServidorTitulo')}
      action={<button onClick={onRefresh} disabled={!!busy} className="flex items-center gap-1.5 text-sm text-stone-600 hover:text-stone-900 disabled:opacity-60"><RefreshCw className="w-4 h-4" /> {t('adminPanel.actualizar')}</button>}>
      {users.length === 0 ? (
        <p className="text-sm text-stone-400 py-4 text-center">{t('adminPanel.premeActualizar')}</p>
      ) : (
        <>
        <div className="flex items-center gap-2 mb-3">
          <div className="relative flex-1">
            <Search className="w-4 h-4 text-stone-400 absolute left-2.5 top-1/2 -translate-y-1/2 pointer-events-none" />
            <input
              type="search"
              value={busca}
              onChange={e => setBusca(e.target.value)}
              placeholder={t('adminPanel.buscarUsuarioPlaceholder')}
              className="w-full pl-8 pr-8 py-1.5 text-sm border border-stone-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-stone-300"
            />
            {busca && (
              <button type="button" onClick={() => setBusca('')} title={t('adminPanel.limparBusca')}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-stone-400 hover:text-stone-700 text-sm">✕</button>
            )}
          </div>
          <span className="text-xs text-stone-500 whitespace-nowrap">
            {t('adminPanel.usuariosAtopados', { visibles: visibles.length, total: users.length })}
          </span>
        </div>
        {visibles.length === 0 ? (
          <p className="text-sm text-stone-400 py-4 text-center">{t('adminPanel.senResultadosBusca', { busca })}</p>
        ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-stone-400 uppercase border-b border-stone-100">
                <th className="py-2 pr-3 font-semibold">{t('adminPanel.columnaUsuario')}</th>
                <th className="py-2 pr-3 font-semibold">{t('adminPanel.columnaNome')}</th>
                <th className="py-2 pr-3 font-semibold">{t('adminPanel.columnaGrupo')}</th>
                <th className="py-2 pr-3 font-semibold text-right">{t('adminPanel.columnaAccions')}</th>
              </tr>
            </thead>
            <tbody>
              {visibles.map(u => <UserRow key={u.username} u={u} busy={busy} onDelete={onDelete} onChangePassword={onChangePassword} onToggleProfessor={onToggleProfessor} onChangeGroup={onChangeGroup} askConfirm={askConfirm} />)}
            </tbody>
          </table>
        </div>
        )}
        </>
      )}
    </Card>
  );
}

function UserRow({ u, busy, onDelete, onChangePassword, onToggleProfessor, onChangeGroup, askConfirm }: {
  u: AdminUser; busy: string; onDelete: (name: string) => void; onChangePassword: (name: string, pass: string) => void;
  onToggleProfessor: (name: string, isProf: boolean) => void;
  onChangeGroup: (u: AdminUser, novo: string) => void;
  askConfirm: (text: string, onYes: () => void) => void;
}) {
  const { t } = useTranslation();
  const [pwd, setPwd] = useState('');
  const [open, setOpen] = useState(false);
  const [grupo, setGrupo] = useState<string | null>(null); // non nulo mentres se edita o grupo
  const rowBusy = busy === 'del:' + u.username || busy === 'pwd:' + u.username || busy === 'prof:' + u.username || busy === 'grp:' + u.username;
  // O grupo só ten sentido para o alumnado: é o que decide que materias ve.
  const editaGrupo = !u.isAdmin && !u.isProfessor;
  return (
    <tr className="border-b border-stone-50">
      <td className="py-2 pr-3 font-medium text-stone-800">
        {u.username}
        {u.isAdmin
          ? <span className="ml-2 text-[10px] uppercase bg-stone-200 text-stone-600 rounded px-1.5 py-0.5">{t('adminPanel.badgeAdmin')}</span>
          : u.isProfessor
            ? <span className="ml-2 text-[10px] uppercase bg-indigo-100 text-indigo-700 rounded px-1.5 py-0.5">{t('adminPanel.badgeProfesor')}</span>
            : <span className="ml-2 text-[10px] uppercase bg-emerald-100 text-emerald-700 rounded px-1.5 py-0.5">{t('adminPanel.badgeAlumno')}</span>}
      </td>
      <td className="py-2 pr-3 text-stone-600">{[u.name, u.surname].filter(Boolean).join(' ') || '—'}</td>
      <td className="py-2 pr-3 text-stone-600">
        {grupo !== null ? (
          <form onSubmit={e => { e.preventDefault(); if (grupo.trim()) { onChangeGroup(u, grupo); setGrupo(null); } }} className="flex items-center gap-1">
            <input type="text" value={grupo} onChange={e => setGrupo(e.target.value)} autoFocus placeholder={t('adminPanel.novoGrupoPlaceholder')} className="px-2 py-1 text-xs border border-stone-300 rounded w-28" />
            <button type="submit" disabled={rowBusy} className="px-2 py-1 text-xs bg-stone-900 text-white rounded disabled:opacity-60">OK</button>
            <button type="button" onClick={() => setGrupo(null)} className="px-2 py-1 text-xs text-stone-500">✕</button>
          </form>
        ) : (
          <span className="inline-flex items-center gap-1.5">
            {u.group || '—'}
            {editaGrupo && (
              busy === 'grp:' + u.username ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : (
                <button onClick={() => setGrupo(u.group || '')} disabled={rowBusy} title={t('adminPanel.cambiarGrupoTitulo')}
                  className="p-1 text-stone-400 hover:text-stone-800 hover:bg-stone-100 rounded disabled:opacity-40">
                  <Pencil className="w-3 h-3" />
                </button>
              )
            )}
          </span>
        )}
      </td>
      <td className="py-2 pr-3">
        <div className="flex items-center justify-end gap-2">
          {open ? (
            <form onSubmit={e => { e.preventDefault(); if (pwd.trim()) { onChangePassword(u.username, pwd.trim()); setPwd(''); setOpen(false); } }} className="flex items-center gap-1">
              <input type="text" value={pwd} onChange={e => setPwd(e.target.value)} autoFocus minLength={12} placeholder={t('adminPanel.novoContrasinalPlaceholder')} className="px-2 py-1 text-xs border border-stone-300 rounded w-48" />
              <button type="submit" disabled={rowBusy} className="px-2 py-1 text-xs bg-stone-900 text-white rounded disabled:opacity-60">{busy === 'pwd:' + u.username ? '…' : 'OK'}</button>
              <button type="button" onClick={() => { setOpen(false); setPwd(''); }} className="px-2 py-1 text-xs text-stone-500">✕</button>
            </form>
          ) : (
            <button onClick={() => setOpen(true)} disabled={rowBusy || u.isAdmin} title={t('adminPanel.cambiarContrasinalTitulo')}
              className="flex items-center gap-1 px-2 py-1 text-xs text-stone-600 hover:bg-stone-100 rounded disabled:opacity-40">
              <Lock className="w-3.5 h-3.5" /> {t('adminPanel.contrasinalBoton')}
            </button>
          )}
          {!u.isAdmin && (
            <label className="flex items-center gap-1 text-xs text-stone-500">
              <GraduationCap className="w-3.5 h-3.5" />
              {busy === 'prof:' + u.username ? (
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
              ) : (
                <select
                  value={u.isProfessor ? 'profesor' : 'alumno'}
                  onChange={e => onToggleProfessor(u.username, e.target.value === 'profesor')}
                  disabled={rowBusy}
                  title={t('adminPanel.rolUsuarioTitulo')}
                  className="px-1.5 py-1 text-xs border border-stone-300 rounded bg-white disabled:opacity-40">
                  <option value="alumno">{t('adminPanel.opcionAlumno')}</option>
                  <option value="profesor">{t('adminPanel.opcionProfesor')}</option>
                </select>
              )}
            </label>
          )}
          <button onClick={() => askConfirm(t('adminPanel.confirmarEliminarUsuario', { username: u.username }), () => onDelete(u.username))}
            disabled={rowBusy || u.isAdmin} title={t('adminPanel.eliminarUsuarioTitulo')}
            className="flex items-center gap-1 px-2 py-1 text-xs text-rose-600 hover:bg-rose-50 rounded disabled:opacity-40">
            {busy === 'del:' + u.username ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Trash2 className="w-3.5 h-3.5" />} {t('adminPanel.eliminarBoton')}
          </button>
        </div>
      </td>
    </tr>
  );
}

// ---- Diálogo de confirmación (substitúe o confirm() nativo, non fiable no webview) --

function ConfirmModal({ text, confirmLabel, onConfirm, onCancel }: { text: string; confirmLabel?: string; onConfirm: () => void; onCancel: () => void }) {
  const { t } = useTranslation();
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/50" onClick={onCancel}>
      <div className="bg-white rounded-2xl shadow-xl border border-stone-200 w-full max-w-md p-6" onClick={e => e.stopPropagation()}>
        <div className="flex items-start gap-3">
          <div className="p-2 bg-rose-100 text-rose-600 rounded-lg shrink-0"><AlertTriangle className="w-5 h-5" /></div>
          <div className="flex-1">
            <h3 className="text-base font-bold text-stone-900 mb-1">{t('adminPanel.confirmarAccionTitulo')}</h3>
            <p className="text-sm text-stone-600 whitespace-pre-wrap">{text}</p>
          </div>
        </div>
        <div className="mt-6 flex justify-end gap-2">
          <button onClick={onCancel} className="px-4 py-2 text-sm font-medium text-stone-600 hover:bg-stone-100 rounded-lg">{t('adminPanel.cancelar')}</button>
          {confirmLabel ? (
            <button onClick={onConfirm} autoFocus className="flex items-center gap-2 px-4 py-2 text-sm font-medium bg-stone-900 text-white rounded-lg hover:bg-stone-700">
              <CheckCircle2 className="w-4 h-4" /> {confirmLabel}
            </button>
          ) : (
            <button onClick={onConfirm} autoFocus className="flex items-center gap-2 px-4 py-2 text-sm font-medium bg-rose-600 text-white rounded-lg hover:bg-rose-700">
              <Trash2 className="w-4 h-4" /> {t('adminPanel.eliminarBoton')}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

// ---- Primitivos de UI ----------------------------------------------------------

const inputCls = 'w-full px-3 py-2 bg-white border border-stone-300 rounded-lg shadow-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none text-sm';

function Field({ label, children, className = '' }: { label: string; children: React.ReactNode; className?: string }) {
  return (
    <div className={`flex flex-col gap-1.5 ${className}`}>
      <label className="text-xs font-bold text-stone-600">{label}</label>
      {children}
    </div>
  );
}

function Card({ icon, title, action, children }: { icon: React.ReactNode; title: string; action?: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="bg-white rounded-2xl shadow-sm border border-stone-200 p-5 sm:p-6">
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2.5">
          <div className="p-2 bg-stone-100 text-stone-700 rounded-lg">{icon}</div>
          <h2 className="text-base font-bold text-stone-900">{title}</h2>
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}

function errText(e: any): string {
  return typeof e === 'string' ? e : (e?.message || String(e));
}
