import React, { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { X, Send, Loader2, MessageSquare, Megaphone, Users, User, Paperclip, Download, FileText, Ban, ExternalLink } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { StudentProfile } from './types';
import { gruposDe, pertenceAGrupo } from './grupos';
import { MessagingConfig, RosterRow, Message } from '../bindings/tao/models';
import {
  GetMyMessages, SendMessageToProfessor,
  GetStudentMessages, SendMessageToStudent, SendMessageToStudents,
  GetWallMessages, PostToWall, GetPeerMessages, SendPeerMessage,
  ListMyPeers, PickFiles, DownloadChatAttachment, OpenFile,
} from '../bindings/tao/app';

// MessagesModal is the in-app chat panel (the Wails webview cannot use native
// alert/confirm dialogs). It gathers every messaging channel behind role-specific tabs:
//   - Student: «Profesorado» (per materia), «Muro do grupo» (shared wall) and
//     «Compañeiros» (1:1 cun compañeiro do mesmo grupo OU dunha das súas materias:
//     hai materias que xuntan varios grupos — ver ListMyPeers en mensaxes.go).
//   - Professor: «Alumnado» (recipient picker with group filter — one/several/all, per
//     materia) and «Muro do grupo».
// Any message may carry file attachments the other side downloads.

interface StudentOpt {
  username: string;
  label: string;
  group?: string;
}

interface Props {
  profile: StudentProfile;
  isProfessor: boolean;
  materiaId: string;
  materiaName: string;
  students?: StudentOpt[];       // professor: the roster (with group)
  initialSelected?: string;
  config: MessagingConfig;  // admin messaging policy (wall/peer, disabled lists)
  onClose: () => void;
  onSeen?: (counterpart: string, ts: number) => void;
}

type StudentTab = 'prof' | 'wall' | 'peer';
type ProfTab = 'alumnado' | 'wall';
type Picked = { path: string; name: string };

const fmtTime = (ts: number) => {
  try {
    return new Date(ts).toLocaleString('gl-ES', {
      day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit',
    });
  } catch { return ''; }
};
const baseName = (p: string) => p.split(/[\\/]/).pop() || p;

export default function MessagesModal({
  profile, isProfessor, materiaId, materiaName,
  students = [], initialSelected = '', config, onClose, onSeen,
}: Props) {
  const { t } = useTranslation();
  const myUsername = profile.username;
  const myGroup = (profile.group || '').trim();

  // Is the logged-in student blocked from messaging entirely (admin control)?
  const blocked = !isProfessor && (
    (config.disabledUsers || []).some(u => u.trim().toLowerCase() === myUsername.toLowerCase()) ||
    (!!myGroup && (config.disabledGroups || []).some(g => g.trim().toLowerCase() === myGroup.toLowerCase()))
  );
  const wallOn = !!config.wallEnabled;
  const peerOn = !!config.peerEnabled;

  // ---- Tabs -----------------------------------------------------------------------
  const studentTabs: StudentTab[] = ['prof', ...(wallOn && myGroup ? ['wall' as StudentTab] : []), ...(peerOn && myGroup ? ['peer' as StudentTab] : [])];
  const [sTab, setSTab] = useState<StudentTab>('prof');
  const [pTab, setPTab] = useState<ProfTab>(initialSelected ? 'alumnado' : 'alumnado');

  // ---- Professor recipient picker (Alumnado tab) ----------------------------------
  const [groupFilter, setGroupFilter] = useState<string>('');
  const groups = useMemo(
    () => Array.from(new Set(students.flatMap(s => gruposDe(s.group)))).sort(),
    [students]
  );
  const shownStudents = useMemo(
    () => groupFilter ? students.filter(s => pertenceAGrupo(s.group, groupFilter)) : students,
    [students, groupFilter]
  );
  const [selected, setSelected] = useState<Set<string>>(() => new Set(initialSelected ? [initialSelected] : []));
  const selectedArr = useMemo(() => [...selected], [selected]);
  const single = selectedArr.length === 1 ? selectedArr[0] : '';

  // ---- Professor wall group / student peer target ---------------------------------
  const [wallGroup, setWallGroup] = useState<string>(initialSelected ? (gruposDe(students.find(s => s.username === initialSelected)?.group)[0] || '') : (groups[0] || ''));
  const [mates, setMates] = useState<RosterRow[]>([]);
  const [peer, setPeer] = useState<string>('');

  // ---- Thread + composer state ----------------------------------------------------
  const [thread, setThread] = useState<Message[]>([]);
  const [loading, setLoading] = useState(false);
  const [text, setText] = useState('');
  const [attach, setAttach] = useState<Picked[]>([]);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  // Path of the file just saved to disk, so a button can open it.
  const [downloaded, setDownloaded] = useState('');
  const endRef = useRef<HTMLDivElement>(null);

  // Load the classmates once, when the student opens the «Compañeiros» tab.
  useEffect(() => {
    if (isProfessor || sTab !== 'peer' || mates.length) return;
    ListMyPeers().then(rows => {
      setMates(rows || []);
      if (!peer && rows && rows.length) setPeer(rows[0].username);
    }).catch(() => {});
  }, [isProfessor, sTab]); // eslint-disable-line react-hooks/exhaustive-deps

  // Which channel is active → how to read/send. Returns null when nothing to show yet
  // (e.g. professor composing to several: no single thread).
  const channel = useMemo(() => {
    if (isProfessor) {
      if (pTab === 'wall') return wallGroup ? { kind: 'wall', group: wallGroup, counterpart: `wall:${wallGroup}` } : null;
      if (single) return { kind: 'profStudent', student: single, counterpart: single };
      return null; // 0 or several selected: compose-only
    }
    if (sTab === 'wall') return myGroup ? { kind: 'wall', group: myGroup, counterpart: `wall:${myGroup}` } : null;
    // O fío 1:1 xa non depende do grupo: unha soa conversa por parella.
    if (sTab === 'peer') return peer ? { kind: 'peer', other: peer, counterpart: `peer:${peer}` } : null;
    return { kind: 'prof', counterpart: 'profesor' };
  }, [isProfessor, pTab, sTab, wallGroup, single, myGroup, peer]) as any;

  const loadThread = useCallback(async () => {
    if (!channel) { setThread([]); return; }
    setLoading(true);
    setError('');
    try {
      let msgs: Message[] = [];
      if (channel.kind === 'prof') msgs = await GetMyMessages(materiaId);
      else if (channel.kind === 'profStudent') msgs = await GetStudentMessages(channel.student, materiaId);
      else if (channel.kind === 'wall') msgs = await GetWallMessages(channel.group);
      else if (channel.kind === 'peer') msgs = await GetPeerMessages(channel.other);
      const list = msgs || [];
      setThread(list);
      if (list.length && onSeen) onSeen(channel.counterpart, list[list.length - 1].ts);
    } catch (e: any) {
      setError(t('messages.erroCargarMensaxes', { error: e?.message || e }));
    } finally {
      setLoading(false);
    }
  }, [channel, materiaId, onSeen]);

  useEffect(() => { loadThread(); }, [loadThread]);
  useEffect(() => { endRef.current?.scrollIntoView({ behavior: 'smooth' }); }, [thread]);

  const pickFiles = async () => {
    setError('');
    try {
      const paths = await PickFiles(t('messages.escolleFicheirosAdxuntar'));
      if (paths && paths.length) setAttach(prev => [...prev, ...paths.map(p => ({ path: p, name: baseName(p) }))]);
    } catch (e: any) { setError(e?.message || String(e)); }
  };
  const dropAttach = (path: string) => setAttach(prev => prev.filter(a => a.path !== path));

  const composeDisabled = (() => {
    if (blocked) return true;
    if (isProfessor) return pTab === 'wall' ? !wallGroup : selectedArr.length === 0;
    if (sTab === 'peer') return !peer;
    return false;
  })();
  const isNotice = isProfessor && pTab === 'alumnado' && selectedArr.length > 1;

  const send = async () => {
    const body = text.trim();
    const paths = attach.map(a => a.path);
    if ((!body && paths.length === 0) || sending || composeDisabled) return;
    setSending(true); setError(''); setNotice('');
    try {
      if (isProfessor && pTab === 'wall') {
        await PostToWall(wallGroup, body, paths);
        clearComposer(); await loadThread();
      } else if (isProfessor && single) {
        await SendMessageToStudent(single, materiaId, body, paths);
        clearComposer(); await loadThread();
      } else if (isProfessor) {
        const n = await SendMessageToStudents(selectedArr, materiaId, body, paths);
        setNotice(t('messages.mensaxeEnviadaAlumnado', { count: n }));
        clearComposer();
      } else if (sTab === 'wall') {
        await PostToWall(myGroup, body, paths);
        clearComposer(); await loadThread();
      } else if (sTab === 'peer') {
        await SendPeerMessage(peer, body, paths);
        clearComposer(); await loadThread();
      } else {
        await SendMessageToProfessor(materiaId, body, paths);
        clearComposer(); await loadThread();
      }
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setSending(false);
    }
  };
  const clearComposer = () => { setText(''); setAttach([]); setDownloaded(''); };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); }
  };

  const downloadAttachment = async (m: Message, name: string) => {
    setError(''); setNotice(''); setDownloaded('');
    try {
      const dest = await DownloadChatAttachment(m.attachBase || '', m.id, name);
      if (dest) { setNotice(t('messages.gardadoEn', { dest })); setDownloaded(dest); }
    } catch (e: any) { setError(t('messages.erroDescargar', { error: e?.message || e })); }
  };
  const openDownloaded = async () => {
    if (!downloaded) return;
    try { await OpenFile(downloaded); } catch (e: any) { setError(t('messages.erroAbrirFicheiro', { error: e?.message || e })); }
  };

  // ---- Render helpers -------------------------------------------------------------
  const TabButton = ({ active, onClick, icon, label }: { active: boolean; onClick: () => void; icon: React.ReactNode; label: string }) => (
    <button onClick={onClick} className={`flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors ${active ? 'bg-stone-900 text-white' : 'text-stone-600 hover:bg-stone-100'}`}>
      {icon}{label}
    </button>
  );

  const renderThread = () => (
    <div className="flex-1 overflow-y-auto p-4 bg-stone-50 flex flex-col gap-3">
      {loading ? (
        <div className="m-auto text-stone-400 flex items-center gap-2"><Loader2 className="w-5 h-5 animate-spin" /> {t('messages.cargando')}</div>
      ) : thread.length === 0 ? (
        <div className="m-auto text-center text-stone-400 text-sm">{t('messages.semMensaxes')}</div>
      ) : (
        thread.map(m => {
          const mine = m.author === myUsername;
          return (
            <div key={m.id} className={`flex flex-col max-w-[80%] ${mine ? 'self-end items-end' : 'self-start items-start'}`}>
              <div className={`px-3.5 py-2 rounded-2xl text-sm whitespace-pre-wrap break-words ${mine ? 'bg-stone-900 text-white rounded-br-sm' : 'bg-white border border-stone-200 text-stone-800 rounded-bl-sm'}`}>
                {!mine && (m.from === 'profesor' || m.broadcast) && (
                  <span className={`flex items-center gap-1 text-[11px] font-bold uppercase tracking-wide mb-1 ${m.from === 'profesor' ? 'text-emerald-600' : 'text-indigo-500'}`}>
                    {m.from === 'profesor' ? <><User className="w-3 h-3" /> {t('messages.profesorLabel')}</> : <><Megaphone className="w-3 h-3" /> {t('messages.avisoLabel')}</>}
                  </span>
                )}
                {m.text}
                {(m.attachments || []).length > 0 && (
                  <div className={`mt-2 flex flex-col gap-1 ${mine ? 'items-end' : 'items-start'}`}>
                    {(m.attachments || []).map(f => (
                      <button key={f.name} onClick={() => downloadAttachment(m, f.name)}
                        className={`flex items-center gap-1.5 text-xs px-2 py-1 rounded-lg border transition-colors ${mine ? 'border-white/30 hover:bg-white/10 text-white' : 'border-stone-300 hover:bg-stone-100 text-stone-700'}`}
                        title={t('messages.descargar')}>
                        <FileText className="w-3.5 h-3.5" /><span className="truncate max-w-[180px]">{f.name}</span><Download className="w-3.5 h-3.5 shrink-0" />
                      </button>
                    ))}
                  </div>
                )}
              </div>
              <span className="text-[11px] text-stone-400 mt-1 px-1">{!mine && m.authorName ? `${m.authorName} · ` : ''}{fmtTime(m.ts)}</span>
            </div>
          );
        })
      )}
      <div ref={endRef} />
    </div>
  );

  const renderComposer = () => (
    <div className="border-t border-stone-200 p-3 bg-white flex flex-col gap-2">
      {error && <p className="text-sm text-rose-600 px-1">{error}</p>}
      {notice && (
        <div className="flex items-center gap-2 px-1 text-sm text-emerald-600">
          <span className="break-all min-w-0">{notice}</span>
          {downloaded && (
            <button onClick={openDownloaded}
              className="flex items-center gap-1 shrink-0 px-2 py-1 rounded-lg bg-emerald-600 text-white text-xs font-medium hover:bg-emerald-700 transition-colors">
              <ExternalLink className="w-3.5 h-3.5" /> {t('messages.abrirFicheiro')}
            </button>
          )}
        </div>
      )}
      {attach.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {attach.map(a => (
            <span key={a.path} className="flex items-center gap-1 text-xs bg-stone-100 border border-stone-200 rounded-lg pl-2 pr-1 py-1">
              <FileText className="w-3.5 h-3.5 text-stone-400" /><span className="truncate max-w-[160px]">{a.name}</span>
              <button onClick={() => dropAttach(a.path)} className="p-0.5 text-stone-400 hover:text-rose-600"><X className="w-3.5 h-3.5" /></button>
            </span>
          ))}
        </div>
      )}
      <div className="flex items-end gap-2">
        <button onClick={pickFiles} disabled={composeDisabled || sending} title={t('messages.adxuntarFicheirosTitle')}
          className="p-2.5 rounded-xl border border-stone-300 text-stone-500 hover:text-stone-800 hover:bg-stone-100 transition-colors shrink-0 disabled:opacity-40">
          <Paperclip className="w-4 h-4" />
        </button>
        <textarea
          value={text} onChange={e => setText(e.target.value)} onKeyDown={onKeyDown}
          disabled={composeDisabled || sending} rows={1}
          placeholder={composeDisabled ? t('messages.placeholderNonDisponible') : (isNotice ? t('messages.placeholderAviso') : t('messages.placeholderMensaxe'))}
          className="flex-1 resize-none p-2.5 bg-stone-50 border border-stone-300 rounded-xl shadow-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none text-sm max-h-32 disabled:opacity-60"
        />
        <button onClick={send} disabled={composeDisabled || sending || (!text.trim() && attach.length === 0)}
          className={`flex items-center gap-2 px-4 py-2.5 rounded-xl font-medium text-sm text-white transition-colors shrink-0 disabled:opacity-40 ${isNotice ? 'bg-indigo-600 hover:bg-indigo-700' : 'bg-emerald-600 hover:bg-emerald-700'}`}>
          {sending ? <Loader2 className="w-4 h-4 animate-spin" /> : isNotice ? <Megaphone className="w-4 h-4" /> : <Send className="w-4 h-4" />}
          {isNotice ? t('messages.enviarA', { count: selectedArr.length }) : t('messages.enviar')}
        </button>
      </div>
    </div>
  );

  // ---- Layout ---------------------------------------------------------------------
  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
      <div className={`bg-white rounded-2xl shadow-xl w-full overflow-hidden flex flex-col max-h-[90vh] h-[80vh] ${isProfessor ? 'max-w-4xl' : 'max-w-2xl'}`}>
        {/* Header */}
        <div className="p-4 border-b border-stone-200 flex justify-between items-center bg-stone-50">
          <div className="flex items-center gap-3 min-w-0">
            <div className="p-2 bg-stone-900 text-emerald-400 rounded-lg shrink-0"><MessageSquare className="w-5 h-5" /></div>
            <div className="min-w-0">
              <h3 className="font-bold text-lg text-stone-900 truncate">{t('messages.titulo')}</h3>
              <p className="text-sm text-stone-500 truncate">{materiaName}</p>
            </div>
          </div>
          <button onClick={onClose} className="p-2 text-stone-400 hover:text-stone-600 rounded-lg hover:bg-stone-200 transition-colors shrink-0"><X className="w-5 h-5" /></button>
        </div>

        {/* Tab bar */}
        <div className="flex gap-1 p-2 border-b border-stone-200 bg-white flex-wrap">
          {isProfessor ? (
            <>
              <TabButton active={pTab === 'alumnado'} onClick={() => setPTab('alumnado')} icon={<Users className="w-4 h-4" />} label={t('messages.alumnado')} />
              <TabButton active={pTab === 'wall'} onClick={() => setPTab('wall')} icon={<Megaphone className="w-4 h-4" />} label={t('messages.muroDoGrupo')} />
            </>
          ) : (
            studentTabs.map(tab => (
              <TabButton key={tab} active={sTab === tab} onClick={() => setSTab(tab)}
                icon={tab === 'prof' ? <User className="w-4 h-4" /> : tab === 'wall' ? <Megaphone className="w-4 h-4" /> : <Users className="w-4 h-4" />}
                label={tab === 'prof' ? t('messages.profesorado') : tab === 'wall' ? t('messages.muroDoGrupo') : t('messages.companeiros')} />
            ))
          )}
        </div>

        {blocked ? (
          <div className="flex-1 flex flex-col items-center justify-center gap-3 text-stone-500 p-6 text-center">
            <Ban className="w-10 h-10 text-rose-400" />
            <p className="text-sm max-w-sm">{t('messages.mensaxeriaDesactivada')}</p>
          </div>
        ) : (
          <div className="flex-1 flex min-h-0">
            {/* Professor «Alumnado»: recipient picker with group filter */}
            {isProfessor && pTab === 'alumnado' && (
              <div className="w-64 shrink-0 border-r border-stone-200 flex flex-col bg-white">
                <div className="p-2 border-b border-stone-200 flex flex-col gap-2">
                  {groups.length > 0 && (
                    <select value={groupFilter} onChange={e => setGroupFilter(e.target.value)}
                      className="w-full text-sm bg-stone-50 border border-stone-200 rounded-lg p-1.5">
                      <option value="">{t('messages.todosOsGrupos')}</option>
                      {groups.map(g => <option key={g} value={g}>{g}</option>)}
                    </select>
                  )}
                  <label className="flex items-center gap-2 px-2 py-1 rounded-lg hover:bg-stone-100 cursor-pointer text-sm font-medium text-stone-700">
                    <input type="checkbox"
                      checked={shownStudents.length > 0 && shownStudents.every(s => selected.has(s.username))}
                      onChange={() => setSelected(prev => {
                        const every = shownStudents.length > 0 && shownStudents.every(s => prev.has(s.username));
                        const next = new Set(prev);
                        shownStudents.forEach(s => every ? next.delete(s.username) : next.add(s.username));
                        return next;
                      })}
                      className="accent-stone-900" />
                    <Users className="w-4 h-4 text-stone-400" /> {t('messages.todosCount', { count: shownStudents.length })}
                  </label>
                </div>
                <div className="flex-1 overflow-y-auto p-2 flex flex-col gap-0.5">
                  {shownStudents.length === 0 ? (
                    <p className="text-sm text-stone-400 p-2">{t('messages.nonHaiAlumnado')}</p>
                  ) : shownStudents.map(s => (
                    <label key={s.username} className={`flex items-center gap-2 px-2 py-1.5 rounded-lg cursor-pointer text-sm transition-colors ${selected.has(s.username) ? 'bg-stone-100 text-stone-900 font-medium' : 'text-stone-600 hover:bg-stone-50'}`}>
                      <input type="checkbox" checked={selected.has(s.username)}
                        onChange={() => setSelected(prev => { const n = new Set(prev); n.has(s.username) ? n.delete(s.username) : n.add(s.username); return n; })}
                        className="accent-stone-900" />
                      <span className="truncate">{s.label}</span>
                    </label>
                  ))}
                </div>
              </div>
            )}

            {/* Conversation / compose column */}
            <div className="flex-1 flex flex-col min-w-0">
              {/* Professor wall: group selector */}
              {isProfessor && pTab === 'wall' && (
                <div className="p-2 border-b border-stone-200 bg-white flex items-center gap-2">
                  <Megaphone className="w-4 h-4 text-stone-400" />
                  <select value={wallGroup} onChange={e => setWallGroup(e.target.value)}
                    className="text-sm bg-stone-50 border border-stone-200 rounded-lg p-1.5">
                    {groups.length === 0 && <option value="">{t('messages.senGrupos')}</option>}
                    {groups.map(g => <option key={g} value={g}>{t('messages.muroDe', { group: g })}</option>)}
                  </select>
                </div>
              )}
              {/* Student peer: classmate selector */}
              {!isProfessor && sTab === 'peer' && (
                <div className="p-2 border-b border-stone-200 bg-white flex items-center gap-2">
                  <Users className="w-4 h-4 text-stone-400" />
                  {mates.length === 0 ? (
                    <span className="text-sm text-stone-400">{t('messages.nonHaiCompaneiros')}</span>
                  ) : (
                    <select value={peer} onChange={e => setPeer(e.target.value)}
                      className="text-sm bg-stone-50 border border-stone-200 rounded-lg p-1.5">
                      {mates.map(m => {
                        const nome = [m.name, m.surname].filter(Boolean).join(' ') || m.username;
                        // O grupo vai na etiqueta porque a lista pode mesturar grupos
                        // (compañeiros de materia); sen el non se distinguirían.
                        return <option key={m.username} value={m.username}>{m.group ? `${nome} — ${m.group}` : nome}</option>;
                      })}
                    </select>
                  )}
                </div>
              )}

              {/* Body: thread, or professor compose-only notice */}
              {isProfessor && pTab === 'alumnado' && !single ? (
                <div className="flex-1 overflow-y-auto p-4 bg-stone-50 flex items-center">
                  <div className="m-auto text-center text-stone-500 max-w-sm flex flex-col items-center gap-3">
                    {selectedArr.length === 0
                      ? <><Users className="w-10 h-10 text-stone-300" /><p className="text-sm">{t('messages.escolleAlumnos')}</p></>
                      : <><Megaphone className="w-10 h-10 text-indigo-400" /><p className="text-sm">{t('messages.mensaxeEnviaraseA', { count: selectedArr.length })}</p></>}
                  </div>
                </div>
              ) : renderThread()}

              {renderComposer()}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
