import React, { useState, useMemo, useEffect, useRef, forwardRef, useImperativeHandle } from 'react';
import { Calendar as CalendarIcon, BookOpen, Clock, AlertCircle, Save, X, Settings, FileText, Printer, Download, Lock, Unlock, Plus, Trash2, ChevronLeft, ChevronRight, Upload, Database, FileUp, Loader2, FileJson, Sparkles, LogOut, FileSignature, ShieldCheck, Paperclip, MessageSquare, Move, Eye, FolderOpen, Monitor, RefreshCw, ZoomIn, ZoomOut, Info, Users, Pencil, ClipboardList, Package } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { gruposDe } from './grupos';
import { generateMonthlySchedule, calendarEntriesFromLegacy } from './data';
import { DailyLog, AppConfig, Unit, Subject, UnitReport, DayStatus, StudentProfile, CalendarEntry, ProensInfo, Incident, IncidentType } from './types';
import { INCIDENT_TYPES, INCIDENT_SYMBOLS, INCIDENT_LABEL_KEYS, INCIDENT_COLORS } from './incidents';
import { defaultUnits, defaultHolidays, defaultConfig } from './data';
import { GoogleGenAI, Type } from "@google/genai";
import * as pdfjs from 'pdfjs-dist';
import {
  ChangePassword, ImportProens, ImportProensInfo, Logout, PullBackup, PushBackup, RegenerateCertificate, OpenFile, Hostname,
  GetMyMaterias, ListMaterias, ListMyMaterias, SaveMateria, DeleteMateria, ListRoster, GetStudentBackup, SaveStudentBackup,
  AddIncident, RemoveIncident, GetPrivateNote, SavePrivateNote,
  ListWorkFiles, UploadWorkFiles, DownloadWorkFile, DeleteWorkFile, ListWorkFileDates,
  ListStudentWorkFiles, DownloadStudentWorkFile, ListStudentWorkFileDates, ImportSchoolCalendar,
  GetMyMessages, GetStudentMessages, GetMessagingConfig,
  UploadTaskFiles, ListTaskFiles, DownloadTaskFile, DeleteTaskFile,
  SaveStudentLogComment, UploadCommentFile, DeleteCommentFile,
  ListStudentCommentFiles, DownloadStudentCommentFile, ListCommentFiles, DownloadCommentFile,
  PrepareCorrectionFolder, AbrirCartafolCorreccion,
  ExportSignedReport, ListWorkFilesForReport,
  SaveTaskDescription, GetTaskDescription, ListTaskOverrides, DeleteTaskOverride, DeleteAllTaskOverrides,
  GetTaskDescriptionAlumno, SaveTaskDescriptionAlumno,
  AbrirYangParaTarefa, AbrirYangParaTarefaAlumno, YangDispoñible, InstalarYang,
  GetActualizacionTao,
  AbrirMaltaUnidade, AbrirMaltaMensual, MaltaDispoñible, ListMaltaReports, GetMaltaReport,
  ExportMaltaReportPDF, ExportMaltaReportODT, ExportMaltaReportDOCX,
} from '../bindings/tao/app';
import { Events } from '@wailsio/runtime';
import {
  EstadoActualizacionTao, RosterRow, MessagingConfig, MateriaDef, WorkFile, PracticaCorrectionSummary,
} from '../bindings/tao/models';
import Login from './Login';
import MessagesModal from './Messages';
import BackupRestore from './Backup';
import MarkdownPreview from './MarkdownPreview';
import { applyReprogram, NO_CLASS, ReprogramAction, NoClassStatus } from './reprogram';
import AdminPanel from './AdminPanel';
import SobreTao from './Sobre';
import MarkdownEditor from './MarkdownEditor';
import ReportBuilder from './ReportBuilder';
import { buildReportHTML, fmtShort } from './reportHtml';
import CalendarTable from './CalendarTable';
import Logo from './Logo';
import PasswordInput from './PasswordInput';
import ProgramacionPanel from './ProgramacionPanel';
import ModulosTao from './Modulos';

// newEntryId xera o identificador dunha fila do calendario. Só serve como clave de
// React e para editar/borrar a fila, non se persiste con ningún significado.
const newEntryId = () => `c${Date.now().toString(36)}${Math.random().toString(36).slice(2, 7)}`;

// MATERIAS_POLL_MS: intervalo do refresco automático das materias (alumnado), ver
// handleRefreshMaterias e o useEffect que o chama en bucle - así, se o profesor
// desbloquea unha sesión (ou cambia calquera outra cousa da programación), o alumnado
// véxao sen premer "Actualizar" nin ter que saír e volver entrar.
const MATERIAS_POLL_MS = 8000;

// Sentinelas do <select> de alumnado do profesor (ver o comentario xunto ao <select>,
// máis abaixo en App): username real nunca pode chocar con elas.
const MODELO_PLACEHOLDER_VALUE = ' alumnado-placeholder';
const MODELO_OPTION_VALUE = ' programacion-modelo';

// Zoom do calendario (Ctrl+/Ctrl-, ver useEffect en App): moitos profesores non ven ben
// de preto a grade completa (31 columnas). 1 = tamaño normal.
const CALENDAR_ZOOM_MIN = 0.6;
const CALENDAR_ZOOM_MAX = 2;
const CALENDAR_ZOOM_STEP = 0.1;

// weekdayName devolve o nome do día da semana (índice = Date.getDay(), 0 = domingo)
// para o tooltip de cada cela do calendario, segundo o idioma da UI (locale).
function weekdayName(dayOfWeek: number, locale: string): string {
  // 2024-01-07 é un domingo calquera, só serve de ancoraxe para Intl.
  const date = new Date(2024, 0, 7 + dayOfWeek);
  const name = new Intl.DateTimeFormat(locale, { weekday: 'long' }).format(date);
  return name.charAt(0).toUpperCase() + name.slice(1);
}

// Configurar o worker de PDF.js
pdfjs.GlobalWorkerOptions.workerSrc = `https://cdnjs.cloudflare.com/ajax/libs/pdf.js/${pdfjs.version}/pdf.worker.min.js`;

const createDefaultSubject = (): Subject => ({
  id: Date.now().toString(),
  name: 'Tecnoloxía e Dixitalización',
  academicYear: '2025/2026',
  isLocked: false,
  config: { ...defaultConfig, customHolidays: [] },
  units: defaultUnits,
  logs: {},
  unitReports: {},
  monthlyReports: {}
});

export default function App() {
  const { t, i18n } = useTranslation();
  const [subjects, setSubjects] = useState<Subject[]>(() => {
    const saved = localStorage.getItem('school-schedule-subjects');
    if (saved) {
      try {
        const parsed = JSON.parse(saved);
        // Unha sesión anterior sen materias deixa a lista baleira; sen esta comprobación
        // a app quedaría sen materia ningunha que amosar.
        if (Array.isArray(parsed) && parsed.length > 0) {
          return parsed.map((s: any) => ({
            ...s,
            config: {
              ...defaultConfig,
              ...s.config,
              scheduleDays: s.config?.scheduleDays || [1, 3, 5],
              customHolidays: s.config?.customHolidays || []
            }
          }));
        }
      } catch (e) {
        console.error("Failed to parse subjects", e);
      }
    }
    return [createDefaultSubject()];
  });
  
  const [currentSubjectId, setCurrentSubjectId] = useState<string>(subjects[0]?.id || '');
  
  const currentSubject = useMemo(() => {
    return subjects.find(s => s.id === currentSubjectId) || subjects[0];
  }, [subjects, currentSubjectId]);

  const [profile, setProfile] = useState<StudentProfile | null>(null);
  // Nome deste equipo (hostname), para gardalo coa práctica do día e poder localizar o
  // responsable en caso de desperfectos no computador.
  const [hostname, setHostname] = useState('');
  useEffect(() => { Hostname().then(setHostname).catch(() => {}); }, []);
  const [selectedDay, setSelectedDay] = useState<any | null>(null);
  const [showSettings, setShowSettings] = useState(false);
  const [showSobre, setShowSobre] = useState(false);
  const [showProgramacionPanel, setShowProgramacionPanel] = useState(false);
  // UD enfocada no ProgramacionPanel (chip "UD N" do calendario, App.tsx máis
  // abaixo). null cando o panel está na súa vista xeral (botón "Programación").
  const [focusedProensUnit, setFocusedProensUnit] = useState<Unit | null>(null);
  const [actualizacionTaoDispo, setActualizacionTaoDispo] = useState(false);
  useEffect(() => {
    GetActualizacionTao().then((e) => setActualizacionTaoDispo(!!e.disponible)).catch(() => {});
    const off = Events.On('actualizacion_tao_cambiada', (e) => setActualizacionTaoDispo(!!(e.data as EstadoActualizacionTao).disponible));
    return () => { off(); };
  }, []);
  const [showReports, setShowReports] = useState(false);
  const [showSignedReport, setShowSignedReport] = useState(false);
  // datePicker: cando non é null, o próximo clic nun día do calendario xeral non abre
  // o DayModal - captura esa data e chámalle a onPick (ver ReportBuilder, que a activa
  // premendo a icona de calendario xunto a cada campo de data, en vez de escribila).
  const [datePicker, setDatePicker] = useState<{ onPick: (dateString: string) => void } | null>(null);
  const [syncMsg, setSyncMsg] = useState('');
  // calendarZoom: tamaño do calendario (Ctrl+/Ctrl-, ou os botóns xunto á grade) - moitos
  // profesores agradecen poder achegalo sen ter que achegarse eles á pantalla.
  const [calendarZoom, setCalendarZoom] = useState(1);
  const zoomIn = () => setCalendarZoom(z => Math.min(CALENDAR_ZOOM_MAX, Math.round((z + CALENDAR_ZOOM_STEP) * 100) / 100));
  const zoomOut = () => setCalendarZoom(z => Math.max(CALENDAR_ZOOM_MIN, Math.round((z - CALENDAR_ZOOM_STEP) * 100) / 100));
  const zoomReset = () => setCalendarZoom(1);

  // showLegend: a lenda de cores do calendario (fin de semana/festivo/sen clase/día
  // actual) vive nun popover baixo unha icona, en vez de ocupar unha tarxeta enteira
  // sempre visible - legendRef pecha o popover ao clicar fóra del.
  const [showLegend, setShowLegend] = useState(false);
  const legendRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!showLegend) return;
    const onClickOutside = (e: MouseEvent) => {
      if (legendRef.current && !legendRef.current.contains(e.target as Node)) {
        setShowLegend(false);
      }
    };
    document.addEventListener('mousedown', onClickOutside);
    return () => document.removeEventListener('mousedown', onClickOutside);
  }, [showLegend]);

  // Ctrl+/Ctrl- (e Ctrl+0 para restaurar) achegan/alonxan o calendario dende calquera
  // sitio da app, non só con foco na grade - máis doado de descubrir e de usar. Hai que
  // interceptalos (preventDefault) porque, senón, é o propio WebView (WebKitGTK) quen os
  // colle primeiro e faría zoom de PÁXINA enteira (todo, non só o calendario).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey)) return;
      if (e.key === '+' || e.key === '=') { e.preventDefault(); zoomIn(); }
      else if (e.key === '-' || e.key === '_') { e.preventDefault(); zoomOut(); }
      else if (e.key === '0') { e.preventDefault(); zoomReset(); }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);
  // programmingLocked is true when the subjects come from the professor-authored
  // materias on the server: the student can then only publish daily logs, not edit the
  // calendar/units. It stays false in the legacy fallback (no materia assigned yet) so
  // existing users are not left with an empty, uneditable app.
  const [programmingLocked, setProgrammingLocked] = useState(false);
  // Professor mode: the student whose practices are being reviewed ('' = editing the
  // shared programación/modelo), and the roster to populate the selector.
  const [selectedStudent, setSelectedStudent] = useState('');
  // impersonating: the professor is viewing the platform as the selected student
  // (read-only) to check their work and uploaded files, without their password.
  const [impersonating, setImpersonating] = useState(false);
  // professorModelNoClass keeps, per materia, the non-lectivo day statuses (folga/saída/
  // ausencia/exame) AND the per-day unlock overrides of the professor's model programación,
  // so that reviewing/impersonating a student overlays the same reprogramming/estado/bloqueo
  // the student really sees (the student's own backup does not carry it). Kept fresh from
  // the modelo subjects while no student is selected.
  const professorModelNoClass = useRef<Record<string, Record<string, { status: DayStatus, unlocked?: boolean }>>>({});
  const [roster, setRoster] = useState<RosterRow[]>([]);
  // rawStudentBackup keeps the selected student's full backup so saving preserves any
  // materia not currently shown.
  const [rawStudentBackup, setRawStudentBackup] = useState<any[]>([]);
  // rawBackup does the same for the logged-in student's own backup: it may hold work of
  // materias they are no longer enrolled in, which publishing must not wipe.
  const [rawBackup, setRawBackup] = useState<any[]>([]);
  // noMaterias marks a connected student whose group has no materia published: there is
  // no programación to show and nothing they may edit.
  const [noMaterias, setNoMaterias] = useState(false);
  // erroMaterias distingue "o teu grupo non ten materia publicada" de "non se puideron
  // ler as materias" (permisos, servidor caído). Antes ambos os casos amosaban a mesma
  // pantalla, e un fallo de lectura parecía un problema de matrícula.
  const [erroMaterias, setErroMaterias] = useState('');
  // Messaging: whether the chat modal is open, and the unread badge for the current
  // context (the student's thread, or — for the professor — the selected student's).
  const [showMessages, setShowMessages] = useState(false);
  const [unreadCount, setUnreadCount] = useState(0);
  const [showBackup, setShowBackup] = useState(false);
  // Notas privadas (punto 3 do pedido): só o profesor autor da materia (ou un admin, á
  // parte en AdminPanel) as ve/edita; nunca viaxan polo backup do alumno nin polos
  // informes — ver GetPrivateNote/SavePrivateNote (tao/notas_privadas.go).
  const [showPrivateNotes, setShowPrivateNotes] = useState(false);
  const [showModulos, setShowModulos] = useState(false);
  // Reprogramación: confirmación pendente (mensaxe do efecto + acción a aplicar).
  const [reprogramReq, setReprogramReq] = useState<{ message: string; apply: () => void } | null>(null);
  // Admin messaging policy (wall/peer enabled, disabled groups/users). Defaults to all
  // enabled until the shared config is read.
  const [messagingConfig, setMessagingConfig] = useState<MessagingConfig>(() =>
    MessagingConfig.createFrom({ wallEnabled: true, peerEnabled: true, disabledGroups: [], disabledUsers: [] }));

  // Role-derived flags.
  const isProfessor = profile?.role === 'profesor';
  // The professor reviews a student's practices when one is selected; then the
  // programación is read-only and only the day practices can be edited.
  const reviewingStudent = isProfessor && selectedStudent !== '';
  // Whether calendar/units editing is disabled: students always have it locked when the
  // programación comes from the server; the professor only while reviewing a student.
  const effectiveProgrammingLocked = isProfessor ? reviewingStudent : programmingLocked;
  // Whether day cells can be opened: everyone may open a session (students to see the task
  // and submit their entrega; the professor to author/review). Writing to the calendar is a
  // separate concern (readOnlyLog): only the professor writes the programación/logs.
  const canEditDays = true;
  // readOnlyLog: the session record (status + práctica) is read-only only for the
  // professor while impersonating a student ("Acceder como", a read-only view). Students
  // write their own día's práctica; the professor writes it while authoring/reviewing.
  const readOnlyLog = isProfessor && impersonating;

  // workFileDates: sesións (data ISO) que teñen ficheiros de traballo subidos, para o
  // indicador visual do calendario (a raia negra baixo o día) — antes só reflectía
  // currentSubject.logs, así que unha sesión con ficheiros pero sen texto gardado
  // quedaba sen marcar. Cárgase nunha soa chamada por materia (ListWorkFileDates/
  // ListStudentWorkFileDates, traballos.go) en vez dunha petición por día. Só ten
  // sentido cando hai ficheiros que ver (alumnado conectado, ou profesorado revisando
  // un alumno) — nunca na programación "modelo", que non ten ficheiros propios.
  const [workFileDates, setWorkFileDates] = useState<Set<string>>(new Set());
  const refreshWorkFileDates = () => {
    if (!currentSubject) return;
    if (isProfessor && reviewingStudent && selectedStudent) {
      ListStudentWorkFileDates(selectedStudent, currentSubject.id).then(d => setWorkFileDates(new Set(d))).catch(() => {});
    } else if (!isProfessor && profile?.connected) {
      ListWorkFileDates(currentSubject.id).then(d => setWorkFileDates(new Set(d))).catch(() => {});
    } else {
      setWorkFileDates(new Set());
    }
  };
  useEffect(() => {
    refreshWorkFileDates();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentSubject?.id, isProfessor, reviewingStudent, selectedStudent, profile?.connected]);

  // Students of the active materia's groups: the professor's selector only lists the
  // alumnado matriculado nesa materia (matched by group).
  const materiaGroups = (currentSubject?.groups || []).map(g => g.trim().toLowerCase());
  const materiaStudents = isProfessor
    ? roster.filter(r => gruposDe(r.group).some(g => materiaGroups.includes(g.toLowerCase())))
    : [];

  // Recipients for the messaging panel. The professor gets the WHOLE roster (not only
  // the group-matched students), so they can always message someone even if the materia
  // has no groups assigned or the group names do not match the roster; the materia's own
  // students are listed first and the group is shown in the label.
  const inMateria = (g?: string) => materiaGroups.includes((g || '').trim().toLowerCase());
  const messageRecipients = isProfessor
    ? [...roster]
        .sort((a, b) => {
          const am = inMateria(a.group) ? 0 : 1;
          const bm = inMateria(b.group) ? 0 : 1;
          if (am !== bm) return am - bm;
          return [a.name, a.surname].join(' ').localeCompare([b.name, b.surname].join(' '));
        })
        .map(r => ({
          username: r.username,
          label: ([r.name, r.surname].filter(Boolean).join(' ') || r.username) + (r.group ? ` · ${r.group}` : ''),
          group: r.group || '',
        }))
    : [];

  useEffect(() => {
    if (!syncMsg) return;
    const t = setTimeout(() => setSyncMsg(''), 5000);
    return () => clearTimeout(t);
  }, [syncMsg]);

  // ---- Messaging unread badge ----------------------------------------------------
  // The counterpart of the current conversation: for a student it is always the
  // professorado; for a professor it is the student selected in the header (badge only
  // tracks that student's thread — a professor with no student selected sees no badge).
  const msgCounterpart = isProfessor ? selectedStudent : 'profesor';
  const seenKey = (materiaId: string, counterpart: string) => `piztu-msg-seen:${materiaId}:${counterpart}`;

  // handleSeen records that a thread was read up to ts (per materia + counterpart), so
  // the badge clears and stays cleared across sessions on this device. The counterpart
  // is reported by the modal (the professor may read any student, not just the header
  // one); the badge only zeroes when it matches the context it tracks.
  const handleSeen = (counterpart: string, ts: number) => {
    const matId = currentSubject?.id;
    if (!matId) return;
    const key = seenKey(matId, counterpart);
    if (ts > Number(localStorage.getItem(key) || 0)) localStorage.setItem(key, String(ts));
    if (counterpart === msgCounterpart) setUnreadCount(0);
  };

  // Load the admin messaging policy once connected (and refresh when opening the modal),
  // so the chat tabs/composer reflect what is enabled or blocked.
  useEffect(() => {
    if (!profile?.connected) return;
    GetMessagingConfig().then(setMessagingConfig).catch(() => {});
  }, [profile?.connected, showMessages]);

  // Poll the current thread to keep the unread badge fresh: on login, on materia/
  // student change, when the modal closes (the "seen" mark just moved) and every 45 s.
  useEffect(() => {
    const matId = currentSubject?.id;
    if (!profile?.connected || !matId) { setUnreadCount(0); return; }
    const counterpart = isProfessor ? selectedStudent : 'profesor';
    const otherSide = isProfessor ? 'alumno' : 'profesor';
    let cancelled = false;
    const run = async () => {
      if (isProfessor && !selectedStudent) { if (!cancelled) setUnreadCount(0); return; }
      try {
        const msgs = (isProfessor
          ? await GetStudentMessages(selectedStudent, matId)
          : await GetMyMessages(matId)) || [];
        const seen = Number(localStorage.getItem(seenKey(matId, counterpart)) || 0);
        const n = msgs.filter((m) => m.from === otherSide && m.ts > seen).length;
        if (!cancelled) setUnreadCount(n);
      } catch { /* servidor non dispoñible: manter o contador actual */ }
    };
    run();
    const id = setInterval(run, 45000);
    return () => { cancelled = true; clearInterval(id); };
  }, [profile?.connected, currentSubject?.id, isProfessor, selectedStudent, showMessages]);

  // buildStudentSubjects reconstructs the editable Subjects from the professor-authored
  // materia definitions (calendar + units, read-only for the student) merged with the
  // student's own work (logs/reports) kept in their backup, matched by subject id.
  const buildStudentSubjects = (materias: any[], backup: any[]): Subject[] =>
    materias.map((m: any) => {
      const prev = Array.isArray(backup) ? backup.find((s: any) => s.id === m.id) : null;
      // O alumno queda cos seus propios rexistros (logs/informes), pero o estado da sesión
      // (folga/saída/ausencia/exame) decídeo o profesor en m.logs, non cada alumno por
      // separado. Superpóñense eses estados sobre os logs do alumno para que tanto o
      // calendario (que despraza as unidades en días sen clase) coma o modal do día (que
      // amosa o estado en só lectura) reflictan sempre o que marcou o profesor.
      const studentLogs: Record<string, any> = { ...(prev?.logs || {}) };
      if (m.logs && typeof m.logs === 'object') {
        for (const [date, log] of Object.entries<any>(m.logs)) {
          const st = log?.status;
          if ((st && st !== 'lectivo') || log?.unlocked) {
            const hadOwn = !!studentLogs[date];
            // Márcase con _profReprog só cando o alumno non tiña rexistro propio ese día:
            // así é un marcador «prestado» do profesor que se elimina antes de gardar o
            // backup (stripReprogFlags), para que nunca quede obsoleto se o profesor
            // restaura o día como lectivo. Se o alumno xa tiña contido, consérvase.
            studentLogs[date] = {
              ...(studentLogs[date] || {}), date, status: st, unlocked: !!log?.unlocked,
              ...(hadOwn ? {} : { _profReprog: true }),
            };
          }
        }
      }
      return {
        id: m.id,
        name: m.name,
        academicYear: m.academicYear,
        isLocked: false,
        config: {
          ...defaultConfig,
          ...(m.config || {}),
          scheduleDays: m.config?.scheduleDays || [1, 3, 5],
          customHolidays: m.config?.customHolidays || [],
        },
        units: Array.isArray(m.units) ? m.units : [],
        logs: studentLogs,
        unitReports: prev?.unitReports || {},
        monthlyReports: prev?.monthlyReports || {},
        // Info xeral da programación (introdución, obxectivos, criterios, currículo/
        // avaliación por UD): o alumnado nunca chama a Proens (ver ProgramacionPanel.tsx),
        // só le o que o profesorado extraeu e publicou aquí.
        programacionInfo: m.programacionInfo,
        programacionInfoUpdatedAt: m.programacionInfoUpdatedAt,
      };
    });

  // stripReprogFlags devolve unha copia dos subjects sen os marcadores «prestados» do
  // profesor: os días que buildStudentSubjects engadiu con _profReprog (folga/saída/ausencia
  // que o alumno non tiña por conta propia) elimínanse enteiros. Úsase antes de gardar o
  // backup do alumno para que o calendario se re-derive sempre da programación vixente do
  // profesor e non quede pegado un día de folga que xa se restaurou como lectivo.
  const stripReprogFlags = (subs: Subject[]): Subject[] =>
    subs.map(s => {
      const logs: Record<string, any> = {};
      for (const [date, log] of Object.entries<any>(s.logs || {})) {
        if (log && log._profReprog) continue; // marcador prestado: descártase (re-derívase ao cargar)
        logs[date] = log;
      }
      return { ...s, logs };
    });

  // buildProfessorSubjects turns the shared materia definitions into editable Subjects
  // (calendar + units + groups), with empty logs: the professor designs the programación
  // here, and reviews a student's practices by selecting them in the header.
  const buildProfessorSubjects = (materias: any[]): Subject[] =>
    (materias || []).map((m: any) => ({
      id: m.id,
      name: m.name,
      academicYear: m.academicYear,
      isLocked: false,
      config: {
        ...defaultConfig,
        ...(m.config || {}),
        scheduleDays: m.config?.scheduleDays || [1, 3, 5],
        customHolidays: m.config?.customHolidays || [],
      },
      units: Array.isArray(m.units) ? m.units : [],
      groups: Array.isArray(m.groups) ? m.groups : [],
      // The professor's own session log (what was worked on / whether the class happened)
      // travels with the materia, so it is restored here for the "modelo" view.
      logs: (m.logs && typeof m.logs === 'object') ? m.logs : {},
      unitReports: {},
      monthlyReports: {},
      // Cache da última extracción de Proens publicada polo profesorado (esta mesma
      // sesión ou outra máquina) — ver programacionInfo en buildStudentSubjects.
      programacionInfo: m.programacionInfo,
      programacionInfoUpdatedAt: m.programacionInfoUpdatedAt,
    }));

  const handleLogin = async (p: StudentProfile) => {
    setProfile(p);
    if (!p.connected) return;

    // Profesor: mesma interface que o alumnado (materias editables + Configuración
    // completa) e un selector de alumno/a para revisar as súas prácticas.
    if (p.role === 'profesor') {
      try {
        // ListMyMaterias (non ListMaterias): un profesor só ve as súas propias materias
        // (máis calquera aínda sen dono, ver MateriaDef.Owner) — nunca as doutro profesor.
        const materias = (await ListMyMaterias()) || [];
        const built = buildProfessorSubjects(materias);
        if (built.length > 0) {
          setSubjects(built);
          setCurrentSubjectId(built[0].id);
        }
        setRoster((await ListRoster()) || []);
        setSelectedStudent('');
        setSyncMsg(built.length > 0
          ? t('app.sync.programacionCargada', { count: built.length })
          : t('app.sync.senMaterias'));
      } catch (e: any) {
        setSyncMsg(t('app.sync.erroCargarServidor', { error: e?.message || e }));
      }
      return;
    }

    // Alumno: as materias (calendario + unidades) veñen do servidor, definidas polo
    // profesor e filtradas polo grupo do alumno (GetMyMaterias); o traballo propio
    // (rexistros) vén do backup. Sen materias no seu grupo non hai nada que amosar.
    try {
      const json = await PullBackup();
      const parsed = json && json.trim() ? JSON.parse(json) : [];
      const backup = Array.isArray(parsed) ? parsed : [];
      let materias: any[] = [];
      let erroLendo = '';
      try {
        materias = (await GetMyMaterias()) || [];
      } catch (e: any) {
        erroLendo = e?.message || String(e);
      }

      // Gárdase o backup completo para que, ao publicar, non se borre o traballo das
      // materias que xa non lle tocan (p.ex. despois dun cambio de grupo).
      setRawBackup(backup);

      if (materias.length === 0) {
        setSubjects([]);
        setErroMaterias(erroLendo);
        setNoMaterias(true);
        return;
      }

      const built = buildStudentSubjects(materias, backup);
      setSubjects(built);
      setCurrentSubjectId(built[0].id);
      setProgrammingLocked(true);
      setNoMaterias(false);
      setErroMaterias('');
      setSyncMsg(t('app.sync.programacionDescargada', { count: built.length }));
    } catch (e: any) {
      setSyncMsg(t('app.sync.erroDescargarCopia', { error: e?.message || e }));
    }
  };

  // handleRefreshMaterias volve buscar as materias (calendario, unidades, estado das
  // sesións, desbloqueos) que publicou o profesor, sen tocar o traballo propio en
  // memoria (usa o rawBackup xa cargado como base, non volve pedir PullBackup): así o
  // alumnado pode ver decontado que o profesor desbloqueou unha práctica, por exemplo,
  // sen perder o que estea a escribir nese intre nin ter que volver a iniciar sesión.
  // silent=true (poll automático cada MATERIAS_POLL_MS, ver useEffect máis abaixo): non
  // toca syncMsg, para non facer parpadear "Actualizando/Actualizada" cada poucos
  // segundos - só actualiza subjects en silencio se hai cambios.
  const handleRefreshMaterias = async (silent = false) => {
    if (isProfessor || !profile?.connected) return;
    if (!silent) setSyncMsg(t('app.sync.actualizandoProgramacion'));
    try {
      const materias = (await GetMyMaterias()) || [];
      if (materias.length === 0) {
        if (!silent) setSyncMsg(t('app.sync.senMaterias'));
        return;
      }
      const built = buildStudentSubjects(materias, rawBackup);
      setSubjects(built);
      if (!built.some(s => s.id === currentSubjectId)) {
        setCurrentSubjectId(built[0].id);
      }
      if (!silent) setSyncMsg(t('app.sync.programacionActualizada'));
    } catch (e: any) {
      if (!silent) setSyncMsg(t('app.sync.erroActualizarProgramacion', { error: e?.message || e }));
    }
  };

  // Ref á versión máis recente de handleRefreshMaterias: o useEffect de abaixo só volve
  // executarse cando cambia isProfessor/profile.connected (case nunca despois de
  // conectar), así que sen isto o setInterval quedaría enganchado para sempre á foto de
  // rawBackup/currentSubjectId (e do resto do que handleRefreshMaterias captura) que
  // había no intre exacto de conectar - un rawBackup desactualizado faría que cada poll
  // silencioso sobrescribise subjects cun traballo do alumno máis vello có que hai
  // realmente en memoria/servidor.
  const handleRefreshMateriasRef = useRef(handleRefreshMaterias);
  handleRefreshMateriasRef.current = handleRefreshMaterias;

  // Chama a handleRefreshMaterias en segundo plano cada MATERIAS_POLL_MS mentres o
  // alumnado estea conectado - é o que fai que un desbloqueo do profesor apareza só,
  // sen premer "Actualizar" nin saír e volver entrar (ver comentario de
  // handleRefreshMaterias enriba). Non hai canle en tempo real co servidor (mesma
  // limitación ca FILES_POLL_MS en WorkFilesSection: isto é polling, non un
  // watchdog+evento).
  useEffect(() => {
    if (isProfessor || !profile?.connected) return;
    const id = setInterval(() => { handleRefreshMateriasRef.current(true); }, MATERIAS_POLL_MS);
    return () => clearInterval(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isProfessor, profile?.connected]);

  const handleLogout = () => {
    Logout().catch(() => {});
    setProfile(null);
    setProgrammingLocked(false);
    setSelectedStudent('');
    setImpersonating(false);
    setRoster([]);
    setRawStudentBackup([]);
    setRawBackup([]);
    setNoMaterias(false);
  };

  // handleSelectStudent (professor) overlays the chosen student's practices onto the
  // materias, or clears them to return to the editable programación ("modelo").
  const handleSelectStudent = async (username: string) => {
    setImpersonating(false); // cambiar de alumno no selector sae sempre da vista «Acceder como»
    setSelectedStudent(username);
    if (!username) {
      setRawStudentBackup([]);
      setSubjects(prev => prev.map(s => ({ ...s, logs: {}, unitReports: {}, monthlyReports: {} })));
      return;
    }
    try {
      const json = await GetStudentBackup(username);
      const arr = json && json.trim() ? JSON.parse(json) : [];
      const backup = Array.isArray(arr) ? arr : [];
      setRawStudentBackup(backup);
      setSubjects(prev => prev.map(s => {
        const w = backup.find((x: any) => x.id === s.id);
        // Superpóñense os estados non lectivos do modelo do profesor (folga/saída/ausencia/
        // exame) sobre os logs do alumno, para que a vista (revisión / «Acceder como»)
        // reflicta a mesma reprogramación e o mesmo estado de sesión que ve o alumno.
        const logs: Record<string, any> = { ...(w?.logs || {}) };
        const estadosProfesor = professorModelNoClass.current[s.id] || {};
        for (const [date, info] of Object.entries(estadosProfesor)) {
          const hadOwn = !!logs[date];
          // _profReprog marca o marcador prestado para eliminalo antes de gardar (stripReprogFlags),
          // igual que en buildStudentSubjects, para non ensuciar o backup do alumno.
          logs[date] = {
            ...(logs[date] || {}), date, status: info.status, unlocked: info.unlocked,
            ...(hadOwn ? {} : { _profReprog: true }),
          };
        }
        return { ...s, logs, unitReports: w?.unitReports || {}, monthlyReports: w?.monthlyReports || {} };
      }));
      setSyncMsg(t('app.sync.amosandoPracticas', { username }));
    } catch (e: any) {
      setSyncMsg(t('app.sync.erroCargarTraballoAlumno', { error: e?.message || e }));
    }
  };

  // enterImpersonation carga o traballo do alumno (como o selector) e activa a vista de só
  // lectura «Acceder como»: o profesor ve a plataforma tal e como a ve o alumno (tarefa,
  // entregas, comentarios) sen precisar o seu contrasinal e sen poder modificar nada.
  const enterImpersonation = async (username: string) => {
    if (!username) return;
    await handleSelectStudent(username);
    setImpersonating(true);
    const r = roster.find(x => x.username === username);
    setSyncMsg(t('app.sync.vendoPlataformaComo', { name: [r?.name, r?.surname].filter(Boolean).join(' ') || username }));
  };

  const exitImpersonation = () => {
    setImpersonating(false);
    handleSelectStudent('');
  };

  // If the professor switches to a materia the currently reviewed student is not enrolled
  // in, drop back to the editable programación (the student is not in this group).
  useEffect(() => {
    if (!isProfessor || !selectedStudent) return;
    if (!materiaStudents.some(r => r.username === selectedStudent)) {
      handleSelectStudent('');
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentSubjectId]);

  // Save to localStorage whenever subjects change (sen os marcadores prestados do profesor)
  useEffect(() => {
    localStorage.setItem('school-schedule-subjects', JSON.stringify(stripReprogFlags(subjects)));
  }, [subjects]);

  // While the professor edits the modelo (no student selected), snapshot its non-lectivo
  // day statuses (folga/saída/ausencia/exame) and unlock overrides per materia so a later
  // review/impersonation overlays the same reprogramming, session status and bloqueo.
  useEffect(() => {
    if (!isProfessor || selectedStudent) return;
    const snap: Record<string, Record<string, { status: DayStatus, unlocked?: boolean }>> = {};
    for (const s of subjects) {
      const m: Record<string, { status: DayStatus, unlocked?: boolean }> = {};
      for (const [date, log] of Object.entries<any>(s.logs || {})) {
        if (log && ((log.status && log.status !== 'lectivo') || log.unlocked)) {
          m[date] = { status: log.status, unlocked: !!log.unlocked };
        }
      }
      snap[s.id] = m;
    }
    professorModelNoClass.current = snap;
  }, [subjects, isProfessor, selectedStudent]);

  const updateCurrentSubject = (updates: Partial<Subject>) => {
    setSubjects(prev => prev.map(s => s.id === currentSubjectId ? { ...s, ...updates } : s));
  };

  // publicarMateria envía (asíncrono, sen bloquear a UI) unha materia enteira ao
  // servidor — calendario/unidades/grupos, o log de sesións do profesor E a caché de
  // programación de Proens (programacionInfo). É o único punto que chama SaveMateria coa
  // materia "modelo" do profesorado; TODO cambio que faga o profesorado sobre a súa propia
  // materia (fóra de modo revisión) pasa por aquí decontado — non hai xa ningún camiño que
  // dependa de premer un botón para que o alumnado o vexa (nin sequera "Sincronizar co
  // alumnado", que segue existindo só coma reintento manual se algo fallou). Non amosa
  // mensaxes por si soa: cada chamador decide o texto de éxito/erro (ver sincronizarMateria
  // máis abaixo, para os casos que non teñen unha mensaxe propia coma handleSaveLog).
  const publicarMateria = (updated: Subject) => SaveMateria(MateriaDef.createFrom({
    id: updated.id, name: updated.name, academicYear: updated.academicYear,
    groups: updated.groups || [], config: updated.config, units: updated.units,
    logs: updated.logs || {},
    programacionInfo: updated.programacionInfo,
    programacionInfoUpdatedAt: updated.programacionInfoUpdatedAt,
  }));

  // sincronizarMateria envolve publicarMateria coa mensaxe xenérica de "cambios
  // sincronizados" (a mesma que xa amosaba o antigo botón "Publicar programación"), para
  // os camiños que non teñen unha mensaxe de éxito máis específica propia (handleSaveLog
  // si a ten: "sesión publicada"). Nunca lanza — o chamador non ten que capturar nada.
  const sincronizarMateria = (updated: Subject) => {
    publicarMateria(updated)
      .then(() => setSyncMsg(t('app.sync.programacionPublicada', { name: updated.name })))
      .catch((e: any) => setSyncMsg(t('app.sync.erroPublicarProgramacion', { error: e?.message || e })));
  };

  // onProgramacionInfoExtraida centraliza o que pasa cando se extrae (ou reextrae)
  // calendario/Proens, tanto dende Configuración (handleImport, xunto coas URLs) coma
  // dende o panel de programación (o botón "Actualizar", só para a info de Proens):
  // actualízase a materia en local decontado, e — só se é o profesorado editando a súa
  // propia materia, nunca en modo revisión nin como alumnado — publícase no servidor sen
  // agardar a "Aplicar cambios" nin a "Sincronizar co alumnado", para que nada se perda se
  // se peche a xanela con "Cancelar" ou se acceda dende outro equipo despois.
  const onProgramacionInfoExtraida = (data: { proensUrl?: string, dogUrl?: string, programacionInfo?: ProensInfo }) => {
    const updates: Partial<Subject> = {};
    if (data.proensUrl !== undefined || data.dogUrl !== undefined) {
      updates.config = {
        ...currentSubject.config,
        ...(data.proensUrl !== undefined ? { proensUrl: data.proensUrl } : {}),
        ...(data.dogUrl !== undefined ? { dogUrl: data.dogUrl } : {}),
      };
    }
    if (data.programacionInfo) {
      updates.programacionInfo = data.programacionInfo;
      updates.programacionInfoUpdatedAt = new Date().toISOString();
    }
    if (Object.keys(updates).length === 0) return;
    updateCurrentSubject(updates);
    if (isProfessor && !reviewingStudent) {
      sincronizarMateria({ ...currentSubject, ...updates });
    }
  };

  const { months, stats } = useMemo(() => {
    if (!currentSubject) return { months: [], stats: { totalClassDays: 0, totalUnitSessions: 0, totalSchoolDays: 0 } };
    return generateMonthlySchedule(currentSubject.config, currentSubject.units, currentSubject.logs, i18n.language);
  }, [currentSubject, i18n.language]);

  // close=false: usado polo botón de informe asinado do DayModal (ver
  // onQuickSignedReport, máis abaixo) - quere gardar a práctica coma "Gardar
  // práctica" pero SEN pechar a xanela, porque amosa o progreso do informe e
  // actualiza a lista de ficheiros adxuntos na propia xanela aberta.
  const handleSaveLog = async (log: DailyLog, opts: { close?: boolean } = {}) => {
    const { close = true } = opts;
    if (currentSubject.isLocked) return;
    const updatedSubjects = subjects.map(s =>
      s.id === currentSubjectId ? { ...s, logs: { ...s.logs, [log.date]: log } } : s
    );
    setSubjects(updatedSubjects);
    if (close) setSelectedDay(null);

    // When the professor is reviewing a student, persist the change (the corrected
    // práctica and/or the feedback comment) to the student's backup right away, so it
    // reaches them without needing the separate "Gardar traballo" button.
    if (isProfessor && reviewingStudent && selectedStudent) {
      try {
        const others = (rawStudentBackup || []).filter((s: any) => !updatedSubjects.some(cur => cur.id === s.id));
        const next = [...others, ...stripReprogFlags(updatedSubjects)];
        await SaveStudentBackup(selectedStudent, JSON.stringify(next));
        setRawStudentBackup(next);
        setSyncMsg(t('app.sync.gardadoTraballoDe', { username: selectedStudent }));
      } catch (e: any) {
        setSyncMsg(t('app.sync.erroGardarServidor', { error: e?.message || e }));
      }
      return;
    }

    // Profesorado editando a programación modelo (estado da sesión, desbloqueo, contidos
    // da sesión...), non revisando un alumno: publícase decontado ESTA materia no
    // servidor, igual que xa fai o alumnado coa súa propia práctica (ver máis abaixo) -
    // antes non se facía, e o cambio quedaba só en local ata premer "Sincronizar co
    // alumnado" á parte, o que facía parecer que un desbloqueo "non collía" no alumnado.
    // "Sincronizar co alumnado" segue existindo para o que non se publica só (editar
    // calendario/unidades/grupos en Configuración, reprogramar sesións, importar un CSV).
    if (isProfessor && !reviewingStudent) {
      const updated = updatedSubjects.find(s => s.id === currentSubjectId);
      if (updated) {
        try {
          await publicarMateria(updated);
          setSyncMsg(t('app.sync.sesionPublicada'));
        } catch (e: any) {
          setSyncMsg(t('app.sync.erroPublicarSesion', { error: e?.message || e }));
        }
      }
      return;
    }

    // Alumno gardando a súa propia práctica: publícase decontado no servidor (se hai
    // sesión activa), sen depender de que prema despois no botón "Gardar" xeral. É clave
    // para que o equipo empregado quede rexistrado no JSON do servidor aínda que peche
    // Tao sen máis, para poder localizar responsables en caso de desperfectos.
    if (!isProfessor && profile?.connected) {
      try {
        const cleaned = stripReprogFlags(updatedSubjects);
        const others = (rawBackup || []).filter((s: any) => !updatedSubjects.some(cur => cur.id === s.id));
        const next = [...others, ...cleaned];
        await PushBackup(JSON.stringify(next));
        setRawBackup(next);
        setSyncMsg(t('app.sync.practicaGardadaServidor'));
      } catch (e: any) {
        setSyncMsg(t('app.sync.erroGardarServidor', { error: e?.message || e }));
      }
    }
  };

  // handleSaveStudentComment persists the professor's feedback comment on a student's
  // práctica immediately (via SaveStudentLogComment), and reflects it in local state so
  // the modal and the eventual "Gardar práctica" stay consistent. Used by the draggable
  // comment panel.
  const handleSaveStudentComment = async (date: string, comment: string) => {
    if (!isProfessor || !selectedStudent) return;
    await SaveStudentLogComment(selectedStudent, currentSubjectId, date, comment);
    setSubjects(prev => prev.map(s => s.id === currentSubjectId
      ? { ...s, logs: { ...s.logs, [date]: { ...(s.logs[date] || { date, content: '', status: 'lectivo' as DayStatus }), profesorComment: comment } } }
      : s));
    setSyncMsg(t('app.sync.comentarioGardadoTraballoDe', { username: selectedStudent }));
  };

  // handleAddIncident/handleRemoveIncident: mesmo patrón ca handleSaveStudentComment —
  // gárdase decontado no backup do alumno (AddIncident/RemoveIncident, tao/incidencias.go)
  // e reflíctese en local para que a grella e o DayModal queden ao día sen recargar.
  const handleAddIncident = async (date: string, type: IncidentType, note: string) => {
    if (!isProfessor || !selectedStudent) return;
    // O id definitivo vén do servidor (AddIncident), non se inventa aquí: RemoveIncident
    // ten que referirse exactamente ao que quedou gardado no backup do alumno.
    const incident = await AddIncident(selectedStudent, currentSubjectId, date, type, note) as Incident;
    setSubjects(prev => prev.map(s => s.id === currentSubjectId
      ? {
          ...s, logs: {
            ...s.logs,
            [date]: {
              ...(s.logs[date] || { date, content: '', status: 'lectivo' as DayStatus }),
              incidents: [...(s.logs[date]?.incidents || []), incident],
            },
          },
        }
      : s));
  };

  const handleRemoveIncident = async (date: string, incidentID: string) => {
    if (!isProfessor || !selectedStudent) return;
    await RemoveIncident(selectedStudent, currentSubjectId, date, incidentID);
    setSubjects(prev => prev.map(s => s.id === currentSubjectId
      ? { ...s, logs: { ...s.logs, [date]: { ...s.logs[date], incidents: (s.logs[date]?.incidents || []).filter(i => i.id !== incidentID) } } }
      : s));
  };

  // handleReprogram applies a session reprogramming (mark no-class / clear / insert / delete)
  // to the professor's model: it shifts the per-session content and adjusts unit session
  // counts, then asks for confirmation showing the effect. Modelo (non-review) only.
  const handleReprogram = (date: string, action: ReprogramAction, count = 1) => {
    if (!isProfessor || reviewingStudent) return;
    const res = applyReprogram(currentSubject, date, action, count);
    const verb =
      action.kind === 'markNoClass' ? t('app.reprogram.marcaraseSenClase') :
      action.kind === 'clearNoClass' ? t('app.reprogram.restauraraseLectivo') :
      action.kind === 'insert' ? t('app.reprogram.inseriranse', { count }) :
      t('app.reprogram.eliminaranse', { count });
    const bits = [verb];
    if (res.unitsNote) bits.push(res.unitsNote);
    if (res.overflow > 0) bits.push(t('app.reprogram.overflow', { count: res.overflow }));
    setReprogramReq({
      message: bits.join('\n\n'),
      apply: () => {
        const updates = { logs: res.logs, units: res.units };
        updateCurrentSubject(updates);
        setReprogramReq(null);
        setSelectedDay(null);
        setSyncMsg(t('app.sync.programacionReaxustada'));
        // Publícase decontado — a reprogramación xa non depende de premer "Sincronizar co
        // alumnado" á parte (ver publicarMateria/sincronizarMateria máis arriba).
        sincronizarMateria({ ...currentSubject, ...updates });
      },
    });
  };

  const handleCreateSubject = () => {
    const newSubject = createDefaultSubject();
    newSubject.name = t('app.novaMateria', { count: subjects.length + 1 });
    newSubject.units = [];
    setSubjects([...subjects, newSubject]);
    setCurrentSubjectId(newSubject.id);
  };

  const handleDeleteSubject = (id: string) => {
    // Se é a única materia, substitúese por unha nova EN BRANCO en vez de deixar a
    // lista baleira: o resto da app asume que sempre hai unha currentSubject (ver o
    // comentario en subjects, arriba), así que unha lista baleira faría petar a
    // interface. "En branco" = mesmo criterio ca handleCreateSubject (nome xenérico e
    // sen unidades) — createDefaultSubject() a secas trae o nome e as unidades de
    // exemplo ("Tecnoloxía e Dixitalización"), o que non é partir de cero de verdade.
    const newSubjects = subjects.length === 1
      ? [{ ...createDefaultSubject(), name: t('app.novaMateria', { count: 1 }), units: [] }]
      : subjects.filter(s => s.id !== id);
    setSubjects(newSubjects);
    if (currentSubjectId === id) {
      setCurrentSubjectId(newSubjects[0].id);
    }
    // O profesor, en modo programación, elimina tamén a materia compartida no servidor.
    if (isProfessor && !reviewingStudent) {
      DeleteMateria(id)
        .then(() => setSyncMsg(t('app.sync.materiaEliminada')))
        .catch((e: any) => setSyncMsg(t('app.sync.erroEliminarMateria', { error: e?.message || e })));
    }
  };

  const toggleLock = () => {
    updateCurrentSubject({ isLocked: !currentSubject.isLocked });
  };

  // handleExportBackup: só para o alumnado (o profesorado xa non ten botón — toda edición
  // súa publícase soa decontado, ver publicarMateria/sincronizarMateria). Garda a súa
  // propia práctica no servidor de ficheiros se hai sesión, ou descarga unha copia local
  // se non a hai.
  const handleExportBackup = async () => {
    // Se hai sesión no servidor de ficheiros, gárdase alí unha copia.
    if (profile?.connected) {
      setSyncMsg(t('app.sync.gardandoServidorFicheiros'));
      try {
        // O backup pode conter materias que xa non se cursan (un cambio de grupo deixa
        // de amosalas): consérvanse tal cal en vez de perdelas ao sobrescribir.
        const cleaned = stripReprogFlags(subjects);
        const others = (rawBackup || []).filter((s: any) => !subjects.some(cur => cur.id === s.id));
        const next = [...others, ...cleaned];
        const name = await PushBackup(JSON.stringify(next));
        setRawBackup(next);
        setSyncMsg(t('app.sync.copiaGardadaServidor', { name }));
      } catch (e: any) {
        setSyncMsg(t('app.sync.erroGardarServidor', { error: e?.message || e }));
      }
      return;
    }
    // Modo local: descarga o ficheiro de copia.
    const dataStr = JSON.stringify(stripReprogFlags(subjects), null, 2);
    const blob = new Blob([dataStr], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `backup-dixitalizacion-${new Date().toISOString().split('T')[0]}.json`;
    link.click();
    URL.revokeObjectURL(url);
  };

  const handleImportBackup = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (event) => {
      try {
        const imported = JSON.parse(event.target?.result as string);
        if (Array.isArray(imported) && imported.length > 0 && imported[0].id) {
          setSubjects(imported);
          setCurrentSubjectId(imported[0].id);
          alert(t('app.alert.datosImportadosExito'));
        } else {
          alert(t('app.alert.formatoIncorrecto'));
        }
      } catch (err) {
        alert(t('app.alert.erroLerJsonBackup'));
      }
    };
    reader.readAsText(file);
    // Reset file input
    e.target.value = '';
  };

  const todayString = new Date().toISOString().split('T')[0];

  const adjustUnitSessions = (unitId: number, delta: number) => {
    if (currentSubject.isLocked) return;
    updateCurrentSubject({
      units: currentSubject.units.map(u => {
        if (u.id === unitId) {
          const newSessions = Math.max(1, u.sessions + delta);
          return { ...u, sessions: newSessions };
        }
        return u;
      })
    });
  };

  if (!profile) {
    return <Login onLogin={handleLogin} />;
  }

  // Modo administración: interface distinta, sen calendario nin unidades didácticas.
  if (profile.isAdmin) {
    return <AdminPanel onLogout={handleLogout} />;
  }

  // Alumnado sen materias no seu grupo: non hai programación que amosar nin nada que
  // editar. Amósase o aviso en lugar de caer nunha materia de exemplo, que só levaría
  // a rexistrar traballo nun calendario que non é o da súa clase.
  if (noMaterias) {
    return <NoMateriasScreen profile={profile} erro={erroMaterias} onLogout={handleLogout} />;
  }

  if (!currentSubject) return null;

  return (
    <div className="min-h-screen bg-stone-100 font-sans text-stone-900 selection:bg-stone-200 pb-12 print:bg-white print:pb-0">
      {/* Header */}
      <header className="bg-white border-b border-stone-200 sticky top-0 z-30 shadow-sm print:hidden">
        <div className="max-w-[1600px] mx-auto px-4 sm:px-6 lg:px-8 py-4 flex flex-col lg:flex-row items-center justify-between gap-4">
          <div className="flex items-start gap-4 w-full lg:w-auto">
            <div className="flex items-center gap-2.5 shrink-0" title="Tao">
              <div className="p-1 rounded-lg bg-piztu-primary-soft border border-piztu-primary-soft-border">
                <Logo className="w-9 h-9" />
              </div>
              <span className="font-extrabold text-stone-900 tracking-tight hidden xl:inline">Tao</span>
            </div>
            <div className="flex-1 lg:flex-none flex flex-col gap-3">
              {/* relative z-10: cando isProfessor amosa unha segunda fila debaixo (o
                  selector de alumnado), esta fila queda sempre por riba en caso de
                  solapamento físico entre as dúas — sen isto, os botóns +/🗑 podían
                  quedar tapados en stacking pola fila de embaixo (que vén despois no
                  DOM) aínda que visualmente parecesen libres. */}
              <div className="relative z-10 flex items-center gap-3">
                <select
                  value={currentSubjectId}
                  onChange={(e) => setCurrentSubjectId(e.target.value)}
                  className="bg-stone-50 border border-stone-200 text-stone-800 text-sm rounded-lg focus:ring-piztu-primary focus:border-piztu-primary block p-2 font-bold w-full lg:w-[250px]"
                >
                  {subjects.map(s => (
                    <option key={s.id} value={s.id}>{s.name} ({s.academicYear})</option>
                  ))}
                </select>

                {!effectiveProgrammingLocked && (
                  <button onClick={handleCreateSubject} className="p-2 text-stone-500 hover:text-stone-900 hover:bg-stone-100 rounded-lg transition-colors shrink-0" title={t('app.novaMateriaTitulo')}>
                    <Plus className="w-5 h-5" />
                  </button>
                )}
                {!effectiveProgrammingLocked && (
                  <button onClick={() => handleDeleteSubject(currentSubjectId)} className="p-2 text-stone-500 hover:text-rose-600 hover:bg-rose-50 rounded-lg transition-colors shrink-0" title={subjects.length === 1 ? t('app.eliminarUltimaMateriaTitulo') : t('app.eliminarMateriaTitulo')}>
                    <Trash2 className="w-5 h-5" />
                  </button>
                )}
              </div>

            {isProfessor && (
              <div className="flex items-center gap-3">
                {/* O valor real por baixo (selectedStudent) segue a ser '' para "Programación
                    (modelo)" - iso non cambia, hai 20+ sitios no código que o dan por feito.
                    O que cambia é só a caixa pechada do <select>: en vez de amosar
                    "Programación (modelo)" por defecto (confuso, semella un alumno máis),
                    amosa "Alumnado" coma placeholder (MODELO_PLACEHOLDER, disabled+hidden -
                    non seleccionable, só é o que se ve cando value=''). "Programación
                    (modelo)" pasa a ser unha fila propia (MODELO_OPTION_VALUE) que, ao
                    premela, traduce de volta a '' - así o resto do código nin se decata.
                    Truco: se volves premer "Programación (modelo)" despois de ver un
                    alumno, a caixa pechada volve dicir "Alumnado" (comparten o mesmo valor
                    interno ''), non "Programación (modelo)" outra vez - aceptable porque a
                    vista principal (o propio calendario) xa amosa claramente que mudou. */}
                <select
                  value={selectedStudent === '' ? MODELO_PLACEHOLDER_VALUE : selectedStudent}
                  onChange={(e) => handleSelectStudent(e.target.value === MODELO_OPTION_VALUE ? '' : e.target.value)}
                  title={t('app.escolleAlumnoTitulo')}
                  className="bg-stone-50 border border-stone-200 text-stone-800 text-sm rounded-lg focus:ring-piztu-primary focus:border-piztu-primary block p-2 font-medium w-full lg:w-[250px]"
                >
                  <option value={MODELO_PLACEHOLDER_VALUE} disabled hidden>{t('app.alumnado')}</option>
                  <option value={MODELO_OPTION_VALUE}>{t('app.programacionModelo')}</option>
                  <optgroup label={t('app.alumnado')}>
                    {materiaStudents.map(r => (
                      <option key={r.username} value={r.username}>
                        {[r.name, r.surname].filter(Boolean).join(' ') || r.username}{r.group ? ` · ${r.group}` : ''}
                      </option>
                    ))}
                  </optgroup>
                </select>
                {selectedStudent && !impersonating && (
                  <button
                    onClick={() => enterImpersonation(selectedStudent)}
                    title={t('app.verComoAlumnoTitulo')}
                    className="flex items-center gap-1.5 px-3 py-2 text-sm bg-violet-100 text-violet-700 hover:bg-violet-200 rounded-lg font-medium transition-colors shrink-0"
                  >
                    <Eye className="w-4 h-4" /> {t('app.accederComo')}
                  </button>
                )}
                {selectedStudent && !impersonating && (
                  <button
                    onClick={() => setShowPrivateNotes(true)}
                    title={t('app.notasPrivadas.titulo')}
                    className="flex items-center gap-1.5 px-3 py-2 text-sm bg-stone-800 text-white hover:bg-stone-700 rounded-lg font-medium transition-colors shrink-0"
                  >
                    <Pencil className="w-4 h-4" /> {t('app.notasPrivadas.boton')}
                  </button>
                )}
                {impersonating && (
                  <button
                    onClick={exitImpersonation}
                    title={t('app.sairVistaAlumnoTitulo')}
                    className="flex items-center gap-1.5 px-3 py-2 text-sm bg-stone-800 text-white hover:bg-stone-700 rounded-lg font-medium transition-colors shrink-0"
                  >
                    <X className="w-4 h-4" /> {t('app.sairDaVista')}
                  </button>
                )}
              </div>
            )}
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2 w-full lg:w-auto justify-end">
            {!effectiveProgrammingLocked && (
              <button
                onClick={toggleLock}
                className={`flex items-center gap-2 px-4 py-2 rounded-lg font-medium transition-colors text-sm ${
                  currentSubject.isLocked
                    ? 'bg-rose-100 text-rose-700 hover:bg-rose-200'
                    : 'bg-emerald-100 text-emerald-700 hover:bg-emerald-200'
                }`}
              >
                {currentSubject.isLocked ? <Lock className="w-4 h-4" /> : <Unlock className="w-4 h-4" />}
                {currentSubject.isLocked ? t('app.bloqueado') : t('app.desbloqueado')}
              </button>
            )}

            <div className="flex items-center gap-0.5 bg-stone-100 border border-stone-200 rounded-lg p-1">
              {!effectiveProgrammingLocked && (
                <label className="flex items-center gap-2 px-3 py-1.5 hover:bg-stone-200 text-stone-700 rounded-md font-medium transition-colors text-sm cursor-pointer">
                  <Upload className="w-4 h-4" />
                  {t('app.abrir')}
                  <input
                    type="file"
                    accept=".json"
                    onChange={handleImportBackup}
                    className="hidden"
                  />
                </label>
              )}
              {/* Actualizar: só para o alumnado conectado. Recolle os cambios que fixo o
                  profesor (estado da sesión, desbloqueos, unidades...) sen perder o traballo
                  en curso, para non ter que peschar sesión e volver entrar para velos. */}
              {!isProfessor && profile?.connected && (
                <button
                  onClick={handleRefreshMaterias}
                  title={t('app.actualizarProgramacionTitulo')}
                  className="flex items-center gap-2 px-3 py-1.5 hover:bg-stone-200 text-stone-700 rounded-md font-medium transition-colors text-sm"
                >
                  <RefreshCw className="w-4 h-4" />
                  {t('app.actualizar')}
                </button>
              )}
              {/* Só para o alumnado: gardar (no servidor, ou descarga local sen conexión). O
                  profesorado xa non ten nada que resolver aquí — toda edición súa publícase soa
                  decontado (ver publicarMateria/sincronizarMateria), así que o antigo botón
                  "Sincronizar co alumnado" desapareceu. Mentres se «accede como» un alumno a
                  vista é de só lectura: non se garda nada. En modo revisión (reviewingStudent)
                  tampouco hai nada que resolver: cada corrección (handleSaveLog) e cada
                  comentario (handleSaveStudentComment) xa se publica decontado no backup do
                  alumno en canto se garda. */}
              {!impersonating && !isProfessor && (
              <button
                onClick={handleExportBackup}
                className="flex items-center gap-2 px-3 py-1.5 rounded-md font-medium transition-colors text-sm hover:bg-stone-200 text-stone-700"
                title={profile.connected ? t('app.gardarCopiaServidorTitulo') : t('app.descargarCopiaLocalTitulo')}
              >
                <Save className="w-4 h-4" />
                {profile.connected ? t('app.gardarNoServidor') : t('app.gardar')}
              </button>
              )}
            </div>
            {/* Os informes de seguimento (unidades, mensuais) son cousa do profesorado:
                ao alumnado abóndalle co informe asinado das súas prácticas. */}
            {isProfessor && !impersonating && (
              <div className="flex items-center gap-0.5 bg-stone-100 border border-stone-200 rounded-lg p-1">
                <button
                  onClick={() => setShowReports(true)}
                  className="flex items-center gap-2 px-3 py-1.5 hover:bg-stone-200 text-stone-700 rounded-md font-medium transition-colors text-sm"
                >
                  <FileText className="w-4 h-4" />
                  {t('app.informes')}
                </button>
                <button
                  onClick={() => setShowBackup(true)}
                  className="flex items-center gap-2 px-3 py-1.5 hover:bg-stone-200 text-stone-700 rounded-md font-medium transition-colors text-sm"
                  title={t('app.copiasSeguridadeTitulo')}
                >
                  <Database className="w-4 h-4" />
                  {t('app.copias')}
                </button>
                <button
                  onClick={() => setShowModulos(true)}
                  className="flex items-center gap-2 px-3 py-1.5 hover:bg-stone-200 text-stone-700 rounded-md font-medium transition-colors text-sm"
                  title={t('modulos.titulo', 'Módulos de Tao')}
                >
                  <Package className="w-4 h-4" />
                  {t('modulos.botonBarra', 'Módulos')}
                </button>
              </div>
            )}
            <button
              onClick={() => setShowMessages(true)}
              className="relative flex items-center gap-2 px-4 py-2 bg-stone-100 hover:bg-stone-200 text-stone-700 rounded-lg font-medium transition-colors text-sm"
              title={t('app.mensaxesTitulo')}
            >
              <MessageSquare className="w-4 h-4" />
              {t('app.mensaxes')}
              {unreadCount > 0 && (
                <span className="absolute -top-1.5 -right-1.5 bg-rose-600 text-white text-[11px] font-bold rounded-full min-w-[18px] h-[18px] px-1 flex items-center justify-center">
                  {unreadCount}
                </span>
              )}
            </button>
            <div className="flex items-center gap-0.5 bg-stone-100 border border-stone-200 rounded-lg p-1">
              <button
                onClick={() => setShowSettings(true)}
                className="flex items-center gap-2 px-3 py-1.5 hover:bg-stone-200 text-stone-700 rounded-md font-medium transition-colors text-sm"
                disabled={currentSubject.isLocked}
              >
                <Settings className="w-4 h-4" />
                {t('app.configuracion')}
              </button>
              <button
                onClick={() => setShowSobre(true)}
                title={t('sobre.btn', 'ℹ Sobre Tao')}
                className={`flex items-center px-2.5 py-1.5 rounded-md font-medium transition-colors text-sm ${actualizacionTaoDispo ? 'bg-amber-500 hover:bg-amber-600 text-white' : 'hover:bg-stone-200 text-stone-700'}`}
              >
                <Info className="w-4 h-4" />
              </button>
            </div>

            <div className="flex items-center gap-2 pl-2 ml-1 border-l border-stone-200">
              <div className="flex flex-col items-end leading-tight">
                <span className="text-sm font-bold text-stone-800">{profile.name} {profile.surname}</span>
                <span className="text-xs text-stone-500 flex items-center gap-1">
                  {profile.group ? profile.group + ' · ' : ''}
                  {profile.connected
                    ? <span className="inline-flex items-center gap-0.5 text-piztu-on">{t('app.servidorOk')} · </span>
                    : null}
                  {profile.hasCert
                    ? <span className="inline-flex items-center gap-0.5 text-emerald-600"><ShieldCheck className="w-3 h-3" /> {t('app.certificado')}</span>
                    : <span className="text-amber-600">{t('app.senCertificado')}</span>}
                </span>
              </div>
              <button
                onClick={handleLogout}
                className="p-2 text-stone-500 hover:text-rose-600 hover:bg-rose-50 rounded-lg transition-colors"
                title={t('app.pecharSesionTitulo')}
              >
                <LogOut className="w-5 h-5" />
              </button>
            </div>
          </div>
        </div>
      </header>

      {impersonating && (() => {
        const r = roster.find(x => x.username === selectedStudent);
        const label = [r?.name, r?.surname].filter(Boolean).join(' ') || selectedStudent;
        return (
          <div className="bg-violet-600 text-white text-sm px-4 py-2 flex items-center justify-center gap-3 print:hidden">
            <Eye className="w-4 h-4 shrink-0" />
            <span>{t('app.vendoComoBannerPrefixo')} <strong>{label}</strong> {t('app.vendoComoBannerSufixo')}</span>
            <button onClick={exitImpersonation} className="ml-2 px-3 py-1 bg-white/20 hover:bg-white/30 rounded-md font-medium transition-colors shrink-0">{t('app.sair')}</button>
          </div>
        );
      })()}

      {syncMsg && (
        <div className="fixed bottom-4 right-4 z-50 max-w-sm bg-stone-900 text-white text-sm px-4 py-3 rounded-xl shadow-lg print:hidden">
          {syncMsg}
        </div>
      )}

      <main className={`max-w-[1600px] mx-auto px-4 sm:px-6 lg:px-8 py-8 print:py-0 print:px-0 ${showReports ? 'print:hidden' : ''}`}>
        
        {stats.totalUnitSessions > stats.totalClassDays && (
          <div className="mb-6 bg-rose-50 border border-rose-200 text-rose-800 px-4 py-3 rounded-xl flex items-start gap-3 print:hidden">
            <AlertCircle className="w-5 h-5 shrink-0 mt-0.5" />
            <div>
              <h4 className="font-bold">{t('app.avisoPlanificacion')}</h4>
              <p className="text-sm">{t('app.avisoPlanificacionTexto', { programadas: stats.totalUnitSessions, dispoñibles: stats.totalClassDays })}</p>
            </div>
          </div>
        )}

        <div className="mb-6 print:hidden">
          <div className="bg-white p-4 rounded-xl border border-stone-200 shadow-sm flex flex-col">
            <div className="flex items-center justify-between gap-2 mb-1">
              <div className="flex items-center gap-2 text-stone-500">
                <BookOpen className="w-4 h-4" />
                <span className="text-sm font-medium uppercase tracking-wider">{t('app.materia')}</span>
              </div>
              <button
                onClick={() => { setFocusedProensUnit(null); setShowProgramacionPanel(true); }}
                title={t('app.programacion.abrirTitulo')}
                className="p-1.5 text-stone-400 hover:text-stone-700 hover:bg-stone-100 rounded-lg transition-colors shrink-0"
              >
                <ClipboardList className="w-4 h-4" />
              </button>
            </div>
            <input
              type="text"
              value={currentSubject.name}
              onChange={(e) => updateCurrentSubject({ name: e.target.value })}
              disabled={currentSubject.isLocked || effectiveProgrammingLocked}
              className="text-xl font-bold text-stone-900 bg-transparent border-none p-0 focus:ring-0 w-full"
            />
            <input
              type="text"
              value={currentSubject.academicYear}
              onChange={(e) => updateCurrentSubject({ academicYear: e.target.value })}
              disabled={currentSubject.isLocked || effectiveProgrammingLocked}
              className="text-sm text-stone-500 bg-transparent border-none p-0 focus:ring-0 mt-1 w-full"
            />
            {/* Resumo de sesións: integrado aquí (en pequeno) en vez de nun card á
                parte, para non ocupar tanto espazo na pantalla. */}
            <div className="flex items-center gap-1.5 mt-2 pt-2 border-t border-stone-100 text-xs text-stone-500">
              <Clock className="w-3.5 h-3.5 shrink-0" />
              <span className={`font-bold ${stats.totalUnitSessions > stats.totalClassDays ? 'text-rose-600' : 'text-emerald-600'}`}>
                {stats.totalUnitSessions}
              </span>
              <span>/</span>
              <span className="font-bold text-stone-700">{stats.totalClassDays}</span>
              <span>{t('app.sesionsProgramadas')}</span>
            </div>
          </div>
        </div>

        <div className="mb-6 flex flex-wrap gap-2 print:hidden">
          {currentSubject.units.map(unit => (
            <div
              key={unit.id}
              onClick={() => { setFocusedProensUnit(unit); setShowProgramacionPanel(true); }}
              title={t('app.programacion.verDetalleUnidade')}
              className={`flex items-center gap-2 px-3 py-1.5 rounded-lg border border-stone-200 bg-white shadow-sm text-sm cursor-pointer hover:border-indigo-300 hover:shadow transition-shadow`}
            >
              <div className={`w-3 h-3 rounded-full ${unit.color}`}></div>
              <span className="font-medium text-stone-700">UD {unit.id}</span>
              <span className="text-stone-400">({unit.sessions})</span>
              {!currentSubject.isLocked && !effectiveProgrammingLocked && (
                <div className="flex items-center ml-1 border-l border-stone-200 pl-1">
                  <button onClick={(e) => { e.stopPropagation(); adjustUnitSessions(unit.id, -1); }} className="p-0.5 text-stone-400 hover:text-stone-700 rounded"><ChevronLeft className="w-3 h-3" /></button>
                  <button onClick={(e) => { e.stopPropagation(); adjustUnitSessions(unit.id, 1); }} className="p-0.5 text-stone-400 hover:text-stone-700 rounded"><ChevronRight className="w-3 h-3" /></button>
                </div>
              )}
            </div>
          ))}
        </div>

        <div className="bg-white rounded-2xl shadow-sm border border-stone-200 overflow-hidden print:shadow-none print:border-none print:rounded-none">
          <div className="hidden print:block p-8 pb-4 border-b border-stone-200 mb-4">
            <h1 className="text-3xl font-bold tracking-tight text-stone-900 mb-2">{t('app.cronogramaEscolar')}</h1>
            <div className="flex gap-6 text-stone-600">
              <p><span className="font-bold">{t('app.materiaDousPuntos')}</span> {currentSubject.name}</p>
              <p><span className="font-bold">{t('app.cursoDousPuntos')}</span> {currentSubject.academicYear}</p>
            </div>
            
            <div className="mt-4 flex flex-wrap gap-x-4 gap-y-1 text-xs">
              {currentSubject.units.map(unit => (
                <div key={unit.id} className="flex items-center gap-1.5">
                  <div className={`w-2 h-2 rounded-full ${unit.color}`}></div>
                  <span className="font-medium text-stone-800">UD {unit.id}:</span>
                  <span className="text-stone-600">{unit.title} ({unit.sessions})</span>
                </div>
              ))}
            </div>
          </div>

          {/* Zoom do calendario: Ctrl+/Ctrl-/Ctrl+0 dende calquera sitio (ver useEffect
              enriba), ou estes botóns para quen prefira o rato. Á esquerda, a lenda de
              cores (fin de semana/festivo/sen clase/día actual) nun popover, para non
              ocupar unha tarxeta enteira sempre visible. */}
          <div className="flex items-center justify-between gap-1 px-3 pt-2 print:hidden">
            <div ref={legendRef} className="relative">
              <button onClick={() => setShowLegend(v => !v)} title={t('app.lenda')}
                className={`p-1.5 rounded-lg transition-colors ${showLegend ? 'text-stone-900 bg-stone-100' : 'text-stone-500 hover:text-stone-900 hover:bg-stone-100'}`}>
                <Info className="w-4 h-4" />
              </button>
              {showLegend && (
                <div className="absolute left-0 top-full mt-1 z-30 bg-white border border-stone-200 rounded-xl shadow-lg p-3 w-max">
                  <div className="text-xs font-medium text-stone-500 uppercase tracking-wider mb-2">{t('app.lenda')}</div>
                  <div className="flex flex-col gap-1.5">
                    <div className="flex items-center gap-1.5">
                      <div className="w-3 h-3 rounded-full bg-rose-100 border border-rose-200"></div>
                      <span className="text-xs text-stone-600">{t('app.finDeSemana')}</span>
                    </div>
                    <div className="flex items-center gap-1.5">
                      <div className="w-3 h-3 rounded-full bg-amber-100 border border-amber-200"></div>
                      <span className="text-xs text-stone-600">{t('app.festivo')}</span>
                    </div>
                    <div className="flex items-center gap-1.5">
                      <div className="w-3 h-3 rounded-full bg-stone-100 border border-stone-200"></div>
                      <span className="text-xs text-stone-600">{t('app.senClase')}</span>
                    </div>
                    <div className="flex items-center gap-1.5">
                      <div className="w-3 h-3 rounded-full bg-emerald-500"></div>
                      <span className="text-xs text-stone-600">{t('app.diaActual')}</span>
                    </div>
                    <div className="my-1 border-t border-stone-100"></div>
                    {INCIDENT_TYPES.map(ty => (
                      <div key={ty} className="flex items-center gap-1.5">
                        <span className={`w-3 h-3 leading-[12px] rounded-full text-[8px] font-bold text-center ${INCIDENT_COLORS[ty]}`}>
                          {INCIDENT_SYMBOLS[ty]}
                        </span>
                        <span className="text-xs text-stone-600">{t(INCIDENT_LABEL_KEYS[ty])}</span>
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
            <div className="flex items-center gap-1">
              <button onClick={zoomOut} disabled={calendarZoom <= CALENDAR_ZOOM_MIN} title={t('app.zoom.alonxarTitulo')}
                className="p-1.5 text-stone-500 hover:text-stone-900 hover:bg-stone-100 rounded-lg transition-colors disabled:opacity-30 disabled:hover:bg-transparent">
                <ZoomOut className="w-4 h-4" />
              </button>
              <button onClick={zoomReset} title={t('app.zoom.restaurarTitulo')}
                className="w-12 text-center text-xs font-medium text-stone-500 hover:text-stone-900 rounded-lg transition-colors">
                {Math.round(calendarZoom * 100)}%
              </button>
              <button onClick={zoomIn} disabled={calendarZoom >= CALENDAR_ZOOM_MAX} title={t('app.zoom.achegarTitulo')}
                className="p-1.5 text-stone-500 hover:text-stone-900 hover:bg-stone-100 rounded-lg transition-colors disabled:opacity-30 disabled:hover:bg-transparent">
                <ZoomIn className="w-4 h-4" />
              </button>
            </div>
          </div>

          <div className="overflow-x-auto" style={{ zoom: calendarZoom }}>
            <table className="w-full text-sm border-collapse">
              <thead>
                <tr>
                  <th className="w-24 p-3 text-left font-bold text-stone-500 uppercase tracking-wider bg-stone-50 border-b border-stone-200 sticky left-0 z-20">{t('app.mes')}</th>
                  {Array.from({ length: 31 }, (_, i) => i + 1).map(day => (
                    <th key={day} className="w-8 p-2 text-center font-bold text-stone-500 bg-stone-50 border-b border-stone-200 border-l border-stone-100">
                      {day}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-stone-100">
                {months.map((month, monthIdx) => (
                  <tr key={monthIdx} className="hover:bg-stone-50/50 transition-colors">
                    <td className="p-3 font-bold text-stone-700 uppercase tracking-wider bg-white border-r border-stone-100 sticky left-0 z-10">
                      {month.name}
                    </td>
                    {Array.from({ length: 31 }, (_, i) => i + 1).map(dayNum => {
                      const day = month.days.find((d: any) => d.day === dayNum);
                      
                      if (!day) {
                        return <td key={dayNum} className="p-0 border-l border-stone-100 bg-stone-50/30"></td>;
                      }

                      const isWeekend = day.isWeekend;
                      const isHoliday = day.isHoliday;
                      const isClassDay = day.isClassDay;
                      const unit = day.unit;
                      const log = currentSubject.logs[day.dateString];
                      const isToday = day.dateString === todayString;
                      const isException = day.isException;
                      // hasFiles: ficheiros de traballo subidos nesta sesión aínda que non se
                      // gardase texto (log). Conta igual có log á hora de amosar a raia negra.
                      const hasFiles = workFileDates.has(day.dateString);
                      // sessionTag substitúe a data ISO ("2026-10-02", que non aporta nada
                      // dun vistazo) no tooltip de cada día: "(3/12 Venres)" - sesión 3 de 12
                      // da unidade didáctica (só cando o día ten unidade e sesión asignadas,
                      // ver sessionIndex en data.ts) + día da semana, segundo o idioma da UI.
                      const dayName = weekdayName(day.dayOfWeek, i18n.language);
                      const sessionTag = unit && day.sessionIndex
                        ? `(${day.sessionIndex}/${unit.sessions} ${dayName})`
                        : `(${dayName})`;

                      let bgColor = 'bg-white';
                      let textColor = 'text-stone-700';
                      let content = '';
                      let title = sessionTag;
                      let borderClass = 'border-l border-stone-100';

                      if (isToday) {
                        borderClass = 'border-2 border-emerald-500 z-10 relative';
                      }

                      if (isWeekend) {
                        bgColor = 'bg-rose-50/50';
                        textColor = 'text-rose-300';
                      } else if (isHoliday) {
                        bgColor = 'bg-amber-50';
                        textColor = 'text-amber-600';
                        title = `${sessionTag} - ${day.holidayName}`;
                      } else if (!isClassDay) {
                        bgColor = 'bg-stone-50';
                        textColor = 'text-stone-300';
                      } else if (isException) {
                        bgColor = 'bg-amber-100';
                        textColor = 'text-amber-700';
                        content = log.status.charAt(0).toUpperCase();
                        title = `${sessionTag} - ${log.status} - ${log.content}`;
                      } else if (unit) {
                        bgColor = unit.bgLight;
                        textColor = unit.text;
                        content = unit.id.toString();
                        title = `${sessionTag} UD ${unit.id}: ${unit.title}`;

                        if (log) {
                          if (log.status === 'exame') {
                            bgColor = 'bg-purple-100';
                            textColor = 'text-purple-700';
                            content = 'E';
                            title = `${sessionTag} Exame - ${log.content}`;
                          } else {
                            // Lectivo with log
                            borderClass += ' border-b-2 border-b-stone-900';
                            title = `${sessionTag} UD ${unit.id} - ${log.content}`;
                          }
                        } else if (hasFiles) {
                          borderClass += ' border-b-2 border-b-stone-900';
                        }
                      } else if (log) {
                        if (log.status === 'exame') {
                          bgColor = 'bg-purple-100';
                          textColor = 'text-purple-700';
                          content = 'E';
                        } else {
                          borderClass += ' border-b-2 border-b-stone-900';
                        }
                        title = `${sessionTag} - ${log.status} - ${log.content}`;
                      } else if (hasFiles) {
                        borderClass += ' border-b-2 border-b-stone-900';
                      }

                      // Incidencias (positivo/negativo/expulsión/falta de orde): insignias de
                      // cor á parte da letra de status, para non confundir "F" de falta de
                      // orde coa "F" que xa amosa unha folga (ver isException arriba).
                      const incidentTypes: IncidentType[] = day.incidentTypes || [];
                      if (incidentTypes.length > 0) {
                        title += ` [${incidentTypes.map(t => INCIDENT_SYMBOLS[t]).join(' ')}]`;
                      }

                      const isDayClickable = !!datePicker || (!isWeekend && !isHoliday && isClassDay && !currentSubject.isLocked && canEditDays);
                      return (
                        <td
                          key={dayNum}
                          className={`p-0 text-center relative ${borderClass} ${bgColor} ${isDayClickable ? 'cursor-pointer hover:opacity-80' : ''} ${datePicker ? 'ring-1 ring-inset ring-stone-400' : ''}`}
                          title={datePicker ? t('reportBuilder.pickFromCalendarTitle', { date: title }) : title}
                          onClick={() => {
                            if (datePicker) { datePicker.onPick(day.dateString); setDatePicker(null); return; }
                            if (!isWeekend && !isHoliday && isClassDay && !currentSubject.isLocked && canEditDays) {
                              setSelectedDay(day);
                            }
                          }}
                        >
                          <div className={`flex items-center justify-center h-10 w-full font-medium ${textColor}`}>
                            {content}
                          </div>
                          {incidentTypes.length > 0 && (
                            <div className="absolute bottom-0.5 left-0 right-0 flex items-center justify-center gap-0.5">
                              {incidentTypes.map(ty => (
                                <span key={ty} title={t(INCIDENT_LABEL_KEYS[ty])}
                                  className={`w-3.5 h-3.5 leading-[14px] rounded-full text-[9px] font-bold ${INCIDENT_COLORS[ty]}`}>
                                  {INCIDENT_SYMBOLS[ty]}
                                </span>
                              ))}
                            </div>
                          )}
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </main>

      {selectedDay && !currentSubject.isLocked && (
        <DayModal
          day={selectedDay}
          existingLog={currentSubject.logs[selectedDay.dateString]}
          professorMode={isProfessor && !reviewingStudent}
          // O rexistro da sesión (estado + práctica) é de só lectura para o alumnado (só
          // escribe o profesor) e para o profesor mentres «accede como» un alumno.
          readOnlyLog={readOnlyLog}
          today={todayString}
          hostname={hostname}
          subjectId={currentSubject.id}
          // Ficheiros fonte: o alumnado xestiona os seus; o profesorado só os consulta
          // ao revisar un alumno. Na programación ("modelo") non hai ficheiros que amosar.
          filesMode={isProfessor ? (reviewingStudent ? 'readonly' : 'off') : (profile?.connected ? 'own' : 'off')}
          filesOwner={selectedStudent}
          // Comentario do profesor sobre a práctica: edítao ao revisar un alumno; en vista
          // «Acceder como» (e para o alumno) véo en lectura.
          commentMode={impersonating ? 'view' : (reviewingStudent ? 'edit' : (!isProfessor && profile?.connected ? 'view' : 'off'))}
          // Tarefa da sesión (tipo Moodle): o profesor xestiónaa (en modelo ou revisando);
          // o alumnado — e o profesor en vista «Acceder como» — só a descarga.
          taskMode={impersonating ? 'view' : (isProfessor ? 'manage' : (profile?.connected ? 'view' : 'off'))}
          onSave={handleSaveLog}
          onSaveComment={handleSaveStudentComment}
          onAddIncident={handleAddIncident}
          onRemoveIncident={handleRemoveIncident}
          onReprogram={handleReprogram}
          // Ao pechar refréscanse workFileDates: puido subirse ou borrarse un ficheiro
          // mentres a xanela estaba aberta, e o indicador do calendario debe reflectilo.
          onClose={() => { setSelectedDay(null); refreshWorkFileDates(); }}
          // Datos para o botón de informe asinado desta sesión (ver dentro de DayModal):
          // só o alumnado sobre a súa propia práctica (nunca en modelo, revisión ou
          // «Acceder como» — aí non hai un certificado propio co que asinar). Garda coma
          // "Gardar práctica" pero sen pechar a xanela (close:false), xa que o botón
          // quere quedar aí para amosar o progreso e actualizar os ficheiros adxuntos.
          subjectName={currentSubject.name}
          academicYear={currentSubject.academicYear}
          profile={profile}
          onSaveQuiet={!isProfessor ? (log: DailyLog) => handleSaveLog(log, { close: false }) : undefined}
        />
      )}

      {reprogramReq && (
        <div className="fixed inset-0 bg-black/50 z-[60] flex items-center justify-center p-4">
          <div className="bg-white rounded-2xl shadow-xl w-full max-w-md overflow-hidden">
            <div className="p-4 border-b border-stone-200 bg-stone-50 font-bold text-stone-900 flex items-center gap-2">
              <AlertCircle className="w-5 h-5 text-amber-500" /> {t('app.confirmarReprogramacion')}
            </div>
            <div className="p-5 text-sm text-stone-700 whitespace-pre-wrap">{reprogramReq.message}</div>
            <div className="p-4 border-t border-stone-200 bg-stone-50 flex justify-end gap-2">
              <button onClick={() => setReprogramReq(null)} className="px-4 py-2 text-stone-600 font-medium hover:bg-stone-200 rounded-lg transition-colors">{t('app.cancelar')}</button>
              <button onClick={() => reprogramReq.apply()} className="px-4 py-2 bg-stone-900 text-white font-medium hover:bg-stone-800 rounded-lg transition-colors">{t('app.aplicar')}</button>
            </div>
          </div>
        </div>
      )}

      {showMessages && profile && currentSubject && (
        <MessagesModal
          profile={profile}
          isProfessor={isProfessor}
          materiaId={currentSubject.id}
          materiaName={currentSubject.name}
          students={messageRecipients}
          initialSelected={selectedStudent}
          config={messagingConfig}
          onClose={() => setShowMessages(false)}
          onSeen={handleSeen}
        />
      )}

      {showBackup && isProfessor && (
        <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-2xl shadow-xl w-full max-w-2xl overflow-hidden flex flex-col max-h-[90vh]">
            <div className="p-4 border-b border-stone-200 flex justify-between items-center bg-stone-50">
              <h3 className="font-bold text-lg text-stone-900 flex items-center gap-2"><Database className="w-5 h-5" /> {t('app.copiasSeguridade')}</h3>
              <button onClick={() => setShowBackup(false)} className="p-2 text-stone-400 hover:text-stone-600 rounded-lg hover:bg-stone-200 transition-colors">
                <X className="w-5 h-5" />
              </button>
            </div>
            <div className="p-6 overflow-y-auto">
              <BackupRestore role="professor" courses={subjects.map(s => ({ id: s.id, name: `${s.name} (${s.academicYear})` }))} />
            </div>
          </div>
        </div>
      )}

      {showPrivateNotes && selectedStudent && (
        <PrivateNoteModal
          username={selectedStudent}
          materiaID={currentSubjectId}
          studentLabel={(() => {
            const r = roster.find(x => x.username === selectedStudent);
            return [r?.name, r?.surname].filter(Boolean).join(' ') || selectedStudent;
          })()}
          onClose={() => setShowPrivateNotes(false)}
        />
      )}

      {showSobre && <SobreTao onClose={() => setShowSobre(false)} />}

      {showModulos && isProfessor && <ModulosTao onClose={() => setShowModulos(false)} />}

      {showProgramacionPanel && (
        <ProgramacionPanel
          subject={currentSubject}
          proensUrl={currentSubject.config.proensUrl || ''}
          // Só o profesorado editando a súa propia materia (non revisando alumnado) pode
          // volver chamar a Proens dende aquí — o alumnado e o modo revisión len sempre a
          // caché xa publicada (subject.programacionInfo, chegada do servidor no login).
          podeActualizar={isProfessor && !reviewingStudent}
          onClose={() => { setShowProgramacionPanel(false); setFocusedProensUnit(null); }}
          onOpenSettings={() => { setShowProgramacionPanel(false); setShowSettings(true); }}
          onImported={(info) => onProgramacionInfoExtraida({ programacionInfo: info })}
          focusUnit={focusedProensUnit ?? undefined}
          onUnfocus={() => setFocusedProensUnit(null)}
        />
      )}

      {showSettings && !currentSubject.isLocked && (
        <SettingsModal
          config={currentSubject.config}
          units={currentSubject.units}
          subjects={subjects}
          profile={profile}
          canEditProgramming={!effectiveProgrammingLocked}
          showGroups={isProfessor && !reviewingStudent}
          groups={currentSubject.groups || []}
          onProfileUpdate={setProfile}
          onSave={(config, units, groups) => {
            const updates: Partial<Subject> = { config, units, ...(groups !== undefined ? { groups } : {}) };
            updateCurrentSubject(updates);
            setShowSettings(false);
            // Calendario, unidades e grupos publícanse decontado tamén dende aquí (non só
            // dende o log de sesións e a extracción de Proens/DOG): así o alumnado ve
            // calquera cambio da programación sen que o profesorado teña que premer
            // "Sincronizar co alumnado" á parte.
            if (isProfessor && !reviewingStudent) {
              sincronizarMateria({ ...currentSubject, ...updates });
            }
          }}
          onImport={(importedSubjects) => {
            setSubjects(importedSubjects);
            setCurrentSubjectId(importedSubjects[0].id);
            setShowSettings(false);
          }}
          onExtracted={(data) => onProgramacionInfoExtraida(data)}
          onClose={() => setShowSettings(false)}
        />
      )}

      {showReports && (
        <ReportsModal 
          subject={currentSubject}
          months={months}
          onClose={() => setShowReports(false)}
          onSaveReport={(unitId, report) => {
            if (currentSubject.isLocked) return;
            updateCurrentSubject({
              unitReports: {
                ...currentSubject.unitReports,
                [unitId]: report
              }
            });
          }}
          onSaveMonthlyReport={(monthKey, report) => {
            if (currentSubject.isLocked) return;
            updateCurrentSubject({
              monthlyReports: {
                ...(currentSubject.monthlyReports || {}),
                [monthKey]: report
              }
            });
          }}
          onOpenSignedReport={() => { setShowReports(false); setShowSignedReport(true); }}
        />
      )}

      {showSignedReport && (
        <ReportBuilder
          subject={currentSubject}
          profile={profile}
          title={isProfessor ? t('app.xerarInformeAsinado') : t('app.xerarInforme')}
          onClose={() => setShowSignedReport(false)}
          onRequestPickDate={(onPick) => setDatePicker({ onPick })}
          onCancelPickDate={() => setDatePicker(null)}
        />
      )}
    </div>
  );
}

// NoMateriasScreen is what a connected student sees when no materia is published for
// their group: there is no calendar to fill in, and offering an editable one would only
// invite work to be logged against a programación that is not their class's.
function NoMateriasScreen({ profile, erro, onLogout }: { profile: StudentProfile, erro?: string, onLogout: () => void }) {
  const { t } = useTranslation();
  return (
    <div className="min-h-screen bg-stone-100 font-sans text-stone-900 flex items-center justify-center p-6">
      <div className="bg-white border border-stone-200 rounded-2xl shadow-sm max-w-md w-full p-8 flex flex-col items-center gap-4 text-center">
        <div className="p-3 bg-stone-100 text-stone-500 rounded-2xl">
          <BookOpen className="w-8 h-8" />
        </div>
        <h1 className="text-xl font-bold text-stone-900">
          {erro ? t('app.noMaterias.tituloErro') : t('app.noMaterias.titulo')}
        </h1>
        {erro ? (
          <>
            <p className="text-sm text-stone-600">{t('app.noMaterias.descricionErro')}</p>
            <p className="text-xs text-rose-700 bg-rose-50 border border-rose-200 rounded-lg px-3 py-2 break-words w-full">{erro}</p>
          </>
        ) : (
          <>
            <p className="text-sm text-stone-600">
              {t('app.noMaterias.grupoPrefixo')} (<span className="font-medium text-stone-800">{profile.group?.trim() || t('app.noMaterias.senAsignar')}</span>)
              {' '}{t('app.noMaterias.grupoSufixo')}
            </p>
            <p className="text-xs text-stone-500">
              {t('app.noMaterias.falaProfesor')}
            </p>
          </>
        )}
        <button
          onClick={onLogout}
          className="mt-2 px-4 py-2 bg-stone-900 text-white font-medium hover:bg-stone-800 rounded-lg transition-colors flex items-center gap-2"
        >
          <LogOut className="w-4 h-4" />
          {t('app.sair')}
        </button>
      </div>
    </div>
  );
}

// FilesMode decides what the day modal does with the source files of the work:
// 'own' -> the student uploads/downloads/removes their own; 'readonly' -> the professor
// lists and downloads the reviewed student's; 'off' -> no file section at all.
type FilesMode = 'own' | 'readonly' | 'off';

const formatFileSize = (bytes: number): string => {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
};

// FILES_POLL_MS: intervalo do refresco silencioso das seccións de ficheiros (traballo,
// tarefa, comentario) mentres o día está aberto - así, se o alumno ou o profesor
// (segundo corresponda) publica un ficheiro novo, a outra parte véxao sen ter que
// pechar e reabrir o día. Non hai canle en tempo real co servidor de datos (File
// Browser é un produto de terceiros, sen webhooks/SSE) nin entre os propios Tao's
// (cada un é illado, só fala con File Browser) - así que isto é polling, non un
// watchdog+evento; 8s dá unha sensación case instantánea sen saturar o servidor.
const FILES_POLL_MS = 8000;

// WorkFilesSection lists the source files attached to one session and, for the student
// who owns them, lets them add or remove files. The picker and the save dialog are
// native (opened from Go), so nothing needs to travel through the webview.
function WorkFilesSection({ subjectId, date, mode, owner, refreshSignal }: { subjectId: string, date: string, mode: Exclude<FilesMode, 'off'>, owner: string, refreshSignal?: number }) {
  const { t } = useTranslation();
  const [files, setFiles] = useState<WorkFile[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isUploading, setIsUploading] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [downloaded, setDownloaded] = useState(''); // path just saved, so it can be opened
  // Marca "estase a subir": o refresco automático de máis abaixo párase mentres dura.
  // Unha subida grande pode levar minutos (ver tempoTransferencia en Go) e cada
  // listado que se lle cruzase competía polas poucas conexións que o servidor de
  // ficheiros mantén abertas, alongando aínda máis a propia subida.
  const uploadingRef = useRef(false);
  // Name of the file awaiting a delete confirmation (the webview has no confirm()).
  const [pendingDelete, setPendingDelete] = useState('');

  const readOnly = mode === 'readonly';

  // silent=true (background poll, ver FILES_POLL_MS máis abaixo): non toca
  // isLoading/error, para non facer parpadear a lista nin amosar un erro puntual de
  // rede - simplemente mantén a última lista coñecida se a chamada falla.
  const load = async (silent = false) => {
    if (!silent) { setIsLoading(true); setError(''); }
    try {
      const list = readOnly
        ? await ListStudentWorkFiles(owner, subjectId, date)
        : await ListWorkFiles(subjectId, date);
      setFiles(list || []);
    } catch (e: any) {
      if (!silent) setError(t('app.files.erroListar', { error: e?.message || e }));
    } finally {
      if (!silent) setIsLoading(false);
    }
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subjectId, date, mode, owner, refreshSignal]);

  useEffect(() => {
    const id = setInterval(() => { if (!uploadingRef.current) load(true); }, FILES_POLL_MS);
    return () => clearInterval(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subjectId, date, mode, owner]);

  const handleUpload = async () => {
    uploadingRef.current = true;
    setIsUploading(true);
    setError('');
    setMessage('');
    try {
      const uploaded = await UploadWorkFiles(subjectId, date);
      if (uploaded && uploaded.length > 0) {
        setMessage(t('app.files.subidosServidor', { count: uploaded.length }));
        await load();
      }
    } catch (e: any) {
      setError(t('app.files.erroSubir', { error: e?.message || e }));
      await load(); // algún ficheiro puido subir antes do erro
    } finally {
      uploadingRef.current = false;
      setIsUploading(false);
    }
  };

  const handleDownload = async (name: string) => {
    setError('');
    setMessage('');
    setDownloaded('');
    try {
      const dest = readOnly
        ? await DownloadStudentWorkFile(owner, subjectId, date, name)
        : await DownloadWorkFile(subjectId, date, name);
      if (dest) { setMessage(t('app.files.gardadoEn', { dest })); setDownloaded(dest); }
    } catch (e: any) {
      setError(t('app.files.erroDescargar', { error: e?.message || e }));
    }
  };

  const handleDelete = async (name: string) => {
    setPendingDelete('');
    setError('');
    setMessage('');
    try {
      await DeleteWorkFile(subjectId, date, name);
      setMessage(t('app.files.eliminadoNome', { name }));
      await load();
    } catch (e: any) {
      setError(t('app.files.erroEliminar', { error: e?.message || e }));
    }
  };

  return (
    <div className="flex flex-col gap-1.5 shrink-0">
      <div className="flex items-center justify-between">
        <label className="text-sm font-bold text-stone-700 flex items-center gap-1.5">
          <Paperclip className="w-4 h-4" />
          {t('app.files.ficheirosFonteTraballo')}
          {readOnly && <span className="font-normal text-stone-500">{t('app.files.de')} {owner}</span>}
        </label>
        {!readOnly && (
          <button
            onClick={handleUpload}
            disabled={isUploading}
            className="px-3 py-1.5 text-sm bg-stone-100 border border-stone-300 text-stone-700 font-medium hover:bg-stone-200 rounded-lg transition-colors flex items-center gap-2 disabled:opacity-50"
          >
            {isUploading ? <Loader2 className="w-4 h-4 animate-spin" /> : <Upload className="w-4 h-4" />}
            {isUploading ? t('app.files.subindo') : t('app.files.engadirFicheiros')}
          </button>
        )}
      </div>

      <div className="border border-stone-200 rounded-lg divide-y divide-stone-100 max-h-44 overflow-y-auto">
        {isLoading ? (
          <p className="p-3 text-sm text-stone-500 flex items-center gap-2">
            <Loader2 className="w-4 h-4 animate-spin" /> {t('app.files.cargandoFicheiros')}
          </p>
        ) : files.length === 0 ? (
          <p className="p-3 text-sm text-stone-500">
            {readOnly
              ? t('app.files.alumnoNonSubiu')
              : t('app.files.aindaNonSubiches')}
          </p>
        ) : (
          files.map(f => (
            <div key={f.name} className="p-2 flex items-center gap-2">
              <FileText className="w-4 h-4 text-stone-400 shrink-0" />
              <span className="flex-1 min-w-0 truncate text-sm text-stone-800" title={f.name}>{f.name}</span>
              <span className="text-xs text-stone-400 shrink-0">{formatFileSize(f.size)}</span>
              {pendingDelete === f.name ? (
                <span className="flex items-center gap-1 shrink-0">
                  <span className="text-xs text-stone-600">{t('app.files.eliminarPregunta')}</span>
                  <button onClick={() => handleDelete(f.name)} className="px-2 py-1 text-xs bg-red-600 text-white font-medium rounded hover:bg-red-700 transition-colors">{t('app.files.si')}</button>
                  <button onClick={() => setPendingDelete('')} className="px-2 py-1 text-xs text-stone-600 font-medium rounded hover:bg-stone-100 transition-colors">{t('app.files.non')}</button>
                </span>
              ) : (
                <span className="flex items-center gap-1 shrink-0">
                  <button onClick={() => handleDownload(f.name)} title={t('app.files.descargar')} className="p-1.5 text-stone-400 hover:text-stone-700 rounded hover:bg-stone-100 transition-colors">
                    <Download className="w-4 h-4" />
                  </button>
                  {!readOnly && (
                    <button onClick={() => setPendingDelete(f.name)} title={t('app.files.eliminar')} className="p-1.5 text-stone-400 hover:text-red-600 rounded hover:bg-red-50 transition-colors">
                      <Trash2 className="w-4 h-4" />
                    </button>
                  )}
                </span>
              )}
            </div>
          ))
        )}
      </div>

      {message && (
        <div className="flex items-center gap-2 text-xs text-emerald-700">
          <span className="break-all min-w-0">{message}</span>
          {downloaded && (
            <button onClick={() => OpenFile(downloaded).catch((e: any) => setError(t('app.files.erroAbrir', { error: e?.message || e })))}
              className="flex items-center gap-1 shrink-0 px-2 py-1 rounded-lg bg-emerald-600 text-white font-medium hover:bg-emerald-700 transition-colors">
              <FileUp className="w-3.5 h-3.5" /> {t('app.files.abrirFicheiro')}
            </button>
          )}
        </div>
      )}
      {error && <p className="text-xs text-red-600 flex items-start gap-1"><AlertCircle className="w-3 h-3 mt-0.5 shrink-0" />{error}</p>}
      {!readOnly && !error && (
        <p className="text-xs text-stone-400">
          {t('app.files.gardanseServidor')}
        </p>
      )}
    </div>
  );
}

type CommentMode = 'edit' | 'view' | 'off';
type TaskMode = 'manage' | 'view' | 'off';

// TaskFilesSection is the simple Moodle-like assignment for a session: the professor
// ('manage') attaches one or more files that become the task for the whole materia, and
// every student ('view') can download them. The files are shared (one copy per session),
// not per-student.
// PracticaCorrectionSection lets the professor build (or refresh) a de-identified copy
// of this session's student work — one subfolder per student named only by their
// internal uuid, never their name or certificate — in dixitalizacion/practicas/, so it
// can be handed to an AI tool for correction without exposing personal data. See
// PrepareCorrectionFolder in practicas.go.
function PracticaCorrectionSection({ subjectId, date }: { subjectId: string, date: string }) {
  const { t } = useTranslation();
  const [isPreparing, setIsPreparing] = useState(false);
  const [result, setResult] = useState<PracticaCorrectionSummary | null>(null);
  const [error, setError] = useState('');

  const handlePrepare = async () => {
    setIsPreparing(true); setError(''); setResult(null);
    try {
      setResult(await PrepareCorrectionFolder(subjectId, date));
    } catch (e: any) {
      setError(t('app.dayModal.prepararCorreccionErro', { error: e?.message || e }));
    } finally {
      setIsPreparing(false);
    }
  };

  const handleOpenFolder = async () => {
    setError('');
    try {
      await AbrirCartafolCorreccion(subjectId, date);
    } catch (e: any) {
      setError(t('app.dayModal.abrirCartafolCorreccionErro', { error: e?.message || e }));
    }
  };

  return (
    <div className="flex flex-col gap-1 rounded-lg border border-emerald-200 bg-white p-2">
      <div className="flex items-center justify-between gap-3">
        {/* A descrición longa xa non vai en liña propia (compactado): queda como
            tooltip do título, ver app.dayModal.prepararCorreccionDesc. */}
        <label className="text-sm font-bold text-emerald-800 flex items-center gap-1.5" title={t('app.dayModal.prepararCorreccionDesc')}>
          <ShieldCheck className="w-4 h-4" />
          {t('app.dayModal.prepararCorreccionTitulo')}
        </label>
        <button onClick={handlePrepare} disabled={isPreparing}
          className="px-2.5 py-1 text-xs bg-emerald-100 border border-emerald-300 text-emerald-800 font-medium hover:bg-emerald-200 rounded-lg transition-colors flex items-center gap-1.5 disabled:opacity-50 shrink-0">
          {isPreparing ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <ShieldCheck className="w-3.5 h-3.5" />}
          {isPreparing ? t('app.dayModal.prepararCorreccionPreparando') : t('app.dayModal.prepararCorreccionBoton')}
        </button>
      </div>

      {result && (
        result.files > 0 ? (
          <div className="flex items-center justify-between gap-3">
            <p className="text-xs text-emerald-700">
              {t('app.dayModal.prepararCorreccionExito', { files: result.files, students: result.students })}
            </p>
            <button onClick={handleOpenFolder}
              className="flex items-center gap-1.5 px-2 py-0.5 text-xs bg-emerald-50 border border-emerald-300 text-emerald-800 font-medium hover:bg-emerald-100 rounded-lg transition-colors shrink-0">
              <FolderOpen className="w-3.5 h-3.5" /> {t('app.dayModal.abrirCartafolCorreccion')}
            </button>
          </div>
        ) : (
          <p className="text-xs text-stone-500">{t('app.dayModal.prepararCorreccionSenTraballos')}</p>
        )
      )}
      {result && result.skipped && result.skipped.length > 0 && (
        <p className="text-xs text-amber-700">{t('app.dayModal.prepararCorreccionOmitidos', { list: result.skipped.join(', ') })}</p>
      )}
      {error && <p className="text-xs text-red-600">{error}</p>}
    </div>
  );
}

// TaskFilesSectionHandle expón flushPendingDescription para que DayModal poida
// gardar unha explicación de tarefa escrita e non gardada explicitamente cando se
// preme "Gardar sesión" — ver o ref en DayModal e o comentario sobre
// descriptionDirtyRef máis abaixo.
export type TaskFilesSectionHandle = { flushPendingDescription: () => Promise<void> };

const TaskFilesSection = forwardRef<TaskFilesSectionHandle, { subjectId: string, date: string, mode: Exclude<TaskMode, 'off'>, fill?: boolean }>(function TaskFilesSection({ subjectId, date, mode, fill }, ref) {
  const { t } = useTranslation();
  const [files, setFiles] = useState<WorkFile[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isUploading, setIsUploading] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [downloaded, setDownloaded] = useState('');
  const [pendingDelete, setPendingDelete] = useState('');

  // Explicación escrita da tarefa (pestana "Tarefa" do profesorado, ver DayModal) -
  // gárdase á parte dos ficheiros (SaveTaskDescription/GetTaskDescription, Go) para
  // que nunca se mesture coa listaxe de ListTaskFiles. O profesorado ten o seu propio
  // botón "Gardar explicación" para gardala ao instante; ademais, se queda escrita e
  // sen gardar, "Gardar sesión" (o botón principal do pé de DayModal) tamén a garda
  // decontado — ver descriptionDirtyRef e flushPendingDescription máis abaixo, antes
  // isto perdíase se se premía "Gardar sesión" sen antes premer "Gardar explicación".
  // O alumnado só a le.
  const [description, setDescription] = useState('');
  const [descLoaded, setDescLoaded] = useState(false);
  const [descSaving, setDescSaving] = useState(false);
  const [descSaved, setDescSaved] = useState(false);
  const [descError, setDescError] = useState('');
  // descriptionDirtyRef: true dende que o profesorado escribe algo (onChange do
  // editor, non a carga inicial nin un push de Yang) até que se garda — por
  // handleSaveDescription ou por flushPendingDescription.
  const descriptionDirtyRef = useRef(false);

  // Excepcións individuais (docs/api-tao.md §11): alumnado con tarefa distinta
  // desta xeral, enviada por Yang (yang/tarefa_tao.go, EnviarReparto) ou editada
  // á man aquí mesmo (SaveTaskDescriptionAlumno). O aviso é pasivo — editar o
  // texto xeral de abaixo NON as toca — pero visible: un botón chamativo a
  // carón de "Editar con Yang" abre showOverridesModal coa lista completa.
  //
  // Cada fila leva un par "Xeral"/"Individual" (revertPendentes marca só a
  // intención, sen executar nada) — "Marcar todos → Xeral" selecciona sen
  // borrar, e só "Aplicar cambios" chama DeleteTaskOverride; así ningún clic
  // solto (nin o de "marcar todos") fai desaparecer a ninguén sen confirmar.
  // Premer o nome dun alumno en modo Individual abre un editor Markdown
  // (GetTaskDescriptionAlumno/SaveTaskDescriptionAlumno) para cambiar
  // directamente a súa variante, sen pasar por Yang.
  const [overrides, setOverrides] = useState<string[]>([]);
  const [showOverridesModal, setShowOverridesModal] = useState(false);
  const [revertPendentes, setRevertPendentes] = useState<Record<string, boolean>>({});
  const [overrideBusy, setOverrideBusy] = useState('');
  const [overrideError, setOverrideError] = useState('');
  const [editingUsername, setEditingUsername] = useState('');
  const [editText, setEditText] = useState('');
  const [editLoading, setEditLoading] = useState(false);
  const [editSaving, setEditSaving] = useState(false);
  const [editError, setEditError] = useState('');
  const [editYangLaunching, setEditYangLaunching] = useState(false);
  const [editYangError, setEditYangError] = useState('');

  // "Editar con Yang" (ver tao/yang_launch.go, yang/tarefa_tao.go): abre Yang
  // xa apuntado a esta materia/día; en canto o profesorado xera o Markdown
  // alí, chega aquí por evento en vivo (tao:tarefa-actualizada), sen recargar
  // nin gardar á man.
  const [yangLaunching, setYangLaunching] = useState(false);
  const [yangError, setYangError] = useState('');
  const [yangUpdated, setYangUpdated] = useState(false);
  const [yangDisponible, setYangDisponible] = useState(false);
  const [yangComprobado, setYangComprobado] = useState(false);
  const [yangInstalando, setYangInstalando] = useState(false);

  const manage = mode === 'manage';

  // Comproba se hai Yang de verdade neste equipo (coma módulo de Piztu, ou
  // instalado á parte — ver tao/yang_launch.go, YangDispoñible). Se non o
  // hai, "Editar con Yang" cede o seu sitio a "Instalar Yang"
  // (handleInstalarYang) en vez de agocharse sen máis.
  const comprobarYang = () => {
    YangDispoñible().then(setYangDisponible).catch(() => setYangDisponible(false)).finally(() => setYangComprobado(true));
  };
  useEffect(() => {
    if (!manage) return;
    comprobarYang();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [manage]);

  // silent=true: ver comentario equivalente en WorkFilesSection/FILES_POLL_MS.
  const load = async (silent = false) => {
    if (!silent) { setIsLoading(true); setError(''); }
    try {
      setFiles((await ListTaskFiles(subjectId, date)) || []);
    } catch (e: any) {
      if (!silent) setError(t('app.files.erroCargarTarefa', { error: e?.message || e }));
    } finally {
      if (!silent) setIsLoading(false);
    }
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subjectId, date, mode]);

  useEffect(() => {
    const id = setInterval(() => load(true), FILES_POLL_MS);
    return () => clearInterval(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subjectId, date, mode]);

  useEffect(() => {
    setDescLoaded(false);
    GetTaskDescription(subjectId, date)
      .then((d) => setDescription(d || ''))
      .catch(() => setDescription(''))
      .finally(() => setDescLoaded(true));
  }, [subjectId, date, mode]);

  // Só o profesorado pode ver ListTaskOverrides (profesor only, ver traballos.go) —
  // en modo 'view' (alumnado) nin se intenta, non hai aviso que amosar aí.
  const loadOverrides = () => {
    if (!manage) return;
    ListTaskOverrides(subjectId, date)
      .then((list) => setOverrides(list || []))
      .catch(() => setOverrides([]));
  };
  useEffect(() => {
    loadOverrides();
    setShowOverridesModal(false);
    setRevertPendentes({});
    setEditingUsername('');
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subjectId, date, manage]);

  // Se a última excepción se borra (individual ou "revertir todas"), non
  // deixa o modal aberto e baleiro.
  useEffect(() => {
    if (overrides.length === 0) setShowOverridesModal(false);
  }, [overrides]);

  const pendentesCount = Object.values(revertPendentes).filter(Boolean).length;

  const toggleRevert = (username: string, revert: boolean) => {
    setRevertPendentes((prev) => ({ ...prev, [username]: revert }));
  };

  // marcarTodos só toca revertPendentes (selección), nunca chama a ningunha
  // API - ver handleAplicarCambios para a acción de verdade.
  const marcarTodos = (revert: boolean) => {
    const next: Record<string, boolean> = {};
    if (revert) overrides.forEach((u) => { next[u] = true; });
    setRevertPendentes(next);
  };

  const handleAplicarCambios = async () => {
    const usernames = overrides.filter((u) => revertPendentes[u]);
    if (usernames.length === 0) return;
    setOverrideBusy('*'); setOverrideError('');
    try {
      if (usernames.length === overrides.length) {
        await DeleteAllTaskOverrides(subjectId, date);
      } else {
        for (const username of usernames) {
          await DeleteTaskOverride(subjectId, date, username);
        }
      }
      setRevertPendentes({});
      loadOverrides();
    } catch (e: any) {
      setOverrideError(typeof e === 'string' ? e : (e?.message || String(e)));
    } finally {
      setOverrideBusy('');
    }
  };

  // Editor individual: abre con GetTaskDescriptionAlumno (contido actual dese
  // alumno) e garda con SaveTaskDescriptionAlumno - texto baleiro reverte á
  // tarefa xeral, coma DeleteTaskOverride (mesma regra ca SaveTaskDescription).
  const handleOpenEditor = async (username: string) => {
    setEditingUsername(username); setEditText(''); setEditError(''); setEditYangError(''); setEditLoading(true);
    try {
      setEditText((await GetTaskDescriptionAlumno(subjectId, date, username)) || '');
    } catch (e: any) {
      setEditError(typeof e === 'string' ? e : (e?.message || String(e)));
    } finally {
      setEditLoading(false);
    }
  };

  const handleSaveEditor = async () => {
    setEditSaving(true); setEditError('');
    try {
      await SaveTaskDescriptionAlumno(subjectId, date, editingUsername, editText);
      setEditingUsername('');
      loadOverrides();
    } catch (e: any) {
      setEditError(typeof e === 'string' ? e : (e?.message || String(e)));
    } finally {
      setEditSaving(false);
    }
  };

  const handleSaveDescription = async () => {
    setDescSaving(true); setDescError(''); setDescSaved(false);
    try {
      await SaveTaskDescription(subjectId, date, description);
      descriptionDirtyRef.current = false;
      setDescSaved(true);
    } catch (e: any) {
      setDescError(typeof e === 'string' ? e : (e?.message || String(e)));
    } finally {
      setDescSaving(false);
    }
  };

  // flushPendingDescription: chámao DayModal (por ref) xusto antes de "Gardar
  // sesión", para que unha explicación escrita e non gardada explicitamente non se
  // perda. Silencioso a propósito (best-effort): se falla, a explicación queda tal
  // e como estaba escrita no editor (nada se perde na propia xanela) e o botón
  // "Gardar explicación" segue aí para reintentar á man.
  useImperativeHandle(ref, () => ({
    flushPendingDescription: async () => {
      if (!manage || !descriptionDirtyRef.current) return;
      try {
        await SaveTaskDescription(subjectId, date, description);
        descriptionDirtyRef.current = false;
        setDescSaved(true);
      } catch { /* "Gardar explicación" segue dispoñible para reintentar */ }
    },
  }), [manage, subjectId, date, description]);

  // Escoita o push de Yang (tao/api_handlers.go handleSaveTarefa) e actualiza
  // esta pestana ao instante, só se é a que Yang tiña aberta (mesma
  // materia+día) — sen tocar o botón "Gardar explicación", que segue
  // funcionando igual para edicións manuais.
  useEffect(() => {
    if (!manage) return;
    const off = Events.On('tao:tarefa-actualizada', (e) => {
      const payload: { materiaId: string, date: string, markdown: string } = e.data;
      if (payload && payload.materiaId === subjectId && payload.date === date) {
        setDescription(payload.markdown || '');
        setYangUpdated(true);
      }
    });
    // Push individualizado (EnviarReparto, ou "Editar con Yang" dun alumno
    // concreto): non toca o texto xeral de arriba, só refresca a listaxe do
    // aviso (ListTaskOverrides) en vez de reconsultala por polling — e, se é
    // xustamente o alumno que se está a editar no modal, actualiza editText
    // ao instante (mesmo criterio ca setDescription arriba para a xeral).
    const offAlumno = Events.On('tao:tarefa-alumno-actualizada', (e) => {
      const payload: { materiaId: string, date: string, username: string, markdown: string } = e.data;
      if (payload && payload.materiaId === subjectId && payload.date === date) {
        loadOverrides();
        if (payload.username === editingUsername) {
          setEditText(payload.markdown || '');
        }
      }
    });
    return () => { off(); offAlumno(); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subjectId, date, manage, editingUsername]);

  const handleOpenYang = async () => {
    setYangError(''); setYangUpdated(false); setYangLaunching(true);
    try {
      await AbrirYangParaTarefa(subjectId, date);
    } catch (e: any) {
      setYangError(typeof e === 'string' ? e : (e?.message || String(e)));
    } finally {
      setYangLaunching(false);
    }
  };

  // "Editar con Yang" dentro do editor dun alumno concreto (modal de
  // excepcións): mesma tubaxe ca handleOpenYang, pero Yang pecha o círculo
  // enviando SÓ a ese alumno (tao/yang_launch.go AbrirYangParaTarefaAlumno,
  // yang/tarefa_tao.go EnviarATarefaTaoAlumno) — chega aquí en vivo polo
  // mesmo evento tao:tarefa-alumno-actualizada que xa actualiza editText.
  const handleOpenYangAlumno = async (username: string) => {
    setEditYangError(''); setEditYangLaunching(true);
    try {
      await AbrirYangParaTarefaAlumno(subjectId, date, username);
    } catch (e: any) {
      setEditYangError(typeof e === 'string' ? e : (e?.message || String(e)));
    } finally {
      setEditYangLaunching(false);
    }
  };

  // Descarga e desprega Yang neste equipo (tao/yang_instalar.go, mesmo
  // mecanismo de "<id>_<versión>.zip" en github.com/piztusistemas/modulos que xa
  // usa Piztu para os seus propios módulos, sen sudo). Ao rematar volve
  // comprobar YangDispoñible() para que o botón cambie a "Editar con Yang"
  // sen ter que pechar e reabrir a pestana.
  const handleInstalarYang = async () => {
    setYangError(''); setYangInstalando(true);
    try {
      await InstalarYang();
      comprobarYang();
    } catch (e: any) {
      setYangError(typeof e === 'string' ? e : (e?.message || String(e)));
    } finally {
      setYangInstalando(false);
    }
  };

  const handleUpload = async () => {
    setIsUploading(true); setError(''); setMessage('');
    try {
      const uploaded = await UploadTaskFiles(subjectId, date);
      if (uploaded && uploaded.length > 0) {
        setMessage(t('app.files.subidosTarefa', { count: uploaded.length }));
        await load();
      }
    } catch (e: any) {
      setError(t('app.files.erroSubir', { error: e?.message || e }));
      await load();
    } finally {
      setIsUploading(false);
    }
  };

  const handleDownload = async (name: string) => {
    setError(''); setMessage(''); setDownloaded('');
    try {
      const dest = await DownloadTaskFile(subjectId, date, name);
      if (dest) { setMessage(t('app.files.gardadoEn', { dest })); setDownloaded(dest); }
    } catch (e: any) {
      setError(t('app.files.erroDescargar', { error: e?.message || e }));
    }
  };

  const handleDelete = async (name: string) => {
    setPendingDelete(''); setError(''); setMessage('');
    try {
      await DeleteTaskFile(subjectId, date, name);
      setMessage(t('app.files.eliminadoNome', { name }));
      await load();
    } catch (e: any) {
      setError(t('app.files.erroEliminar', { error: e?.message || e }));
    }
  };

  return (
    <div className={`flex flex-col gap-1.5 rounded-lg border border-amber-200 bg-amber-50/60 p-3 ${fill ? 'flex-1 min-h-0' : 'shrink-0'}`}>
      <div className="flex items-center justify-between shrink-0">
        <label className="text-sm font-bold text-amber-800 flex items-center gap-1.5">
          <FileUp className="w-4 h-4" />
          {t('app.files.tarefaSesion')} {manage ? t('app.files.paraOAlumnado') : ''}
        </label>
        {manage && (
          <button onClick={handleUpload} disabled={isUploading}
            className="px-3 py-1.5 text-sm bg-amber-100 border border-amber-300 text-amber-800 font-medium hover:bg-amber-200 rounded-lg transition-colors flex items-center gap-2 disabled:opacity-50">
            {isUploading ? <Loader2 className="w-4 h-4 animate-spin" /> : <Upload className="w-4 h-4" />}
            {isUploading ? t('app.files.subindo') : t('app.files.subirTarefa')}
          </button>
        )}
      </div>

      {/* Explicación escrita da tarefa: cadro propio, á parte da listaxe de
          ficheiros. O profesorado edítaa e gárdaa co seu propio botón; o alumnado só a
          ve (nada que amosar se aínda non hai ningunha escrita). */}
      {manage ? (
        <div className="flex flex-col gap-1.5 rounded-lg border border-amber-200 bg-white p-2.5">
          <MarkdownEditor value={description} onChange={(v) => { setDescription(v); descriptionDirtyRef.current = true; }} />
          <div className="flex items-center gap-2">
            <button onClick={handleSaveDescription} disabled={descSaving || !descLoaded}
              className="px-3 py-1.5 text-sm bg-amber-100 border border-amber-300 text-amber-800 font-medium hover:bg-amber-200 rounded-lg transition-colors flex items-center gap-2 disabled:opacity-50">
              {descSaving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
              {descSaving ? t('app.files.gardando') : t('app.files.gardarExplicacionTarefa')}
            </button>
            {yangComprobado && (yangDisponible ? (
              <button onClick={handleOpenYang} disabled={yangLaunching}
                className="px-3 py-1.5 text-sm bg-indigo-100 border border-indigo-300 text-indigo-800 font-medium hover:bg-indigo-200 rounded-lg transition-colors flex items-center gap-2 disabled:opacity-50">
                {yangLaunching ? <Loader2 className="w-4 h-4 animate-spin" /> : <Sparkles className="w-4 h-4" />}
                {t('app.files.editarConYang', 'Editar con Yang')}
              </button>
            ) : (
              <button onClick={handleInstalarYang} disabled={yangInstalando}
                title={t('app.files.instalarYangDesc', 'Descarga Yang (xerador de material didáctico) e instálao neste equipo')}
                className="px-3 py-1.5 text-sm bg-indigo-50 border border-indigo-200 text-indigo-700 font-medium hover:bg-indigo-100 rounded-lg transition-colors flex items-center gap-2 disabled:opacity-50">
                {yangInstalando ? <Loader2 className="w-4 h-4 animate-spin" /> : <Download className="w-4 h-4" />}
                {yangInstalando ? t('app.files.instalandoYang', 'Instalando Yang…') : t('app.files.instalarYang', 'Yang non está instalado — Instalar')}
              </button>
            ))}
            {overrides.length > 0 && (
              <button type="button" onClick={() => { setRevertPendentes({}); setEditingUsername(''); setShowOverridesModal(true); }}
                title={t('app.files.excepcionsTarefaTooltip', 'Hai alumnado con tarefa individual distinta desta - xestionar')}
                className="px-3 py-1.5 text-sm bg-orange-500 text-white font-bold hover:bg-orange-600 rounded-lg transition-colors flex items-center gap-2 shadow-sm">
                <Users className="w-4 h-4" />
                {t('app.files.excepcionsTarefa', { count: overrides.length })}
              </button>
            )}
            {descSaved && <span className="text-xs text-emerald-700">{t('app.files.explicacionGardada')}</span>}
            {yangUpdated && <span className="text-xs text-emerald-700">{t('app.files.actualizadoDendeYang', 'Actualizado dende Yang')}</span>}
            {(descError || yangError) && <span className="text-xs text-red-600">{descError || yangError}</span>}
          </div>
        </div>
      ) : (
        descLoaded && description.trim() && (
          <div className={`rounded-lg border border-amber-200 bg-white p-3 text-sm text-stone-800 overflow-y-auto ${fill ? 'flex-1 min-h-[240px]' : ''}`}>
            <MarkdownPreview content={description} />
          </div>
        )
      )}

      <div className="border border-amber-200 rounded-lg divide-y divide-amber-100 bg-white max-h-44 shrink-0 overflow-y-auto">
        {isLoading ? (
          <p className="p-3 text-sm text-stone-500 flex items-center gap-2"><Loader2 className="w-4 h-4 animate-spin" /> {t('app.files.cargando')}</p>
        ) : files.length === 0 ? (
          <p className="p-3 text-sm text-stone-500">
            {manage ? t('app.files.aindaNonSubichesTarefa') : t('app.files.profesoradoNonSubiu')}
          </p>
        ) : (
          files.map(f => (
            <div key={f.name} className="p-2 flex items-center gap-2">
              <FileText className="w-4 h-4 text-amber-500 shrink-0" />
              <span className="flex-1 min-w-0 truncate text-sm text-stone-800" title={f.name}>{f.name}</span>
              <span className="text-xs text-stone-400 shrink-0">{formatFileSize(f.size)}</span>
              {pendingDelete === f.name ? (
                <span className="flex items-center gap-1 shrink-0">
                  <span className="text-xs text-stone-600">{t('app.files.eliminarPregunta')}</span>
                  <button onClick={() => handleDelete(f.name)} className="px-2 py-1 text-xs bg-red-600 text-white font-medium rounded hover:bg-red-700 transition-colors">{t('app.files.si')}</button>
                  <button onClick={() => setPendingDelete('')} className="px-2 py-1 text-xs text-stone-600 font-medium rounded hover:bg-stone-100 transition-colors">{t('app.files.non')}</button>
                </span>
              ) : (
                <span className="flex items-center gap-1 shrink-0">
                  <button onClick={() => handleDownload(f.name)} title={t('app.files.descargar')} className="p-1.5 text-stone-400 hover:text-stone-700 rounded hover:bg-stone-100 transition-colors">
                    <Download className="w-4 h-4" />
                  </button>
                  {manage && (
                    <button onClick={() => setPendingDelete(f.name)} title={t('app.files.eliminar')} className="p-1.5 text-stone-400 hover:text-red-600 rounded hover:bg-red-50 transition-colors">
                      <Trash2 className="w-4 h-4" />
                    </button>
                  )}
                </span>
              )}
            </div>
          ))
        )}
      </div>

      {message && (
        <div className="flex items-center gap-2 text-xs text-emerald-700">
          <span className="break-all min-w-0">{message}</span>
          {downloaded && (
            <button onClick={() => OpenFile(downloaded).catch((e: any) => setError(t('app.files.erroAbrir', { error: e?.message || e })))}
              className="flex items-center gap-1 shrink-0 px-2 py-1 rounded-lg bg-emerald-600 text-white font-medium hover:bg-emerald-700 transition-colors">
              <FileUp className="w-3.5 h-3.5" /> {t('app.files.abrirFicheiro')}
            </button>
          )}
        </div>
      )}
      {error && <p className="text-xs text-red-600 flex items-start gap-1"><AlertCircle className="w-3 h-3 mt-0.5 shrink-0" />{error}</p>}

      {/* Xestión das excepcións individuais: mesmo patrón de modal ca outros
          diálogos desta pantalla (fixed inset-0 + caixa centrada), tamaño e
          posición equivalentes ao panel de configuración de Xesta. */}
      {showOverridesModal && (
        <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
          <div className="bg-white rounded-2xl shadow-xl w-full max-w-3xl overflow-hidden flex flex-col h-[85vh] max-h-[85vh]">
            <div className="p-4 border-b border-stone-200 flex justify-between items-center bg-orange-50">
              <h3 className="font-bold text-lg text-stone-900 flex items-center gap-2">
                <Users className="w-5 h-5 text-orange-600" /> {t('app.files.excepcionsTarefaTitulo', 'Tarefas individuais')}
              </h3>
              <button onClick={() => setShowOverridesModal(false)} className="p-2 text-stone-400 hover:text-stone-600 rounded-lg hover:bg-stone-200 transition-colors">
                <X className="w-5 h-5" />
              </button>
            </div>
            <div className="p-4 overflow-y-auto flex flex-col gap-2">
              <p className="text-xs text-stone-500">{t('app.files.excepcionsTarefaExplicacion', 'Este alumnado recibiu unha variante propia (xerada con Yang, ou editada aquí mesmo) en vez da tarefa xeral de arriba. Marca "Xeral" e preme "Aplicar cambios" para revertelos; ou preme un nome para cambiarlle a variante.')}</p>

              <div className="flex items-center gap-3 pb-1">
                <button type="button" onClick={() => marcarTodos(true)} disabled={overrideBusy !== ''}
                  className="text-xs text-indigo-700 hover:underline disabled:opacity-50">
                  {t('app.files.excepcionsMarcarTodos', 'Marcar todos → Xeral')}
                </button>
                {pendentesCount > 0 && (
                  <button type="button" onClick={() => marcarTodos(false)} disabled={overrideBusy !== ''}
                    className="text-xs text-stone-500 hover:underline disabled:opacity-50">
                    {t('app.files.excepcionsDesmarcarTodos', 'Desmarcar todos')}
                  </button>
                )}
              </div>

              {overrides.map((username) => {
                const pendente = !!revertPendentes[username];
                const editing = editingUsername === username;
                return (
                  <div key={username} className={`rounded-lg border px-3 py-2 flex flex-col gap-1.5 ${pendente ? 'border-stone-200 bg-stone-50' : 'border-indigo-200 bg-indigo-50/60'}`}>
                    <div className="flex items-center gap-3">
                      <label className="flex items-center gap-1 text-xs text-stone-600 cursor-pointer shrink-0">
                        <input type="radio" name={`estado-${username}`} checked={pendente} onChange={() => toggleRevert(username, true)} />
                        {t('app.files.excepcionsXeral', 'Xeral')}
                      </label>
                      <label className="flex items-center gap-1 text-xs text-indigo-700 cursor-pointer shrink-0">
                        <input type="radio" name={`estado-${username}`} checked={!pendente} onChange={() => toggleRevert(username, false)} />
                        {t('app.files.excepcionsIndividual', 'Individual')}
                      </label>
                      {pendente ? (
                        <span className="flex-1 min-w-0 truncate text-sm text-stone-500">{username}</span>
                      ) : (
                        <button type="button" onClick={() => setEditingUsername(editing ? '' : username)}
                          title={t('app.files.excepcionEditar', 'Cambiar a súa tarefa individual')}
                          className="flex-1 min-w-0 flex items-center gap-1.5 text-sm text-indigo-900 font-medium hover:underline text-left">
                          <span className="truncate">{username}</span>
                          <Pencil className="w-3.5 h-3.5 shrink-0 text-indigo-500" />
                        </button>
                      )}
                    </div>
                    {editing && !pendente && (
                      <div className="flex flex-col gap-1.5 border-t border-indigo-100 pt-1.5">
                        {editLoading ? (
                          <p className="text-xs text-stone-500 flex items-center gap-2"><Loader2 className="w-3.5 h-3.5 animate-spin" /> {t('app.files.cargando')}</p>
                        ) : (
                          <>
                            <MarkdownEditor value={editText} onChange={setEditText} />
                            <div className="flex items-center gap-2 flex-wrap">
                              {yangComprobado && yangDisponible && (
                                <button type="button" onClick={() => handleOpenYangAlumno(username)} disabled={editYangLaunching}
                                  title={t('app.files.editarConYangAlumnoDesc', 'Abre Yang para xerar a tarefa deste alumno - o resultado chega aquí en vivo')}
                                  className="px-2.5 py-1 text-xs bg-indigo-100 border border-indigo-300 text-indigo-800 font-medium hover:bg-indigo-200 rounded transition-colors flex items-center gap-1.5 disabled:opacity-50">
                                  {editYangLaunching ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Sparkles className="w-3.5 h-3.5" />}
                                  {t('app.files.editarConYangAlumno', 'Editar con Yang')}
                                </button>
                              )}
                              <button type="button" onClick={handleSaveEditor} disabled={editSaving}
                                className="px-2.5 py-1 text-xs bg-indigo-600 text-white font-medium rounded hover:bg-indigo-700 transition-colors disabled:opacity-50">
                                {editSaving ? t('app.files.gardando') : t('app.files.gardarExplicacionTarefa')}
                              </button>
                              <button type="button" onClick={() => setEditingUsername('')} disabled={editSaving}
                                className="px-2.5 py-1 text-xs text-stone-600 hover:bg-stone-100 rounded transition-colors disabled:opacity-50">
                                {t('app.files.non')}
                              </button>
                              {editError && <span className="text-xs text-red-600">{editError}</span>}
                              {editYangError && <span className="text-xs text-red-600">{editYangError}</span>}
                            </div>
                          </>
                        )}
                      </div>
                    )}
                  </div>
                );
              })}
              {overrideError && <span className="text-xs text-red-600">{overrideError}</span>}
            </div>
            <div className="p-4 border-t border-stone-200 flex items-center justify-between gap-2">
              <span className="text-xs text-stone-500">
                {pendentesCount > 0 ? t('app.files.excepcionsMarcados', { count: pendentesCount }) : ''}
              </span>
              <button type="button" onClick={handleAplicarCambios} disabled={pendentesCount === 0 || overrideBusy !== ''}
                className="px-3 py-1.5 text-sm bg-orange-500 text-white font-medium rounded-lg hover:bg-orange-600 transition-colors disabled:opacity-40">
                {overrideBusy === '*' ? t('app.files.excepcionsAplicando', 'Aplicando...') : t('app.files.excepcionsAplicar', 'Aplicar cambios')}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
});

// CommentFilesSection lists the clarifying files the professor attaches to their
// feedback on a student's práctica. 'manage' (professor) can add/download/remove; 'view'
// (student) can only download, and renders nothing when there are no files.
function CommentFilesSection({ subjectId, date, mode, owner }: { subjectId: string, date: string, mode: 'manage' | 'view', owner: string }) {
  const { t } = useTranslation();
  const [files, setFiles] = useState<WorkFile[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isUploading, setIsUploading] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [downloaded, setDownloaded] = useState('');
  const [pendingDelete, setPendingDelete] = useState('');
  const manage = mode === 'manage';

  // silent=true: ver comentario equivalente en WorkFilesSection/FILES_POLL_MS.
  const load = async (silent = false) => {
    if (!silent) { setIsLoading(true); setError(''); }
    try {
      setFiles(((manage ? await ListStudentCommentFiles(owner, subjectId, date) : await ListCommentFiles(subjectId, date)) || []));
    } catch (e: any) {
      if (!silent) setError(t('app.files.erroListar', { error: e?.message || e }));
    } finally { if (!silent) setIsLoading(false); }
  };
  useEffect(() => { load(); /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [subjectId, date, mode, owner]);
  useEffect(() => {
    const id = setInterval(() => load(true), FILES_POLL_MS);
    return () => clearInterval(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subjectId, date, mode, owner]);

  const handleUpload = async () => {
    setIsUploading(true); setError(''); setMessage('');
    try {
      const up = await UploadCommentFile(owner, subjectId, date);
      if (up && up.length > 0) { setMessage(t('app.files.adxuntados', { count: up.length })); await load(); }
    } catch (e: any) { setError(t('app.files.erroSubir', { error: e?.message || e })); await load(); }
    finally { setIsUploading(false); }
  };
  const handleDownload = async (name: string) => {
    setError(''); setMessage(''); setDownloaded('');
    try {
      const dest = manage ? await DownloadStudentCommentFile(owner, subjectId, date, name) : await DownloadCommentFile(subjectId, date, name);
      if (dest) { setMessage(t('app.files.gardadoEn', { dest })); setDownloaded(dest); }
    } catch (e: any) { setError(t('app.files.erroDescargar', { error: e?.message || e })); }
  };
  const handleDelete = async (name: string) => {
    setPendingDelete(''); setError(''); setMessage('');
    try { await DeleteCommentFile(owner, subjectId, date, name); await load(); }
    catch (e: any) { setError(t('app.files.erroEliminar', { error: e?.message || e })); }
  };

  // The student sees nothing when there are no clarifying files.
  if (!manage && !isLoading && files.length === 0) return null;

  return (
    <div className="flex flex-col gap-1.5 shrink-0">
      <div className="flex items-center justify-between">
        <label className="text-sm font-bold text-indigo-700 flex items-center gap-1.5">
          <Paperclip className="w-4 h-4" /> {t('app.files.ficheiroAclaratorio')} {manage ? '' : t('app.files.doProfesorado')}
        </label>
        {manage && (
          <button onClick={handleUpload} disabled={isUploading}
            className="px-3 py-1.5 text-sm bg-indigo-50 border border-indigo-300 text-indigo-700 font-medium hover:bg-indigo-100 rounded-lg transition-colors flex items-center gap-2 disabled:opacity-50">
            {isUploading ? <Loader2 className="w-4 h-4 animate-spin" /> : <Upload className="w-4 h-4" />}
            {isUploading ? t('app.files.subindo') : t('app.files.adxuntarFicheiro')}
          </button>
        )}
      </div>
      <div className="border border-stone-200 rounded-lg divide-y divide-stone-100 max-h-40 overflow-y-auto">
        {isLoading ? (
          <p className="p-3 text-sm text-stone-500 flex items-center gap-2"><Loader2 className="w-4 h-4 animate-spin" /> {t('app.files.cargando')}</p>
        ) : files.length === 0 ? (
          <p className="p-3 text-sm text-stone-500">{t('app.files.senFicheirosAclaratorios')}</p>
        ) : files.map(f => (
          <div key={f.name} className="p-2 flex items-center gap-2">
            <FileText className="w-4 h-4 text-indigo-400 shrink-0" />
            <span className="flex-1 min-w-0 truncate text-sm text-stone-800" title={f.name}>{f.name}</span>
            <span className="text-xs text-stone-400 shrink-0">{formatFileSize(f.size)}</span>
            {pendingDelete === f.name ? (
              <span className="flex items-center gap-1 shrink-0">
                <button onClick={() => handleDelete(f.name)} className="px-2 py-1 text-xs bg-red-600 text-white font-medium rounded hover:bg-red-700">{t('app.files.si')}</button>
                <button onClick={() => setPendingDelete('')} className="px-2 py-1 text-xs text-stone-600 font-medium rounded hover:bg-stone-100">{t('app.files.non')}</button>
              </span>
            ) : (
              <span className="flex items-center gap-1 shrink-0">
                <button onClick={() => handleDownload(f.name)} title={t('app.files.descargar')} className="p-1.5 text-stone-400 hover:text-stone-700 rounded hover:bg-stone-100"><Download className="w-4 h-4" /></button>
                {manage && <button onClick={() => setPendingDelete(f.name)} title={t('app.files.eliminar')} className="p-1.5 text-stone-400 hover:text-red-600 rounded hover:bg-red-50"><Trash2 className="w-4 h-4" /></button>}
              </span>
            )}
          </div>
        ))}
      </div>
      {message && (
        <div className="flex items-center gap-2 text-xs text-emerald-700">
          <span className="break-all min-w-0">{message}</span>
          {downloaded && (
            <button onClick={() => OpenFile(downloaded).catch((e: any) => setError(t('app.files.erroAbrir', { error: e?.message || e })))}
              className="flex items-center gap-1 shrink-0 px-2 py-1 rounded-lg bg-emerald-600 text-white font-medium hover:bg-emerald-700">
              <FileUp className="w-3.5 h-3.5" /> {t('app.files.abrir')}
            </button>
          )}
        </div>
      )}
      {error && <p className="text-xs text-red-600 flex items-start gap-1"><AlertCircle className="w-3 h-3 mt-0.5 shrink-0" />{error}</p>}
    </div>
  );
}

// CommentPanel is a floating, draggable panel where the professor writes the feedback
// for a student's práctica and attaches a clarifying file. It saves the comment on its
// own (immediately), independent of the práctica's own save button.
function CommentPanel({ username, subjectId, date, value, onChange, onSave, onClose }: {
  username: string, subjectId: string, date: string,
  value: string, onChange: (v: string) => void, onSave: () => Promise<void>, onClose: () => void,
}) {
  const { t } = useTranslation();
  const [pos, setPos] = useState(() => ({ x: Math.max(16, window.innerWidth / 2 - 230), y: 84 }));
  const dragRef = useRef<{ dx: number, dy: number } | null>(null);
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState('');
  const [err, setErr] = useState('');

  useEffect(() => {
    const move = (e: MouseEvent) => { if (dragRef.current) setPos({ x: e.clientX - dragRef.current.dx, y: e.clientY - dragRef.current.dy }); };
    const up = () => { dragRef.current = null; };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
    return () => { window.removeEventListener('mousemove', move); window.removeEventListener('mouseup', up); };
  }, []);
  const startDrag = (e: React.MouseEvent) => { dragRef.current = { dx: e.clientX - pos.x, dy: e.clientY - pos.y }; };

  const save = async () => {
    setSaving(true); setErr(''); setMsg('');
    try { await onSave(); setMsg(t('app.comentario.gardado')); }
    catch (e: any) { setErr(e?.message || String(e)); }
    finally { setSaving(false); }
  };

  return (
    <div className="fixed z-[60] w-[460px] max-w-[95vw] bg-white rounded-xl shadow-2xl border border-stone-300 flex flex-col"
      style={{ left: pos.x, top: pos.y, maxHeight: '82vh' }}>
      <div onMouseDown={startDrag} className="cursor-move select-none flex items-center justify-between gap-2 px-4 py-3 bg-indigo-600 text-white rounded-t-xl">
        <div className="flex items-center gap-2 font-bold text-sm"><Move className="w-4 h-4" /> {t('app.comentario.comentarPractica')}</div>
        <button onClick={onClose} className="p-1 hover:bg-white/20 rounded"><X className="w-4 h-4" /></button>
      </div>
      <div className="p-4 flex flex-col gap-4 overflow-y-auto">
        <div className="flex flex-col gap-1.5">
          <label className="text-sm font-bold text-stone-700">{t('app.comentario.comentarioParaAlumno')}</label>
          <MarkdownEditor value={value} onChange={onChange} />
        </div>
        <CommentFilesSection subjectId={subjectId} date={date} mode="manage" owner={username} />
      </div>
      <div className="p-3 border-t border-stone-200 bg-stone-50 rounded-b-xl flex items-center justify-between gap-2">
        <span className="text-xs min-w-0 break-all">
          {msg && <span className="text-emerald-600">{msg}</span>}
          {err && <span className="text-rose-600">{err}</span>}
        </span>
        <button onClick={save} disabled={saving}
          className="flex items-center gap-2 px-4 py-2 bg-indigo-600 text-white rounded-lg text-sm font-medium hover:bg-indigo-700 transition-colors disabled:opacity-50 shrink-0">
          {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />} {t('app.comentario.gardarComentario')}
        </button>
      </div>
    </div>
  );
}

// estadoLabel traduce o código de estado dunha sesión (DayStatus) á etiqueta amosada,
// usado para o badge en só lectura que ve o alumnado (o estado decídeo o profesor).
function estadoLabel(status: DayStatus, t: (key: string) => string): string {
  switch (status) {
    case 'exame': return t('app.dayModal.exameAvaliacion');
    case 'folga': return t('app.dayModal.folga');
    case 'excursion': return t('app.dayModal.saidaActividade');
    case 'ausencia': return t('app.dayModal.ausencia');
    default: return t('app.dayModal.claseNormal');
  }
}

function DayModal({ day, existingLog, professorMode = false, readOnlyLog = false, subjectId, subjectName = '', academicYear = '', profile = null, filesMode = 'off', filesOwner = '', commentMode = 'off', taskMode = 'off', today, hostname = '', onClose, onSave, onSaveQuiet, onSaveComment, onReprogram, onAddIncident, onRemoveIncident }: { day: any, existingLog?: DailyLog, professorMode?: boolean, readOnlyLog?: boolean, subjectId: string, subjectName?: string, academicYear?: string, profile?: StudentProfile | null, filesMode?: FilesMode, filesOwner?: string, commentMode?: CommentMode, taskMode?: TaskMode, today: string, hostname?: string, onClose: () => void, onSave: (log: DailyLog) => void, onSaveQuiet?: (log: DailyLog) => void, onSaveComment?: (date: string, comment: string) => Promise<void>, onReprogram?: (date: string, action: ReprogramAction, count: number) => void, onAddIncident?: (date: string, type: IncidentType, note: string) => Promise<void>, onRemoveIncident?: (date: string, incidentID: string) => Promise<void> }) {
  const { t } = useTranslation();
  const [content, setContent] = useState(existingLog?.content || '');
  const [status, setStatus] = useState<DayStatus>(existingLog?.status || 'lectivo');
  const [unlocked, setUnlocked] = useState(!!existingLog?.unlocked);
  const [comment, setComment] = useState(existingLog?.profesorComment || '');
  const [showCommentPanel, setShowCommentPanel] = useState(false);
  // Nova incidencia en curso (positivo/negativo/expulsión/falta de orde): non hai
  // "gardar sesión" que a persista — cada AddIncident/RemoveIncident vai decontado ao
  // backup do alumno (ver handleAddIncident en App.tsx), así que só fai falla o tipo
  // escollido e un motivo opcional mentres se compón.
  const [newIncidentType, setNewIncidentType] = useState<IncidentType | null>(null);
  const [newIncidentNote, setNewIncidentNote] = useState('');
  const [savingIncident, setSavingIncident] = useState(false);
  const [reprogCount, setReprogCount] = useState(1);
  // dayTab: profesorado (deseñando/revisando a programación) escolle entre "o que se
  // traballou" e "a tarefa"; o alumnado (e o profesorado revisando/vendo coma un
  // alumno) escolle entre "a tarefa" e "a súa resposta" - ver isProfessorEditingModel
  // e DayTabButton máis abaixo. Cada vista arranca na pestana que máis lle interesa.
  const [dayTab, setDayTab] = useState<'sesion' | 'tarefa' | 'resposta'>(professorMode ? 'sesion' : 'resposta');
  // TAB_COLORS: cor propia por pestana cando está activa (para distinguilas dun
  // vistazo - "Tarefa" sempre en ámbar, coma o seu cadro; "resposta" en esmeralda,
  // coma o botón do informe asinado; "sesion" en índigo, coma o resto de accións do
  // profesorado).
  const TAB_COLORS: Record<string, string> = {
    sesion: 'border-indigo-600 text-indigo-700',
    tarefa: 'border-amber-500 text-amber-700',
    resposta: 'border-emerald-600 text-emerald-700',
  };
  const DayTabButton = ({ tab, children }: { tab: 'sesion' | 'tarefa' | 'resposta'; children: React.ReactNode }) => (
    <button
      type="button"
      onClick={() => setDayTab(tab)}
      className={`px-3 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${dayTab === tab ? TAB_COLORS[tab] : 'border-transparent text-stone-500 hover:text-stone-700'}`}
    >
      {children}
    </button>
  );
  // Modelo mode: whether this day is currently marked as a no-class session.
  const isNoClassDay = professorMode && !!existingLog && NO_CLASS.includes(existingLog.status);
  // Por defecto só se pode editar a práctica do día de hoxe; días pasados ou futuros quedan
  // bloqueados para o alumnado agás que o profesor os desbloquee expresamente (por sesión).
  // O profesorado nunca está suxeito a este bloqueo.
  const isLockedForStudent = !professorMode && day.dateString !== today && !unlocked;
  // isProfessorEditingModel: o profesorado deseñando/revisando a súa propia
  // programación (edita "Sesión"/"Tarefa"), distinto de calquera outro caso en que
  // professorMode tamén é true pero en só lectura - "Acceder como" un alumno
  // (readOnlyLog) amosa exactamente o punto de vista do alumnado, tamén coas súas
  // pestanas ("Tarefa"/"A miña resposta", en só lectura aí).
  const isProfessorEditingModel = professorMode && !readOnlyLog;

  // buildLog: o rexistro tal e como está no editor neste intre - úsao tanto "Gardar
  // práctica" coma o informe asinado (que garda antes de xerar, ver handleQuickReport).
  const buildLog = (): DailyLog => {
    // Keep the professor's comment: it is the editor's value when the professor is
    // editing, or the existing one otherwise (so a student saving does not wipe it).
    const finalComment = (commentMode === 'edit' ? comment : (existingLog?.profesorComment || '')).trim();
    const log: DailyLog = { date: day.dateString, content, status };
    if (finalComment) log.profesorComment = finalComment;
    // As incidencias non se editan neste formulario (van pola súa propia sección, con
    // AddIncident/RemoveIncident xa persistidas decontado) — hai que conservalas
    // explicitamente aquí, senón "Gardar sesión" sobrescribiría o día enteiro e
    // borraríaas do backup do alumno.
    if (existingLog?.incidents?.length) log.incidents = existingLog.incidents;
    if (professorMode) {
      // O desbloqueo é unha decisión do profesor, gárdase no seu log-modelo desta sesión.
      log.unlocked = unlocked;
    } else if (hostname) {
      // Rexistro automático do equipo empregado, para localizar responsables en caso de
      // desperfectos. Só se garda ao gardar dende o propio alumno, nunca ao editar o profesor.
      log.equipo = hostname;
    }
    return log;
  };

  // taskSectionRef: ver TaskFilesSectionHandle — permítelle a "Gardar sesión" gardar
  // tamén unha explicación de tarefa escrita e non gardada explicitamente, en vez de
  // perderse. É `null` cando TaskFilesSection non está montado (pestana "Tarefa" non
  // aberta nesta xanela), caso en que non hai nada pendente que gardar.
  const taskSectionRef = useRef<TaskFilesSectionHandle>(null);

  const handleSave = async () => {
    await taskSectionRef.current?.flushPendingDescription();
    onSave(buildLog());
  };

  // Informe asinado desta sesión: garda a práctica (sen pechar a xanela, ver
  // onSaveQuiet) e xera+asina+sobe o PDF decontado, sen ningunha xanela intermedia de
  // alcance/data (esa é só do informe xeral). Ao rematar, actualiza a lista de
  // ficheiros adxuntos (WorkFilesSection, máis abaixo) para que o PDF xa apareza aí,
  // coma calquera outro ficheiro subido.
  const [reportBusy, setReportBusy] = useState(false);
  const [reportError, setReportError] = useState('');
  // reportPath: copia local de usar-e-tirar do último informe xerado nesta sesión, só
  // para o botón "Abrir" que aparece a carón — mesmo criterio ca savedPath en
  // ReportBuilder.tsx (o informe en si queda gardado no servidor, non aquí).
  const [reportPath, setReportPath] = useState('');
  const [filesRefresh, setFilesRefresh] = useState(0);
  const handleQuickReport = async () => {
    if (!profile) return;
    setReportError('');
    setReportPath('');
    if (!profile.hasCert) { setReportError(t('reportBuilder.noCertError')); return; }
    const log = buildLog();
    if (!log.content.trim()) { setReportError(t('reportBuilder.noEntries')); return; }
    onSaveQuiet?.(log);
    setReportBusy(true);
    try {
      let files: WorkFile[] = [];
      try {
        files = await ListWorkFilesForReport(subjectId, day.dateString);
      } catch {
        files = [];
      }
      const html = buildReportHTML({
        t, today, subjectName, academicYear, profile,
        scopeDescription: t('reportBuilder.scopeSession', { date: fmtShort(day.dateString) }),
        entries: [{ dateString: day.dateString, content: log.content, status: log.status, unitId: day.unit?.id, unitTitle: day.unit?.title, incidents: log.incidents }],
        filesByDate: { [day.dateString]: files },
      });
      const path = await ExportSignedReport(html, `informe-sesion-${day.dateString}.pdf`, subjectId, day.dateString);
      setReportPath(path || '');
      setFilesRefresh((n) => n + 1);
    } catch (err: any) {
      setReportError(typeof err === 'string' ? err : (err?.message || t('reportBuilder.generateError')));
    } finally {
      setReportBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-0">
      <div className="bg-white shadow-xl w-full h-full max-w-none overflow-hidden flex flex-col">
        <div className="p-3 border-b border-stone-200 flex justify-between items-center bg-stone-50">
          <div>
            <h3 className="font-bold text-base text-stone-900">{professorMode ? t('app.dayModal.sesionDeClase') : t('app.dayModal.practicaDoDia')}</h3>
            <p className="text-xs text-stone-500">{new Date(day.date).toLocaleDateString('gl-ES', { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric' })}</p>
            {existingLog?.equipo && (
              <p className="text-xs text-stone-400 flex items-center gap-1 mt-0.5">
                <Monitor className="w-3 h-3" /> {t('app.dayModal.equipoUsado', { equipo: existingLog.equipo })}
              </p>
            )}
          </div>
          <div className="flex items-center gap-1">
            {onSaveQuiet && (
              <button
                onClick={handleQuickReport}
                disabled={reportBusy}
                className="p-2 text-emerald-600 hover:text-emerald-700 rounded-lg hover:bg-emerald-50 transition-colors disabled:opacity-50"
                title={t('app.dayModal.xerarInformeAsinadoTitulo')}
              >
                {reportBusy ? <Loader2 className="w-5 h-5 animate-spin" /> : <FileSignature className="w-5 h-5" />}
              </button>
            )}
            {onSaveQuiet && reportPath && (
              <button
                onClick={() => OpenFile(reportPath).catch((e: any) => setReportError(e?.message || String(e)))}
                className="p-2 text-emerald-600 hover:text-emerald-700 rounded-lg hover:bg-emerald-50 transition-colors"
                title={t('reportBuilder.openButton')}
              >
                <FolderOpen className="w-5 h-5" />
              </button>
            )}
            <button onClick={onClose} className="p-2 text-red-500 hover:text-red-700 rounded-lg hover:bg-red-50 transition-colors">
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        {reportError && (
          <div className="px-3 py-2 bg-rose-50 border-b border-rose-200 text-xs text-rose-700 flex items-center gap-1.5">
            <AlertCircle className="w-3.5 h-3.5 shrink-0" /> {reportError}
          </div>
        )}

        <div className="p-4 flex flex-col gap-3 overflow-y-auto flex-1 min-h-0">
          {day.unit && status === 'lectivo' && (
            <div className={`p-2 rounded-lg border border-black/10 flex items-center gap-2 ${day.unit.bgLight}`}>
              <div className={`w-7 h-7 rounded-lg flex items-center justify-center ${day.unit.color} ${day.unit.text} font-bold text-sm shrink-0`}>
                {day.unit.id}
              </div>
              <div className="min-w-0">
                <p className="text-[10px] font-bold text-stone-600 uppercase tracking-wider">{t('app.dayModal.unidadeProgramada')}</p>
                <p className="text-sm font-medium text-stone-900 truncate">{day.unit.title}</p>
              </div>
            </div>
          )}

          {/* Sesión (profesorado): corrección coa IA, estado — inclúe folga/saída/ausencia,
              que reprograman ao instante (piden confirmación e pechan o modal, ver
              handleReprogram) igual có botón que substitúen — e reprogramación de
              sesións, todo compacto nun só bloque. Para o alumnado (e «Acceder como»),
              o estado amósase en só lectura co que marcou o profesor. */}
          {professorMode ? (
            <div className="flex flex-col gap-2 rounded-lg border border-stone-200 bg-stone-50 p-2.5">
              {!isNoClassDay && (
                <PracticaCorrectionSection subjectId={subjectId} date={day.dateString} />
              )}
              {/* As tres cousas nunha soa liña (envolve en pantallas estreitas): estado,
                  desbloqueo e reprogramación son accións relacionadas do mesmo xesto
                  profesoral sobre esta sesión, non bloques independentes. */}
              <div className="flex flex-wrap items-start gap-3">
                <div className="flex flex-col gap-1.5">
                  <label className="text-sm font-bold text-stone-700">{t('app.dayModal.estadoDaSesion')}</label>
                  <select
                    value={status}
                    onChange={(e) => {
                      const next = e.target.value as DayStatus;
                      if (!onReprogram) { setStatus(next); return; }
                      if ((NO_CLASS as string[]).includes(next)) {
                        // Reprograma ao instante (desprazamento de programación):
                        // igual có antigo botón Folga/Saída/Ausencia.
                        onReprogram(day.dateString, { kind: 'markNoClass', status: next as NoClassStatus }, 1);
                        return;
                      }
                      if (isNoClassDay) {
                        // Saír dun día sen clase tamén reprograma (igual có antigo
                        // botón «Restaurar lectivo»): volve sempre a lectivo, mesmo
                        // se se escolleu «Exame» — para exame hai que reabrir despois.
                        onReprogram(day.dateString, { kind: 'clearNoClass' }, 1);
                        return;
                      }
                      setStatus(next);
                    }}
                    className="p-2 bg-white border border-stone-300 rounded-lg shadow-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none text-sm"
                  >
                    <option value="lectivo">{t('app.dayModal.claseNormal')}</option>
                    <option value="exame">{t('app.dayModal.exameAvaliacion')}</option>
                    <option value="folga">{t('app.dayModal.folga')}</option>
                    <option value="excursion">{t('app.dayModal.saidaActividade')}</option>
                    <option value="ausencia">{t('app.dayModal.ausencia')}</option>
                  </select>
                </div>

                {/* Bloqueo da práctica: por defecto o alumnado só pode editar o día de hoxe;
                    o profesor pode desbloquear esta sesión en concreto se o considera oportuno.
                    Sen sentido nun día sen clase (non hai práctica que desbloquear). */}
                {!isNoClassDay && day.dateString !== today && (
                  <label className="flex items-center gap-2 p-2 rounded-lg border border-stone-200 bg-white text-sm text-stone-700 cursor-pointer self-end">
                    <input type="checkbox" checked={unlocked} onChange={(e) => setUnlocked(e.target.checked)}
                      className="w-4 h-4 accent-stone-900" />
                    {unlocked ? <Unlock className="w-3.5 h-3.5 text-emerald-600" /> : <Lock className="w-3.5 h-3.5 text-stone-400" />}
                    {t('app.dayModal.desbloquearPractica')}
                  </label>
                )}

                {onReprogram && (
                  <div className="flex flex-col gap-1 rounded-lg border border-stone-200 bg-white p-2">
                    <span className="text-[11px] font-medium text-stone-500">{t('app.dayModal.reprogramarSesions')}</span>
                    <div className="flex flex-wrap items-center gap-1.5">
                      <label className="text-xs text-stone-600 flex items-center gap-1">
                        {t('app.dayModal.numeroAbreviado')}
                        <input type="number" min={1} value={reprogCount}
                          onChange={(e) => setReprogCount(Math.max(1, parseInt(e.target.value || '1', 10)))}
                          className="w-14 p-1 bg-white border border-stone-300 rounded-lg text-xs" />
                      </label>
                      <button onClick={() => onReprogram(day.dateString, { kind: 'insert' }, reprogCount)} className="px-2 py-1 text-xs bg-emerald-100 border border-emerald-300 text-emerald-800 rounded-lg font-medium hover:bg-emerald-200">{t('app.dayModal.inserirSesions')}</button>
                      <button onClick={() => onReprogram(day.dateString, { kind: 'delete' }, reprogCount)} className="px-2 py-1 text-xs bg-rose-100 border border-rose-300 text-rose-800 rounded-lg font-medium hover:bg-rose-200">{t('app.dayModal.eliminarSesions')}</button>
                    </div>
                  </div>
                )}
              </div>
            </div>
          ) : (
            <div className="flex flex-col gap-1.5">
              <label className="text-sm font-bold text-stone-700">{t('app.dayModal.estadoDaSesion')}</label>
              <p className="w-full p-2 bg-stone-50 border border-stone-200 rounded-lg text-sm text-stone-700 font-medium">
                {estadoLabel(status, t)}
              </p>
            </div>
          )}

          {/* Práctica bloqueada: por defecto só se pode editar a do día de hoxe. */}
          {!isNoClassDay && isLockedForStudent && (
            <div className="flex items-center gap-2 rounded-lg border border-stone-300 bg-stone-50 p-2.5 text-sm text-stone-600">
              <Lock className="w-4 h-4 shrink-0" />
              {t('app.dayModal.practicaBloqueada')}
            </div>
          )}

          {/* Profesorado deseñando/revisando a programación: dúas pestanas - o que se
              traballou na sesión (o editor de sempre) e a tarefa para o alumnado
              (explicación + ficheiro opcional, TaskFilesSection). */}
          {!isNoClassDay && isProfessorEditingModel && (
            <div className="flex flex-col gap-1.5 flex-1 min-h-[420px]">
              <div className="flex items-center gap-1 border-b border-stone-200">
                <DayTabButton tab="sesion">{t('app.dayModal.pestanaSesion')}</DayTabButton>
                <DayTabButton tab="tarefa">{t('app.dayModal.pestanaTarefa')}</DayTabButton>
              </div>
              {dayTab === 'tarefa' ? (
                taskMode !== 'off' && <TaskFilesSection ref={taskSectionRef} subjectId={subjectId} date={day.dateString} mode={taskMode} />
              ) : (
                <MarkdownEditor
                  value={content}
                  onChange={setContent}
                  fill
                  detachable
                  title={t('app.dayModal.queTraballouNesaSesion')}
                />
              )}
            </div>
          )}

          {/* Todos os demais casos (alumnado editando a súa propia práctica, profesorado
              revisando ou "Acceder como" un alumno): mesmas dúas pestanas, desta volta
              tarefa/resposta - a resposta é editable ou só lectura segundo corresponda
              (isLockedForStudent/readOnlyLog), pero segue vivindo na mesma pestana. */}
          {!isNoClassDay && !isProfessorEditingModel && (
            <div className="flex flex-col gap-1.5 flex-1 min-h-[420px]">
              <div className="flex items-center gap-1 border-b border-stone-200">
                <DayTabButton tab="tarefa">{t('app.dayModal.pestanaTarefa')}</DayTabButton>
                <DayTabButton tab="resposta">{t('app.dayModal.pestanaResposta')}</DayTabButton>
              </div>
              {dayTab === 'tarefa' ? (
                taskMode !== 'off' && <TaskFilesSection ref={taskSectionRef} subjectId={subjectId} date={day.dateString} mode={taskMode} fill />
              ) : isLockedForStudent ? (
                <div className="flex items-center gap-2 rounded-lg border border-stone-300 bg-stone-50 p-2.5 text-sm text-stone-600">
                  <Lock className="w-4 h-4 shrink-0" />
                  {t('app.dayModal.practicaBloqueada')}
                </div>
              ) : readOnlyLog ? (
                <div className="p-3 rounded-lg border border-stone-200 bg-stone-50 text-sm text-stone-800 overflow-y-auto">
                  {(content || '').trim() ? <MarkdownPreview content={content} /> : (
                    <p className="text-stone-500 italic">{t('app.dayModal.senRexistro')}</p>
                  )}
                </div>
              ) : (
                <MarkdownEditor
                  value={content}
                  onChange={setContent}
                  fill
                  detachable
                  title={t('app.dayModal.pestanaResposta')}
                />
              )}
            </div>
          )}

          {/* Student view: the professor's feedback (text + clarifying files). */}
          {commentMode === 'view' && (existingLog?.profesorComment || '').trim() && (
            <div className="flex flex-col gap-1.5 shrink-0">
              <label className="text-sm font-bold text-indigo-700 flex items-center gap-1.5">
                <MessageSquare className="w-4 h-4" />
                {t('app.dayModal.comentarioDoProfesor')}
              </label>
              <div className="p-3 rounded-lg border border-indigo-200 bg-indigo-50 text-sm text-stone-800">
                <MarkdownPreview content={existingLog!.profesorComment!} />
              </div>
            </div>
          )}
          {commentMode === 'view' && (
            <CommentFilesSection subjectId={subjectId} date={day.dateString} mode="view" owner="" />
          )}

          {/* Incidencias (positivo/negativo/expulsión/falta de orde): a listaxe é visible
              para o alumnado (commentMode 'view', igual có comentario de arriba) e para o
              profesorado; engadir/eliminar só cando o profesorado está a revisar este
              alumno en concreto (commentMode 'edit' — mesma condición que o comentario). */}
          {commentMode !== 'off' && (
            <div className="flex flex-col gap-1.5 shrink-0">
              <label className="text-sm font-bold text-stone-700 flex items-center gap-1.5">
                <AlertCircle className="w-4 h-4" />
                {t('app.dayModal.incidencias')}
              </label>
              {(existingLog?.incidents || []).length > 0 && (
                <ul className="flex flex-col gap-1">
                  {(existingLog?.incidents || []).map(inc => (
                    <li key={inc.id} className="flex items-center gap-2 p-2 rounded-lg border border-stone-200 bg-white text-sm">
                      <span title={t(INCIDENT_LABEL_KEYS[inc.type])}
                        className={`w-5 h-5 shrink-0 leading-5 rounded-full text-xs font-bold text-center ${INCIDENT_COLORS[inc.type]}`}>
                        {INCIDENT_SYMBOLS[inc.type]}
                      </span>
                      <span className="font-medium text-stone-700">{t(INCIDENT_LABEL_KEYS[inc.type])}</span>
                      {inc.note && <span className="text-stone-500 truncate">— {inc.note}</span>}
                      {commentMode === 'edit' && (
                        <button
                          onClick={() => onRemoveIncident && onRemoveIncident(day.dateString, inc.id)}
                          className="ml-auto p-1 text-stone-400 hover:text-rose-600 hover:bg-rose-50 rounded transition-colors"
                          title={t('app.dayModal.eliminarIncidencia')}
                        >
                          <X className="w-3.5 h-3.5" />
                        </button>
                      )}
                    </li>
                  ))}
                </ul>
              )}
              {commentMode === 'edit' && (
                <div className="flex flex-col gap-2 p-2 rounded-lg border border-dashed border-stone-300">
                  <div className="flex items-center gap-1.5">
                    {INCIDENT_TYPES.map(ty => (
                      <button
                        key={ty}
                        type="button"
                        onClick={() => setNewIncidentType(prev => prev === ty ? null : ty)}
                        title={t(INCIDENT_LABEL_KEYS[ty])}
                        className={`w-7 h-7 rounded-full text-sm font-bold transition-colors ${newIncidentType === ty ? INCIDENT_COLORS[ty] : 'bg-stone-100 text-stone-500 hover:bg-stone-200'}`}
                      >
                        {INCIDENT_SYMBOLS[ty]}
                      </button>
                    ))}
                    {newIncidentType && (
                      <input
                        type="text"
                        value={newIncidentNote}
                        onChange={e => setNewIncidentNote(e.target.value)}
                        placeholder={t('app.dayModal.motivoOpcional')}
                        className="flex-1 min-w-0 px-2 py-1 text-sm rounded-lg border border-stone-200"
                      />
                    )}
                    {newIncidentType && (
                      <button
                        type="button"
                        disabled={savingIncident}
                        onClick={async () => {
                          if (!onAddIncident || !newIncidentType) return;
                          setSavingIncident(true);
                          try {
                            await onAddIncident(day.dateString, newIncidentType, newIncidentNote);
                            setNewIncidentType(null);
                            setNewIncidentNote('');
                          } finally {
                            setSavingIncident(false);
                          }
                        }}
                        className="px-2 py-1 text-sm font-medium bg-stone-900 text-white rounded-lg hover:bg-stone-800 disabled:opacity-50"
                      >
                        {t('app.dayModal.engadir')}
                      </button>
                    )}
                  </div>
                </div>
              )}
            </div>
          )}

          {filesMode !== 'off' && (
            <WorkFilesSection subjectId={subjectId} date={day.dateString} mode={filesMode} owner={filesOwner} refreshSignal={filesRefresh} />
          )}
        </div>

        <div className="p-3 border-t border-stone-200 bg-stone-50 flex justify-end gap-2">
          {commentMode === 'edit' && (
            <button onClick={() => setShowCommentPanel(true)}
              className="mr-auto flex items-center gap-2 px-4 py-2 bg-indigo-600 text-white font-medium hover:bg-indigo-700 rounded-lg transition-colors">
              <MessageSquare className="w-4 h-4" /> {t('app.dayModal.comentar')}
            </button>
          )}
          <button onClick={onClose} className="px-4 py-2 text-stone-600 font-medium hover:bg-stone-200 rounded-lg transition-colors">
            {isNoClassDay || readOnlyLog || isLockedForStudent ? t('app.pechar') : t('app.cancelar')}
          </button>
          {!isNoClassDay && !readOnlyLog && !isLockedForStudent && (
            <button onClick={handleSave} className="px-4 py-2 bg-stone-900 text-white font-medium hover:bg-stone-800 rounded-lg transition-colors flex items-center gap-2">
              <Save className="w-4 h-4" />
              {professorMode ? t('app.dayModal.gardarSesion') : t('app.dayModal.gardarPractica')}
            </button>
          )}
        </div>
      </div>

      {showCommentPanel && commentMode === 'edit' && (
        <CommentPanel
          username={filesOwner}
          subjectId={subjectId}
          date={day.dateString}
          value={comment}
          onChange={setComment}
          onSave={async () => { if (onSaveComment) await onSaveComment(day.dateString, comment); }}
          onClose={() => setShowCommentPanel(false)}
        />
      )}
    </div>
  );
}

// PrivateNoteModal: nota de texto en markdown do profesorado sobre un alumno, por
// materia (punto 3 do pedido). Nunca visible para o alumnado nin para outro profesor —
// GetPrivateNote/SavePrivateNote (Go) xa comproban que quen a le/escribe é o profesor
// dono da materia (ou un admin, á parte en AdminPanel), e o contido non viaxa polo
// backup do alumno nin polos informes que este xera. Mesmo editor markdown ca sesión/
// tarefa (MarkdownEditor), para manter unha soa forma de escribir texto na app.
function PrivateNoteModal({ username, materiaID, studentLabel, onClose }: { username: string; materiaID: string; studentLabel: string; onClose: () => void }) {
  const { t } = useTranslation();
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    GetPrivateNote(username, materiaID)
      .then(v => { if (!cancelled) setContent(v || ''); })
      .catch((e: any) => { if (!cancelled) setError(e?.message || String(e)); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [username, materiaID]);

  const handleSave = async () => {
    setSaving(true);
    setError('');
    setSaved(false);
    try {
      await SavePrivateNote(username, materiaID, content);
      setSaved(true);
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-2xl overflow-hidden flex flex-col max-h-[90vh]">
        <div className="p-4 border-b border-stone-200 flex justify-between items-center bg-stone-50">
          <div>
            <h3 className="font-bold text-lg text-stone-900 flex items-center gap-2">
              <Pencil className="w-5 h-5" /> {t('app.notasPrivadas.titulo')}
            </h3>
            <p className="text-xs text-stone-500 mt-0.5">{t('app.notasPrivadas.aviso', { alumno: studentLabel })}</p>
          </div>
          <button onClick={onClose} className="p-2 text-stone-400 hover:text-stone-600 rounded-lg hover:bg-stone-200 transition-colors">
            <X className="w-5 h-5" />
          </button>
        </div>
        <div className="p-6 overflow-y-auto flex-1 flex flex-col gap-3">
          {loading ? (
            <div className="flex items-center gap-2 text-stone-500 text-sm"><Loader2 className="w-4 h-4 animate-spin" /> {t('app.cargando')}</div>
          ) : (
            <MarkdownEditor value={content} onChange={setContent} fill />
          )}
          {error && <div className="text-sm text-rose-600">{error}</div>}
          {saved && <div className="text-sm text-emerald-600">{t('app.notasPrivadas.gardada')}</div>}
        </div>
        <div className="p-3 border-t border-stone-200 bg-stone-50 flex justify-end gap-2">
          <button onClick={onClose} className="px-4 py-2 text-stone-600 font-medium hover:bg-stone-200 rounded-lg transition-colors">
            {t('app.pechar')}
          </button>
          <button onClick={handleSave} disabled={loading || saving}
            className="px-4 py-2 bg-stone-900 text-white font-medium hover:bg-stone-800 rounded-lg transition-colors flex items-center gap-2 disabled:opacity-50">
            <Save className="w-4 h-4" /> {t('app.gardar')}
          </button>
        </div>
      </div>
    </div>
  );
}

function SettingsModal({ config, units, subjects, profile, canEditProgramming = true, showGroups = false, groups = [], onProfileUpdate, onClose, onSave, onImport, onExtracted }: { config: AppConfig, units: Unit[], subjects: Subject[], profile: StudentProfile | null, canEditProgramming?: boolean, showGroups?: boolean, groups?: string[], onProfileUpdate: (p: StudentProfile) => void, onClose: () => void, onSave: (c: AppConfig, u: Unit[], groups?: string[]) => void, onImport: (s: Subject[]) => void, onExtracted?: (data: { proensUrl?: string, dogUrl?: string, programacionInfo?: ProensInfo }) => void }) {
  const { t } = useTranslation();
  const [groupsText, setGroupsText] = useState(groups.join(', '));
  // dogUrl e proensUrl persisten as dúas igual (a diferenza de antes, cando dogUrl só
  // servía para importar o calendario unha vez e esquecíase ao pechar): unha vez gardadas
  // en Configuración, reutilízanse sen ter que volver escribilas cada vez.
  const [dogUrl, setDogUrl] = useState(config.dogUrl || '');
  const [proensUrl, setProensUrl] = useState(config.proensUrl || '');
  const [isImporting, setIsImporting] = useState(false);
  const [isProcessingAI, setIsProcessingAI] = useState(false);
  const [importMessage, setImportMessage] = useState('');
  const [backupMessage, setBackupMessage] = useState('');
  const [backupError, setBackupError] = useState(false);
  const [pendingProgrammingData, setPendingProgrammingData] = useState<any[] | null>(null);

  // Change-password form (only usable when connected to a File Browser server).
  // The current password is not requested again: it was already entered at login.
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [isChangingPassword, setIsChangingPassword] = useState(false);
  const [passwordMessage, setPasswordMessage] = useState('');
  const [passwordError, setPasswordError] = useState(false);

  // Real name/surname used for the certificate subject and the visible signature.
  // File Browser only knows the login, so we start blank when the profile name is
  // just the username, prompting the student to enter their real name.
  const [certName, setCertName] = useState(() =>
    profile && profile.name && profile.name !== profile.username ? profile.name : '');
  const [certSurname, setCertSurname] = useState(() => (profile && profile.surname) || '');

  const errText = (e: any) => (typeof e === 'string' ? e : (e?.message || t('app.settings.erroDescoñecido')));

  // regenerateCert issues the certificate with the entered name and syncs the profile
  // so the header badge and the signature name update immediately.
  const regenerateCert = async () => {
    const p = await RegenerateCertificate(certName, certSurname);
    onProfileUpdate({
      username: p.username,
      name: p.name,
      surname: p.surname,
      group: p.group,
      certPath: p.certPath,
      hasCert: p.hasCert,
      certSource: p.certSource,
      connected: p.connected,
    });
  };

  const handleChangePassword = async () => {
    setPasswordMessage('');
    setPasswordError(false);
    if (!certName.trim()) {
      setPasswordError(true);
      setPasswordMessage(t('app.settings.introduceNomeApelidos'));
      return;
    }
    if (!newPassword) {
      setPasswordError(true);
      setPasswordMessage(t('app.settings.introduceNovoContrasinal'));
      return;
    }
    if (newPassword !== confirmPassword) {
      setPasswordError(true);
      setPasswordMessage(t('app.settings.contrasinaisNonCoinciden'));
      return;
    }
    setIsChangingPassword(true);
    try {
      await ChangePassword(newPassword);
      // Password changed: now reissue the certificate with the new password. A
      // certificate failure must not read as a password failure.
      let message = t('app.settings.contrasinalCambiado');
      try {
        await regenerateCert();
        message += ' ' + t('app.settings.certificadoRexenerado');
      } catch (certErr: any) {
        message += ' ' + t('app.settings.avisoNonRexenerarCertificado', { error: errText(certErr) });
      }
      setPasswordError(false);
      setPasswordMessage(message);
      setNewPassword('');
      setConfirmPassword('');
    } catch (err: any) {
      setPasswordError(true);
      setPasswordMessage(errText(err));
    } finally {
      setIsChangingPassword(false);
    }
  };


  const [localConfig, setLocalConfig] = useState<AppConfig>({
    ...config,
    scheduleDays: config.scheduleDays || [1, 3, 5],
    customHolidays: config.customHolidays || [],
    // A táboa é a fonte de verdade dos días non lectivos. Se a materia aínda vén no
    // formato antigo (listas de días soltos), migrase aquí unha soa vez: os días
    // consecutivos co mesmo nome agrúpanse nun rango.
    calendar: config.calendar ?? calendarEntriesFromLegacy(config),
  });
  const [unitsText, setUnitsText] = useState(units.map(u => `${u.title} - ${u.sessions}`).join('\n'));

  const daysOfWeek = [
    { id: 1, label: t('app.settings.luns') },
    { id: 2, label: t('app.settings.martes') },
    { id: 3, label: t('app.settings.mercores') },
    { id: 4, label: t('app.settings.xoves') },
    { id: 5, label: t('app.settings.venres') },
  ];

  const toggleDay = (dayId: number) => {
    setLocalConfig(prev => {
      const days = prev.scheduleDays.includes(dayId)
        ? prev.scheduleDays.filter(d => d !== dayId)
        : [...prev.scheduleDays, dayId].sort();
      return { ...prev, scheduleDays: days };
    });
  };

  const handleExportBackup = () => {
    const dataStr = JSON.stringify(subjects, null, 2);
    const blob = new Blob([dataStr], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `backup-cronograma-${new Date().toISOString().split('T')[0]}.json`;
    link.click();
    URL.revokeObjectURL(url);
    setBackupError(false);
    setBackupMessage(t('app.settings.copiaDescargadaExito'));
    setTimeout(() => setBackupMessage(''), 3000);
  };

  const handleImportBackup = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (event) => {
      try {
        const imported = JSON.parse(event.target?.result as string);
        if (Array.isArray(imported) && imported.length > 0 && imported[0].id) {
          // Add default scheduleDays if missing in imported data
          const migrated = imported.map((s: any) => ({
            ...s,
            config: {
              ...s.config,
              scheduleDays: s.config?.scheduleDays || [1, 3, 5]
            }
          }));
          onImport(migrated);
        } else {
          setBackupError(true);
          setBackupMessage(t('app.settings.formatoIncorrecto'));
        }
      } catch (err) {
        setBackupError(true);
        setBackupMessage(t('app.settings.erroLerJson'));
      }
    };
    reader.readAsText(file);
  };

  const handleImport = async () => {
    if (!dogUrl && !proensUrl) {
      setImportMessage(t('app.settings.introduceUnhaUrl'));
      return;
    }

    setIsImporting(true);
    setImportMessage(t('app.settings.conectandoApisXunta'));

    // As URLs (DOG e Proens) gárdanse decontado — antes mesmo de saber se a extracción ten
    // éxito — para que nunca se perdan: se despois se pecha con "Cancelar" ou a extracción
    // falla, o escrito non desaparece (ver onProgramacionInfoExtraida en App.tsx, que
    // tamén as publica no servidor cando é o profesorado editando a súa propia materia).
    if (dogUrl || proensUrl) onExtracted?.({ dogUrl: dogUrl || undefined, proensUrl: proensUrl || undefined });

    try {
      let newUnitsText = ''; // Start fresh if we are importing
      let newConfig = { ...localConfig };
      let dogSuccess = false;
      let proensSuccess = false;
      // Motivo real do fallo de Proens (p.ex. "non se atopou Chromium"), para amosalo en
      // vez de só o xenérico "non se atoparon datos novos".
      let proensErro = '';

      // 1. IMPORTAR CALENDARIO ESCOLAR (extracción no backend Go, sen proxy)
      let dogWarnings: string[] = [];
      if (dogUrl) {
        setImportMessage(t('app.settings.analizandoCalendario'));
        try {
          const cal = await ImportSchoolCalendar(dogUrl);
          if (cal.startDate) newConfig.startDate = cal.startDate;
          if (cal.endDate) newConfig.endDate = cal.endDate;

          // As filas propias do centro non as fixa o DOG: mantéñense ao reimportar, e
          // só se substitúe o que vén do calendario oficial.
          const own = (newConfig.calendar || []).filter(e => e.type === 'centro' || e.type === 'actividade');
          const official: CalendarEntry[] = (cal.periods || []).map((p, i) => ({
            id: `dog-${i}-${p.start}`,
            type: p.type === 'vacacions' ? 'vacacions' : 'festivo',
            name: p.name,
            start: p.start,
            end: p.end || '',
          }));
          newConfig.calendar = [...official, ...own];
          dogWarnings = cal.warnings || [];
          dogSuccess = !!cal.startDate || !!cal.endDate || official.length > 0;
        } catch (e: any) {
          console.error('Erro calendario:', e);
          setImportMessage(t('app.settings.erroImportarCalendario', { error: e?.message || e }));
          setIsImporting(false);
          return;
        }
      }

      // 2. IMPORTAR PROGRAMACIÓN DE PROENS (extracción no backend Go, sen proxy): as
      // unidades (3.1, para o texto editable) e a información xeral (introdución,
      // obxectivos, criterios, currículo/avaliación por UD — o que amosará
      // ProgramacionPanel) pídense as dúas Á VEZ (Promise.allSettled, independentes entre
      // si) en vez de secuencial — antes Configuración só pedía as unidades e era o panel
      // de programación, nunha acción á parte, quen pedía a información xeral, así que a
      // mesma páxina de Proens acababa cargándose dúas veces en Chromium.
      if (proensUrl) {
        setImportMessage(t('app.settings.extraendoUnidadesProens'));
        const [unitsRes, infoRes] = await Promise.allSettled([
          ImportProens(proensUrl),
          ImportProensInfo(proensUrl),
        ]);

        if (unitsRes.status === 'fulfilled' && unitsRes.value?.units?.length) {
          const result = unitsRes.value;
          newUnitsText = result.units.map(u => `${u.titulo} - ${u.sesions}`).join('\n');
          // Gardamos a descrición como "contidos" para enriquecer as unidades ao gardar.
          setPendingProgrammingData(result.units.map(u => ({
            titulo: u.titulo,
            sesions: u.sesions,
            contidos: u.descricion,
          })));
          proensSuccess = true;
        } else {
          console.error('Erro Proens (unidades):', unitsRes.status === 'rejected' ? unitsRes.reason : 'sen unidades');
          if (unitsRes.status === 'rejected') {
            const r: any = unitsRes.reason;
            proensErro = t('app.settings.erroProens', { error: r?.message || String(r) });
          }
        }

        // A información xeral gárdase (local + servidor) en canto chega, aínda que as
        // unidades fallasen — son extraccións independentes da mesma páxina.
        if (infoRes.status === 'fulfilled' && infoRes.value) {
          onExtracted?.({ proensUrl, programacionInfo: infoRes.value });
        } else if (infoRes.status === 'rejected') {
          console.error('Erro Proens (información xeral):', infoRes.reason);
        }
      }

      // Finalizar importación
      if (dogSuccess || proensSuccess) {
        if (dogUrl) {
          setLocalConfig(newConfig);
        }
        if (proensUrl) {
          // Se se proporcionou URL de Proens, actualizamos o texto (aínda que estea baleiro se fallou)
          setUnitsText(newUnitsText);
        }
        const calMsg = dogSuccess
          ? t('app.settings.calendarioImportado', { count: (newConfig.calendar || []).length })
          : '';
        const warnMsg = dogWarnings.length > 0 ? ` ${dogWarnings.join(' ')}` : '';
        setImportMessage(t('app.settings.importacionCompletada', { calMsg, unidadesMsg: proensUrl ? (proensSuccess ? t('app.settings.unidadesOk') : (proensErro || t('app.settings.nonAtoparonUnidadesUrl'))) : '', warnMsg }));
      } else {
        // Se todo fallou e non había nada, cargamos os datos de exemplo
        if (unitsText.trim() === '') {
          const dummyText = `O proceso tecnolóxico - 10\nDebuxo técnico e deseño - 12\nMateriais de uso técnico - 14\nEstruturas e mecanismos - 16\nElectricidade e electrónica - 15\nHardware e software - 10\nProgramación e robótica - 18\nInternet e seguridade - 8`;
          setUnitsText(dummyText);
          setImportMessage(t('app.settings.nonPuideronExtraerDatos') + (proensErro ? ' ' + proensErro : ''));
        } else {
          setImportMessage(proensErro ? proensErro + ' ' + t('app.settings.mantenseDatosActuais') : t('app.settings.nonAtoparonDatosNovos'));
        }
      }
      
      setIsImporting(false);

    } catch (error) {
      console.error(error);
      setImportMessage(t('app.settings.erroCriticoImportacion'));
      setIsImporting(false);
    }
  };

  const handleImportProgrammingPDF = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    setIsProcessingAI(true);
    setImportMessage(t('app.settings.lendoPdf'));

    try {
      const arrayBuffer = await file.arrayBuffer();
      const pdf = await pdfjs.getDocument({ data: arrayBuffer }).promise;
      let fullText = '';

      for (let i = 1; i <= Math.min(pdf.numPages, 30); i++) { // Aumentamos a 30 páxinas
        const page = await pdf.getPage(i);
        const content = await page.getTextContent();
        const strings = content.items.map((item: any) => item.str);
        fullText += strings.join(' ') + '\n';
      }

      if (fullText.trim().length < 100) {
        throw new Error(t('app.settings.pdfSenTexto'));
      }

      setImportMessage(t('app.settings.analizandoIaGemini'));

      const ai = new GoogleGenAI({ apiKey: process.env.GEMINI_API_KEY });
      const response = await ai.models.generateContent({
        model: "gemini-3-flash-preview",
        contents: `Actúa como un experto en programacións didácticas de Galicia. 
        Analiza o seguinte texto extraído dun PDF e identifica a relación de unidades didácticas.
        
        INSTRUCIÓNS:
        1. Busca títulos que comecen por "Unidade", "UD", "Tema" ou que parezan seccións principais.
        2. Extrae o número de sesións (busca números preto de palabras como "sesións", "horas", "sesions"). Se non o atopas, pon 10 por defecto.
        3. Resume brevemente os obxectivos, contidos e criterios de avaliación se están presentes.
        4. Se o texto está desordenado, tenta reconstruír a estrutura lóxica.
        
        RESPONDE EXCLUSIVAMENTE NUN ARRAY JSON:
        [{"titulo": "Nome", "sesions": 10, "obxectivos": "...", "contidos": "...", "criterios": "..."}]
        
        Texto extraído:
        ${fullText.substring(0, 40000)}`,
        config: {
          responseMimeType: "application/json",
          responseSchema: {
            type: Type.ARRAY,
            items: {
              type: Type.OBJECT,
              properties: {
                titulo: { type: Type.STRING },
                sesions: { type: Type.NUMBER },
                obxectivos: { type: Type.STRING },
                contidos: { type: Type.STRING },
                criterios: { type: Type.STRING }
              },
              required: ["titulo", "sesions"]
            }
          }
        }
      });

      const unitsData = JSON.parse(response.text);
      
      if (Array.isArray(unitsData) && unitsData.length > 0) {
        const formattedText = unitsData.map((u: any) => `${u.titulo} - ${u.sesions}`).join('\n');
        setUnitsText(formattedText);
        setPendingProgrammingData(unitsData);
        setImportMessage(t('app.settings.iaAtopadasUnidades', { count: unitsData.length }));
      } else {
        setImportMessage(t('app.settings.iaNonIdentificouUnidades'));
      }
    } catch (err) {
      console.error(err);
      setImportMessage(t('app.settings.erroProcesarPdfIa'));
    } finally {
      setIsProcessingAI(false);
    }
  };

  const handleImportProgrammingJSON = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (event) => {
      try {
        const imported = JSON.parse(event.target?.result as string);
        
        // Adaptar á estrutura específica do usuario: programacion_didactica.unidades_didacticas
        const unitsData = imported.programacion_didactica?.unidades_didacticas || 
                          imported.unidades_didacticas || 
                          imported.unidades || 
                          (Array.isArray(imported) ? imported : null);
        
        if (Array.isArray(unitsData)) {
          const formattedText = unitsData.map((u: any) => `${u.titulo || u.title || u.nome} - ${u.sesions || u.sessions || 10}`).join('\n');
          setUnitsText(formattedText);
          
          // Mapear campos específicos do JSON do usuario aos campos da nosa App
          const mappedData = unitsData.map((u: any) => ({
            titulo: u.titulo || u.title || u.nome,
            sesions: u.sesions || u.sessions || 10,
            contidos: Array.isArray(u.contidos) ? u.contidos.join(', ') : u.contidos,
            criterios: Array.isArray(u.criterios_detallados) 
              ? u.criterios_detallados.map((c: any) => `${c.codigo}: ${c.descricion}`).join('; ') 
              : u.criterios
          }));
          
          setPendingProgrammingData(mappedData);
          setImportMessage(t('app.settings.jsonCargadasUnidades', { count: unitsData.length }));
        } else {
          setImportMessage(t('app.settings.jsonFormatoNonRecoñecible'));
        }
      } catch (err) {
        setImportMessage(t('app.settings.erroLerJsonProgramacion'));
      }
    };
    reader.readAsText(file);
  };

  const handleImportCalendarJSON = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (event) => {
      try {
        const imported = JSON.parse(event.target?.result as string);
        const newConfig: AppConfig = { ...localConfig };
        let count = 0;
        const newEntries: CalendarEntry[] = [];

        // Detect if it's the new format (has a root key with calendario...)
        const rootKeys = Object.keys(imported);
        let data = imported;
        if (rootKeys.length === 1 && typeof imported[rootKeys[0]] === 'object' && imported[rootKeys[0]].inicio_actividades_lectivas) {
          data = imported[rootKeys[0]];
        } else if (imported.inicio_actividades_lectivas) {
          data = imported;
        }

        // Parse old format
        if (imported.startDate) {
          newConfig.startDate = imported.startDate;
          count++;
        }
        if (imported.endDate) {
          newConfig.endDate = imported.endDate;
          count++;
        }
        if (Array.isArray(imported.scheduleDays)) {
          newConfig.scheduleDays = imported.scheduleDays;
          count++;
        }
        if (Array.isArray(imported.holidays)) {
          imported.holidays.forEach((h: any) => {
            if (h?.date) newEntries.push({ id: newEntryId(), type: 'festivo', name: h.name || t('app.settings.diaNonLectivo'), start: h.date });
          });
        }

        // Parse new format
        if (data.inicio_actividades_lectivas?.xeral) {
          newConfig.startDate = data.inicio_actividades_lectivas.xeral;
          count++;
        }
        if (data.fin_actividades_lectivas?.xeral) {
          newConfig.endDate = data.fin_actividades_lectivas.xeral;
          count++;
        }
        
        // Un período vacacional convértese nunha soa fila da táboa: chega co rango, non
        // fai falta despregalo en días.
        if (Array.isArray(data.vacacions)) {
          data.vacacions.forEach((v: any) => {
            const name = v.periodo || t('app.settings.vacacions');
            if (v.desde && v.ata) {
              newEntries.push({ id: newEntryId(), type: 'vacacions', name, start: v.desde, end: v.ata });
            } else if (Array.isArray(v.dias)) {
              v.dias.forEach((d: string) => {
                newEntries.push({ id: newEntryId(), type: 'vacacions', name, start: d });
              });
            }
          });
        }

        if (Array.isArray(data.dias_non_lectivos_especificos)) {
          data.dias_non_lectivos_especificos.forEach((d: any) => {
            if (d.data) {
              newEntries.push({
                id: newEntryId(),
                type: 'festivo',
                name: d.descripcion || t('app.settings.diaNonLectivo'),
                start: d.data,
              });
            }
          });
        }

        if (newEntries.length > 0) {
          // Como na importación por URL, o que engadiu o centro consérvase e só se
          // substitúe o que vén do calendario oficial.
          const own = (localConfig.calendar || []).filter(e => e.type === 'centro' || e.type === 'actividade');
          newConfig.calendar = [...newEntries, ...own];
          count += newEntries.length;
        }

        setLocalConfig(newConfig);
        setImportMessage(t('app.settings.calendarioImportadosElementos', { count }));
      } catch (err) {
        setImportMessage(t('app.settings.erroLerJsonCalendario'));
      }
    };
    reader.readAsText(file);
  };

  const handleSave = () => {
    // Parse units text
    const lines = unitsText.split('\n').filter(l => l.trim() !== '');
    const newUnits: Unit[] = lines.map((line, index) => {
      const parts = line.split('-');
      const title = parts[0]?.trim() || t('app.settings.unidadeNumero', { number: index + 1 });
      const sessions = parseInt(parts[1]?.trim() || '10', 10);
      
      const colors = [
        { color: 'bg-blue-500', bgLight: 'bg-blue-100', text: 'text-blue-500' },
        { color: 'bg-emerald-500', bgLight: 'bg-emerald-100', text: 'text-emerald-500' },
        { color: 'bg-amber-500', bgLight: 'bg-amber-100', text: 'text-amber-500' },
        { color: 'bg-purple-500', bgLight: 'bg-purple-100', text: 'text-purple-500' },
        { color: 'bg-rose-500', bgLight: 'bg-rose-100', text: 'text-rose-500' },
        { color: 'bg-cyan-500', bgLight: 'bg-cyan-100', text: 'text-cyan-500' },
        { color: 'bg-indigo-500', bgLight: 'bg-indigo-100', text: 'text-indigo-500' },
        { color: 'bg-orange-500', bgLight: 'bg-orange-100', text: 'text-orange-500' },
        { color: 'bg-teal-500', bgLight: 'bg-teal-100', text: 'text-teal-500' },
        { color: 'bg-pink-500', bgLight: 'bg-pink-100', text: 'text-pink-500' },
      ];

      // Try to find matching pending data for metadata
      const pending = pendingProgrammingData?.find((p: any) => (p.titulo || p.title || p.nome) === title) || 
                      pendingProgrammingData?.[index];
      
      return {
        id: index + 1,
        title,
        sessions: isNaN(sessions) ? 10 : sessions,
        ...colors[index % colors.length],
        obxectivos: pending?.obxectivos || pending?.objectives,
        contidos: pending?.contidos || pending?.contents,
        criterios: pending?.criterios || pending?.criteria,
        competencias: pending?.competencias || pending?.competencies,
      };
    });

    const parsedGroups = groupsText.split(',').map(g => g.trim()).filter(Boolean);
    // As listas antigas de días soltos xa foron migradas á táboa ao abrir; báldanse ao
    // gardar para que non quede unha segunda copia dos mesmos días na materia.
    const saved: AppConfig = {
      ...localConfig,
      calendar: localConfig.calendar || [],
      holidays: [],
      customHolidays: [],
      proensUrl: proensUrl.trim() || undefined,
      dogUrl: dogUrl.trim() || undefined,
    };
    onSave(saved, newUnits, showGroups ? parsedGroups : undefined);
  };

  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-3xl overflow-hidden flex flex-col max-h-[90vh]">
        <div className="p-4 border-b border-stone-200 flex justify-between items-center bg-stone-50">
          <h3 className="font-bold text-lg text-stone-900">{canEditProgramming ? t('app.settings.configuracionMateria') : t('app.settings.contaECertificado')}</h3>
          <button onClick={onClose} className="p-2 text-stone-400 hover:text-stone-600 rounded-lg hover:bg-stone-200 transition-colors">
            <X className="w-5 h-5" />
          </button>
        </div>
        
        <div className="p-6 flex flex-col gap-6 overflow-y-auto">
          
          {canEditProgramming && (<>
          {showGroups && (
            <div className="flex flex-col gap-2">
              <h4 className="font-bold text-stone-900 border-b border-stone-200 pb-2">{t('app.settings.gruposImparte')}</h4>
              <p className="text-sm text-stone-500">{t('app.settings.gruposSepararComas')}</p>
              <input
                type="text"
                value={groupsText}
                onChange={(e) => setGroupsText(e.target.value)}
                placeholder="eso4a, eso4b"
                className="w-full p-2.5 bg-white border border-stone-300 rounded-lg shadow-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none"
              />
            </div>
          )}
          <div className="flex flex-col gap-4">
            <h4 className="font-bold text-stone-900 border-b border-stone-200 pb-2">{t('app.settings.importacionAutomatica')}</h4>
            <div className="flex flex-col gap-1.5">
              <label className="text-sm font-bold text-stone-700">{t('app.settings.urlDog')}</label>
              <input
                type="url" 
                value={dogUrl}
                onChange={(e) => setDogUrl(e.target.value)}
                placeholder="Ex: https://www.xunta.gal/dog/Publicados/..."
                className="w-full p-2.5 bg-white border border-stone-300 rounded-lg shadow-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none"
              />
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="flex flex-col gap-1.5">
                <label className="text-sm font-bold text-stone-700">{t('app.settings.programacionPdfIa')}</label>
                <label className={`flex items-center justify-center gap-2 px-4 py-2.5 bg-white border border-stone-300 border-dashed rounded-lg transition-colors text-stone-600 font-medium ${isProcessingAI ? 'opacity-50 cursor-not-allowed' : 'hover:bg-stone-50 cursor-pointer'}`}>
                  {isProcessingAI ? (
                    <Loader2 className="w-4 h-4 animate-spin" />
                  ) : (
                    <FileUp className="w-4 h-4" />
                  )}
                  <span className="text-xs">{isProcessingAI ? t('app.settings.procesando') : t('app.settings.subirPdf')}</span>
                  <input
                    type="file"
                    accept=".pdf"
                    onChange={handleImportProgrammingPDF}
                    disabled={isProcessingAI}
                    className="hidden"
                  />
                </label>
              </div>

              <div className="flex flex-col gap-1.5">
                <label className="text-sm font-bold text-stone-700">{t('app.settings.programacionJson')}</label>
                <label className="flex items-center justify-center gap-2 px-4 py-2.5 bg-white border border-stone-300 border-dashed rounded-lg hover:bg-stone-50 cursor-pointer transition-colors text-stone-600 font-medium">
                  <Upload className="w-4 h-4" />
                  <span className="text-xs">{t('app.settings.subirJson')}</span>
                  <input
                    type="file"
                    accept=".json"
                    onChange={handleImportProgrammingJSON}
                    className="hidden"
                  />
                </label>
              </div>
            </div>
            <p className="text-[10px] text-stone-500 italic">{t('app.settings.podesUsarIa')}</p>

            <div className="flex flex-col gap-1.5">
              <label className="text-sm font-bold text-stone-700">{t('app.settings.urlProens')}</label>
              <input
                type="url" 
                value={proensUrl}
                onChange={(e) => setProensUrl(e.target.value)}
                placeholder="Ex: https://www.edu.xunta.gal/proens/..."
                className="w-full p-2.5 bg-white border border-stone-300 rounded-lg shadow-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none"
              />
            </div>
            
            <button 
              onClick={handleImport}
              disabled={isImporting || (!dogUrl && !proensUrl)}
              className="mt-2 px-4 py-2.5 bg-stone-900 text-white font-medium hover:bg-stone-800 rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex justify-center items-center w-max"
            >
              {isImporting ? t('app.settings.importando') : t('app.settings.extraerDatosUrls')}
            </button>

            {importMessage && (
              <div className="p-3 bg-amber-50 text-amber-800 border border-amber-200 rounded-lg text-sm">
                {importMessage}
              </div>
            )}
          </div>

          <div className="flex flex-col gap-4">
            <h4 className="font-bold text-stone-900 border-b border-stone-200 pb-2">{t('app.settings.unidadesDidacticasManual')}</h4>
            <p className="text-sm text-stone-500">{t('app.settings.podesPegarUnidades')}</p>
            <textarea
              value={unitsText}
              onChange={(e) => setUnitsText(e.target.value)}
              placeholder="Técnicas de representación gráfica - 24&#10;O método de proxectos - 15"
              className="w-full p-3 bg-white border border-stone-300 rounded-lg shadow-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none min-h-[150px] font-mono text-sm"
            ></textarea>
          </div>

          <div className="flex flex-col gap-4">
            <div className="flex justify-between items-center border-b border-stone-200 pb-2">
              <h4 className="font-bold text-stone-900">{t('app.settings.calendarioEscolar')}</h4>
              <label className="flex items-center gap-2 px-3 py-1 bg-stone-100 hover:bg-stone-200 text-stone-700 rounded-lg cursor-pointer transition-colors text-xs font-medium border border-stone-200">
                <FileJson className="w-3.5 h-3.5" />
                <span>{t('app.settings.importarCalendarioJson')}</span>
                <input
                  type="file"
                  accept=".json"
                  onChange={handleImportCalendarJSON}
                  className="hidden"
                />
              </label>
            </div>
            <p className="text-sm text-stone-500">
              {t('app.settings.periodosVacacionaisTexto')}
            </p>
            <CalendarTable
              startDate={localConfig.startDate}
              endDate={localConfig.endDate}
              entries={localConfig.calendar || []}
              onStartDateChange={(d) => setLocalConfig(prev => ({ ...prev, startDate: d }))}
              onEndDateChange={(d) => setLocalConfig(prev => ({ ...prev, endDate: d }))}
              onEntriesChange={(entries) => setLocalConfig(prev => ({ ...prev, calendar: entries }))}
            />
          </div>

          <div className="flex flex-col gap-4">
            <h4 className="font-bold text-stone-900 border-b border-stone-200 pb-2">{t('app.settings.diasDeClase')}</h4>
            <div className="flex flex-wrap gap-4">
              {daysOfWeek.map(day => (
                <label key={day.id} className="flex items-center gap-2 cursor-pointer">
                  <input 
                    type="checkbox" 
                    checked={localConfig.scheduleDays.includes(day.id)}
                    onChange={() => toggleDay(day.id)}
                    className="w-4 h-4 text-emerald-600 rounded border-stone-300 focus:ring-emerald-500"
                  />
                  <span className="text-sm font-medium text-stone-700">{day.label}</span>
                </label>
              ))}
            </div>
            {localConfig.scheduleDays.length === 0 && (
              <p className="text-xs text-rose-600">{t('app.settings.debesSeleccionarDia')}</p>
            )}
          </div>


          <div className="flex flex-col gap-4">
            <h4 className="font-bold text-stone-900 border-b border-stone-200 pb-2">{t('app.settings.copiaDeSeguridade')}</h4>
            <div className="flex flex-wrap gap-3">
              <button
                onClick={handleExportBackup}
                className="flex items-center gap-2 px-4 py-2 bg-stone-100 hover:bg-stone-200 text-stone-700 rounded-lg font-medium transition-colors text-sm"
              >
                <Download className="w-4 h-4" />
                {t('app.settings.exportarDatos')}
              </button>
              
              <label className="flex items-center gap-2 px-4 py-2 bg-stone-100 hover:bg-stone-200 text-stone-700 rounded-lg font-medium transition-colors text-sm cursor-pointer">
                <Upload className="w-4 h-4" />
                {t('app.settings.importarDatos')}
                <input
                  type="file"
                  accept=".json"
                  onChange={handleImportBackup}
                  className="hidden"
                />
              </label>
            </div>
            {backupMessage && (
              <div className={`p-3 rounded-lg text-sm ${backupError ? 'bg-rose-50 text-rose-800 border border-rose-200' : 'bg-emerald-50 text-emerald-800 border border-emerald-200'}`}>
                {backupMessage}
              </div>
            )}
          </div>
          </>)}

          {profile?.connected && (
            <div className="flex flex-col gap-4">
              <h4 className="font-bold text-stone-900 border-b border-stone-200 pb-2 flex items-center gap-2">
                <Lock className="w-4 h-4" />
                {t('app.settings.contaUsuarioCertificado')}
              </h4>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-bold text-stone-700">{t('app.settings.nome')}</label>
                  <input
                    type="text"
                    value={certName}
                    onChange={e => setCertName(e.target.value)}
                    placeholder="Ex: Martín"
                    className="p-2 border border-stone-300 rounded-lg text-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none"
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-bold text-stone-700">{t('app.settings.apelidos')}</label>
                  <input
                    type="text"
                    value={certSurname}
                    onChange={e => setCertSurname(e.target.value)}
                    placeholder="Ex: Pardiñas Fondo"
                    className="p-2 border border-stone-300 rounded-lg text-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none"
                  />
                </div>
              </div>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-bold text-stone-700">{t('app.settings.novoContrasinal')}</label>
                  <PasswordInput
                    value={newPassword}
                    onChange={e => setNewPassword(e.target.value)}
                    autoComplete="new-password"
                    className="p-2 border border-stone-300 rounded-lg text-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none"
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <label className="text-xs font-bold text-stone-700">{t('app.settings.repiteONovo')}</label>
                  <PasswordInput
                    value={confirmPassword}
                    onChange={e => setConfirmPassword(e.target.value)}
                    autoComplete="new-password"
                    className="p-2 border border-stone-300 rounded-lg text-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none"
                  />
                </div>
              </div>
              {/* Un único botón: cambiar o contrasinal rexenera sempre o certificado co
                  novo, que é o que despois abre a sinatura dos informes. */}
              <button
                onClick={handleChangePassword}
                disabled={isChangingPassword || !newPassword || !confirmPassword}
                className="flex items-center gap-2 px-4 py-2 bg-stone-900 text-white font-medium hover:bg-stone-800 rounded-lg transition-colors text-sm disabled:opacity-50 disabled:cursor-not-allowed w-max"
              >
                {isChangingPassword ? <Loader2 className="w-4 h-4 animate-spin" /> : <Lock className="w-4 h-4" />}
                {isChangingPassword ? t('app.settings.procesando') : t('app.settings.cambiarContrasinalCertificado')}
              </button>
              {passwordMessage && (
                <div className={`p-3 rounded-lg text-sm ${passwordError ? 'bg-rose-50 text-rose-800 border border-rose-200' : 'bg-emerald-50 text-emerald-800 border border-emerald-200'}`}>
                  {passwordMessage}
                </div>
              )}
            </div>
          )}

        </div>

        <div className="p-4 border-t border-stone-200 bg-stone-50 flex justify-end gap-2">
          <button onClick={onClose} className="px-4 py-2 text-stone-600 font-medium hover:bg-stone-200 rounded-lg transition-colors">
            {canEditProgramming ? t('app.cancelar') : t('app.pechar')}
          </button>
          {canEditProgramming && (
            <button
              onClick={handleSave}
              disabled={localConfig.scheduleDays.length === 0}
              className="px-4 py-2 bg-stone-900 text-white font-medium hover:bg-stone-800 rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {t('app.settings.aplicarCambios')}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

function ReportsModal({ subject, months, onClose, onSaveReport, onSaveMonthlyReport, onOpenSignedReport }: {
  subject: Subject,
  months: any[],
  onClose: () => void,
  onSaveReport: (unitId: number, report: UnitReport) => void,
  onSaveMonthlyReport: (monthKey: string, report: string) => void,
  // onOpenSignedReport abre o xerador de informe asinado (ReportBuilder) en vez
  // dunha pestana máis: precisa ver o calendario xeral detrás para escoller
  // data (ver comentario "Sen veo escuro" en ReportBuilder.tsx), así que non
  // pode vivir DENTRO deste modal (que ten fondo opaco) — pecha este e ábreo
  // el, mesmo criterio có antigo botón do menú principal que substitúe.
  onOpenSignedReport: () => void
}) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<'monthly' | 'comparative' | 'unit'>('comparative');
  const [selectedUnitId, setSelectedUnitId] = useState<number>(subject.units[0]?.id || 1);

  // Xeración de informes con IA: petición EXPRESA a Malta (tao/modulos/malta).
  // Tao empaqueta a materia completa e lanza Malta xa coa tarefa (sen abrir a
  // súa interface en ningún momento); Malta procesa de forma autónoma e
  // devolve o resultado por evento Wails (tao:malta-informe, ver
  // tao/malta_resultado.go) — nunca por polling. subjectRef evita pechar
  // sobre un `subject` xa desactualizado cando o evento chega (pode tardar
  // ss/decenas de ss en chegar, e o profesorado pode ter editado algo
  // manualmente mentres tanto).
  const subjectRef = useRef(subject);
  subjectRef.current = subject;

  const [maltaDispoñible, setMaltaDispoñible] = useState(false);
  const [maltaIndice, setMaltaIndice] = useState<any[]>([]); // MaltaIndiceEntrada[] — todos os informes desta materia
  const [maltaInformes, setMaltaInformes] = useState<Record<string, any>>({}); // reportId -> informe xa cargado/recibido
  const [maltaXerandoUnidadeId, setMaltaXerandoUnidadeId] = useState<number | null>(null);
  const [maltaXerandoMes, setMaltaXerandoMes] = useState<string | null>(null); // monthKey en curso
  const [maltaErro, setMaltaErro] = useState('');
  const [peticionUnidade, setPeticionUnidade] = useState('');
  const [peticionMes, setPeticionMes] = useState('');
  // selectedMonthKey escolle explicitamente o mes obxecto do informe
  // mensual — antes a pestana só listaba (en automático) os meses que xa
  // tiñan rexistros diarios con texto, así que unha materia sen ese tipo de
  // anotacións amosaba a pestana baleira, sen selector nin botón ningún.
  const [selectedMonthKey, setSelectedMonthKey] = useState<string>(() => {
    const hoxe = new Date();
    return `${hoxe.getFullYear()}-${String(hoxe.getMonth() + 1).padStart(2, '0')}`;
  });
  const [showImprimirPanel, setShowImprimirPanel] = useState(false);

  useEffect(() => {
    let cancelado = false;
    (async () => {
      try {
        const dispoñible = await MaltaDispoñible();
        if (cancelado) return;
        setMaltaDispoñible(dispoñible);
        if (dispoñible) {
          const indice = await ListMaltaReports(subject.id);
          if (!cancelado) setMaltaIndice(indice || []);
        }
      } catch {
        // Malta é opcional: calquera fallo aquí só significa "sen informes IA
        // dispoñibles", nunca debe romper o resto do modal de informes.
      }
    })();
    return () => { cancelado = true; };
  }, [subject.id]);

  // Escoita o resultado que Malta devolve a Tao (POST /api/v1/malta/resultado,
  // tao/malta_resultado.go): actualiza o índice/caché local e, se é un
  // informe de UNIDADE, escribe directamente en unitReports (graoCumprimento/
  // propostas/observacions) — mesmo camiño (onSaveReport) que unha edición
  // manual, así que segue respectando isLocked e o resto do fluxo existente.
  useEffect(() => {
    const off = Events.On('tao:malta-informe', (e) => {
      const payload = e.data;
      if (!payload || payload.materiaId !== subject.id) return;
      if (!payload.ok) {
        setMaltaErro(payload.erro || t('app.reports.erroXerarInformeIa'));
        setMaltaXerandoUnidadeId(null);
        setMaltaXerandoMes(null);
        return;
      }
      setMaltaErro('');
      setMaltaInformes(prev => ({ ...prev, [payload.reportId]: payload }));
      setMaltaIndice(prev => {
        const outros = prev.filter((e: any) => e.reportId !== payload.reportId);
        return [{
          reportId: payload.reportId, tipo: payload.tipo,
          rangoFrom: payload.rangoFrom, rangoTo: payload.rangoTo,
          unitIdObxectivo: payload.unitIdObxectivo,
          unidades: (payload.unidades || []).map((u: any) => u.unitId),
          xeradoEn: new Date().toISOString(),
        }, ...outros];
      });

      if (payload.tipo === 'unidade') {
        setMaltaXerandoUnidadeId(null);
        const u = (payload.unidades || [])[0];
        const cur = subjectRef.current;
        if (u && cur && !cur.isLocked) {
          const existente = cur.unitReports[payload.unitIdObxectivo] || {
            dataAlta: new Date().toLocaleDateString('gl-ES'), dataUltimaModificacion: '',
            graoCumprimento: '', propostas: '', observacions: ''
          };
          onSaveReport(payload.unitIdObxectivo, {
            ...existente,
            dataUltimaModificacion: new Date().toLocaleDateString('gl-ES'),
            graoCumprimento: u.grauConsecucion || existente.graoCumprimento,
            propostas: u.proposta || existente.propostas,
            observacions: u.resumo || existente.observacions,
          });
        }
      } else {
        setMaltaXerandoMes(null);
      }
    });
    return () => { off(); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subject.id]);

  const xerarInformeUnidade = async () => {
    setMaltaErro('');
    setMaltaXerandoUnidadeId(selectedUnitId);
    try {
      await AbrirMaltaUnidade(subject.id, selectedUnitId, peticionUnidade.trim());
    } catch (e: any) {
      setMaltaErro(e?.message || String(e));
      setMaltaXerandoUnidadeId(null);
    }
  };

  // monthKeyOf/monthBounds — helpers compartidos por selector, xeración e
  // busca no índice, para non repetir a mesma conta en tres sitios.
  const monthKeyOf = (month: any) => `${month.year}-${String(month.month + 1).padStart(2, '0')}`;
  const monthBounds = (month: any) => {
    const key = monthKeyOf(month);
    const ultimoDia = new Date(month.year, month.month + 1, 0).getDate();
    return { key, monthStart: `${key}-01`, monthEnd: `${key}-${String(ultimoDia).padStart(2, '0')}` };
  };

  // rangoEfectivoAta: un mes aínda en curso só conta ata hoxe — as sesións
  // posteriores están programadas pero non se empregaron aínda.
  const rangoEfectivoAta = (monthEnd: string) => {
    const hoxe = new Date();
    const hoxeStr = `${hoxe.getFullYear()}-${String(hoxe.getMonth() + 1).padStart(2, '0')}-${String(hoxe.getDate()).padStart(2, '0')}`;
    return hoxeStr < monthEnd ? hoxeStr : monthEnd;
  };

  // unidadesTraballadasNoMes: para cada unidade con polo menos unha sesión
  // no mes (ata hoxe, se está en curso), as sesións dadas no mes, as
  // acumuladas desde o inicio do curso ata ese momento e a porcentaxe da
  // unidade que supoñen. Mesmo reparto de sesións cá grella (day.unit, que
  // xa salta folgas/excursións/ausencias e conta os exames). Espello do
  // cálculo de Malta (tao/modulos/malta/materias.go: calcularMetricas).
  const unidadesTraballadasNoMes = (monthStart: string, monthEnd: string) => {
    const ata = rangoEfectivoAta(monthEnd);
    const porUnidade = new Map<number, { unit: Unit, sesionsMes: number, sesionsAcumuladas: number, sesions: any[] }>();
    months.forEach((month: any) => {
      month.days.forEach((day: any) => {
        if (!day.unit || day.dateString > ata) return;
        let e = porUnidade.get(day.unit.id);
        if (!e) {
          e = { unit: day.unit, sesionsMes: 0, sesionsAcumuladas: 0, sesions: [] };
          porUnidade.set(day.unit.id, e);
        }
        e.sesionsAcumuladas++;
        if (day.dateString >= monthStart) {
          e.sesionsMes++;
          e.sesions.push({ ...day, log: subject.logs[day.dateString] });
        }
      });
    });
    return subject.units
      .map(u => porUnidade.get(u.id))
      .filter((e): e is NonNullable<typeof e> => !!e && e.sesionsMes > 0)
      .map(e => ({
        ...e,
        porcentaxe: e.unit.sessions > 0 ? Math.min(100, Math.round(e.sesionsAcumuladas * 100 / e.unit.sessions)) : 0,
      }));
  };

  const xerarInformeMensual = async (month: any) => {
    const { key, monthStart, monthEnd } = monthBounds(month);
    setMaltaErro('');
    setMaltaXerandoMes(key);
    try {
      await AbrirMaltaMensual(subject.id, monthStart, rangoEfectivoAta(monthEnd), peticionMes.trim());
    } catch (e: any) {
      setMaltaErro(e?.message || String(e));
      setMaltaXerandoMes(null);
    }
  };

  // maltaReportParaMes devolve a entrada do índice (se existe) cuxo rango
  // cobre este mes.
  const maltaReportParaMes = (month: any) => {
    const { monthStart, monthEnd } = monthBounds(month);
    return maltaIndice.find((e: any) => e.tipo === 'mensual' && e.rangoFrom <= monthEnd && e.rangoTo >= monthStart) || null;
  };

  const cargarInformeMalta = async (reportId: string) => {
    if (maltaInformes[reportId]) return maltaInformes[reportId];
    try {
      const informe = await GetMaltaReport(subject.id, reportId);
      setMaltaInformes(prev => ({ ...prev, [reportId]: informe }));
      return informe;
    } catch (e: any) {
      setMaltaErro(e?.message || String(e));
      return null;
    }
  };

  const exportarInformeMalta = async (reportId: string, formato: 'pdf' | 'odt' | 'docx') => {
    setMaltaErro('');
    try {
      const exportar = formato === 'pdf' ? ExportMaltaReportPDF : formato === 'odt' ? ExportMaltaReportODT : ExportMaltaReportDOCX;
      await exportar(subject.id, reportId);
    } catch (e: any) {
      setMaltaErro(e?.message || String(e));
    }
  };

  // Calculate comparative data
  const comparativeData = subject.units.map(unit => {
    let firstDay = null;
    let lastDay = null;
    let actualSessions = 0;

    months.forEach(month => {
      month.days.forEach((day: any) => {
        if (day.unit?.id === unit.id) {
          const log = subject.logs[day.dateString];
          const isException = log && (log.status === 'folga' || log.status === 'excursion' || log.status === 'ausencia');
          
          if (!isException) {
            if (!firstDay) firstDay = day.dateString;
            lastDay = day.dateString;
            actualSessions++;
          }
        }
      });
    });

    return {
      ...unit,
      firstDay,
      lastDay,
      actualSessions
    };
  });

  const selectedUnitData = comparativeData.find(u => u.id === selectedUnitId);
  const currentReport = subject.unitReports[selectedUnitId] || {
    dataAlta: new Date().toLocaleDateString('gl-ES'),
    dataUltimaModificacion: new Date().toLocaleDateString('gl-ES'),
    graoCumprimento: '',
    propostas: '',
    observacions: ''
  };

  const handleReportChange = (field: keyof UnitReport, value: string) => {
    onSaveReport(selectedUnitId, {
      ...currentReport,
      dataUltimaModificacion: new Date().toLocaleDateString('gl-ES'),
      [field]: value
    });
  };

  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4 print:relative print:bg-transparent print:p-0 print:block">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-5xl overflow-hidden flex flex-col max-h-[90vh] print:shadow-none print:max-h-none print:rounded-none">
        <div className="p-4 border-b border-stone-200 flex justify-between items-center bg-stone-50 print:hidden">
          <h3 className="font-bold text-lg text-stone-900">{t('app.reports.informesDeSeguimento')}</h3>
          <div className="flex items-center gap-2">
            <div className="relative print:hidden">
              <button
                onClick={() => setShowImprimirPanel(v => !v)}
                className="flex items-center gap-2 px-3 py-1.5 bg-stone-200 hover:bg-stone-300 text-stone-700 rounded-lg font-medium transition-colors text-sm"
              >
                <Printer className="w-4 h-4" />
                {t('app.reports.imprimirInforme')}
              </button>
              {showImprimirPanel && (
                <div className="absolute right-0 mt-2 w-80 bg-white border border-stone-200 rounded-xl shadow-lg z-10 p-3 max-h-96 overflow-y-auto">
                  {maltaIndice.length === 0 ? (
                    <p className="text-sm text-stone-400 text-center py-4">{t('app.reports.senInformesXerados')}</p>
                  ) : (
                    <ul className="flex flex-col gap-2">
                      {maltaIndice.map((e: any) => (
                        <li key={e.reportId} className="border border-stone-100 rounded-lg p-2">
                          <div className="text-xs font-bold text-stone-700 mb-1.5">
                            {e.tipo === 'unidade'
                              ? `UD ${e.unitIdObxectivo}`
                              : `${e.rangoFrom} → ${e.rangoTo}`}
                            <span className="font-normal text-stone-400"> · {new Date(e.xeradoEn).toLocaleDateString('gl-ES')}</span>
                          </div>
                          <div className="flex gap-1.5">
                            <button onClick={() => exportarInformeMalta(e.reportId, 'pdf')} className="px-2 py-1 bg-stone-100 hover:bg-stone-200 rounded text-[10px] font-bold">{t('app.reports.exportarPDF')}</button>
                            <button onClick={() => exportarInformeMalta(e.reportId, 'odt')} className="px-2 py-1 bg-stone-100 hover:bg-stone-200 rounded text-[10px] font-bold">{t('app.reports.exportarODT')}</button>
                            <button onClick={() => exportarInformeMalta(e.reportId, 'docx')} className="px-2 py-1 bg-stone-100 hover:bg-stone-200 rounded text-[10px] font-bold">{t('app.reports.exportarDOCX')}</button>
                          </div>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              )}
            </div>
            <button onClick={onClose} className="p-2 text-stone-400 hover:text-stone-600 rounded-lg hover:bg-stone-200 transition-colors print:hidden">
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        <div className="flex border-b border-stone-200 bg-stone-50 px-4 print:hidden overflow-x-auto">
          <button
            onClick={() => setTab('comparative')}
            className={`px-4 py-3 font-medium text-sm border-b-2 transition-colors whitespace-nowrap ${tab === 'comparative' ? 'border-stone-900 text-stone-900' : 'border-transparent text-stone-500 hover:text-stone-700'}`}
          >
            {t('app.reports.informeComparado')}
          </button>
          <button
            onClick={() => setTab('unit')}
            className={`px-4 py-3 font-medium text-sm border-b-2 transition-colors whitespace-nowrap ${tab === 'unit' ? 'border-stone-900 text-stone-900' : 'border-transparent text-stone-500 hover:text-stone-700'}`}
          >
            {t('app.reports.seguimentoPorUnidade')}
          </button>
          <button
            onClick={() => setTab('monthly')}
            className={`px-4 py-3 font-medium text-sm border-b-2 transition-colors whitespace-nowrap ${tab === 'monthly' ? 'border-stone-900 text-stone-900' : 'border-transparent text-stone-500 hover:text-stone-700'}`}
          >
            {t('app.reports.rexistroMensual')}
          </button>
          {/* Non é unha pestana máis (non ten contido propio aquí, ver
              onOpenSignedReport): pecha este modal e abre o ReportBuilder. */}
          <button
            onClick={onOpenSignedReport}
            className="flex items-center gap-1.5 px-4 py-3 font-medium text-sm border-b-2 border-transparent text-stone-500 hover:text-stone-700 transition-colors whitespace-nowrap"
          >
            <FileSignature className="w-3.5 h-3.5" />
            {t('app.reports.informeAsinado')}
          </button>
        </div>

        <div className="hidden print:block p-6 border-b border-stone-200">
          <h1 className="text-2xl font-bold tracking-tight text-stone-900">
            {tab === 'comparative' ? t('app.reports.informeComparadoTitulo') :
             tab === 'unit' ? t('app.reports.informacionSeguimentoUnidade') :
             t('app.reports.rexistroMensualDeAula')}
          </h1>
          <p className="text-stone-500 font-medium">{subject.name} • {t('app.reports.curso')} {subject.academicYear}</p>
        </div>

        <div className="p-6 overflow-y-auto flex-1 bg-stone-50/50 print:overflow-visible print:bg-white print:p-0 print:pt-6">
          {tab === 'comparative' && (
            <div className="bg-white border border-stone-200 rounded-xl overflow-hidden shadow-sm print:border-none print:shadow-none">
              <table className="w-full text-left text-sm">
                <thead className="bg-stone-100 text-stone-600 font-bold uppercase tracking-wider text-xs">
                  <tr>
                    <th className="p-4 border-b border-stone-200">{t('app.reports.unidadeDidactica')}</th>
                    <th className="p-4 border-b border-stone-200 text-center">{t('app.reports.sesionsProg')}</th>
                    <th className="p-4 border-b border-stone-200 text-center">{t('app.reports.sesionsReais')}</th>
                    <th className="p-4 border-b border-stone-200">{t('app.reports.dataInicio')}</th>
                    <th className="p-4 border-b border-stone-200">{t('app.reports.dataFin')}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-stone-100">
                  {comparativeData.map(data => (
                    <tr key={data.id} className="hover:bg-stone-50">
                      <td className="p-4">
                        <div className="flex items-center gap-2">
                          <div className={`w-3 h-3 rounded-full ${data.color}`}></div>
                          <span className="font-medium text-stone-900">UD {data.id}: {data.title}</span>
                        </div>
                      </td>
                      <td className="p-4 text-center font-medium text-stone-600">{data.sessions}</td>
                      <td className="p-4 text-center font-bold text-stone-900">
                        <span className={data.actualSessions > data.sessions ? 'text-rose-600' : data.actualSessions < data.sessions ? 'text-amber-600' : 'text-emerald-600'}>
                          {data.actualSessions}
                        </span>
                      </td>
                      <td className="p-4 text-stone-600">{data.firstDay ? new Date(data.firstDay).toLocaleDateString('gl-ES') : '-'}</td>
                      <td className="p-4 text-stone-600">{data.lastDay ? new Date(data.lastDay).toLocaleDateString('gl-ES') : '-'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {tab === 'unit' && selectedUnitData && (
            <div className="flex flex-col gap-6">
              <div className="print:hidden">
                <label className="text-sm font-bold text-stone-700 mb-1 block">{t('app.reports.unidadeDidacticaLabel')}</label>
                <select 
                  value={selectedUnitId}
                  onChange={(e) => setSelectedUnitId(Number(e.target.value))}
                  className="w-full max-w-md p-2.5 bg-white border border-stone-300 rounded-lg shadow-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none"
                >
                  {subject.units.map(u => (
                    <option key={u.id} value={u.id}>UD{u.id} {u.title}</option>
                  ))}
                </select>
              </div>

              <div className="bg-white border border-stone-200 rounded-xl p-6 shadow-sm print:border-none print:shadow-none print:p-0">
                <h2 className="text-xl font-bold text-blue-600 mb-6 border-b border-stone-200 pb-2 print:hidden">
                  {t('app.reports.informacionSeguimentoUnidade')}
                </h2>
                <h2 className="hidden print:block text-xl font-bold text-blue-600 mb-6 border-b border-stone-200 pb-2">
                  UD{selectedUnitData.id} {selectedUnitData.title}
                </h2>

                <div className="grid grid-cols-2 md:grid-cols-5 gap-6 mb-8">
                  <div>
                    <label className="text-sm text-stone-600 block mb-1">{t('app.reports.dataAlta')}</label>
                    <div className="p-2 bg-stone-50 border border-stone-200 rounded text-stone-700 text-sm">{currentReport.dataAlta}</div>
                  </div>
                  <div>
                    <label className="text-sm text-stone-600 block mb-1">{t('app.reports.dataUltimaModificacion')}</label>
                    <div className="p-2 bg-stone-50 border border-stone-200 rounded text-stone-700 text-sm">{currentReport.dataUltimaModificacion}</div>
                  </div>
                  <div>
                    <label className="text-sm text-stone-600 block mb-1">{t('app.reports.dataInicio')}</label>
                    <div className="p-2 bg-white border border-stone-300 rounded text-stone-900 text-sm flex justify-between items-center">
                      {selectedUnitData.firstDay ? new Date(selectedUnitData.firstDay).toLocaleDateString('gl-ES') : '-'}
                      <CalendarIcon className="w-4 h-4 text-stone-400" />
                    </div>
                  </div>
                  <div>
                    <label className="text-sm text-stone-600 block mb-1">{t('app.reports.dataFin')}</label>
                    <div className="p-2 bg-white border border-stone-300 rounded text-stone-900 text-sm flex justify-between items-center">
                      {selectedUnitData.lastDay ? new Date(selectedUnitData.lastDay).toLocaleDateString('gl-ES') : '-'}
                      <CalendarIcon className="w-4 h-4 text-stone-400" />
                    </div>
                  </div>
                </div>

                <div className="grid grid-cols-1 md:grid-cols-3 gap-6 mb-8">
                  <div>
                    <label className="text-sm text-stone-600 block mb-1">{t('app.reports.sesionsPrevistas')}</label>
                    <div className="p-2 bg-stone-50 border border-stone-200 rounded text-stone-700 text-sm">{selectedUnitData.sessions}</div>
                  </div>
                  <div>
                    <label className="text-sm text-stone-600 block mb-1">{t('app.reports.sesionsRealizadas')}</label>
                    <div className="p-2 bg-white border border-stone-300 rounded text-stone-900 text-sm">{selectedUnitData.actualSessions}</div>
                  </div>
                  <div>
                    <label className="text-sm text-stone-600 block mb-1">{t('app.reports.graoDeCumprimento')}</label>
                    <select
                      value={currentReport.graoCumprimento}
                      onChange={(e) => handleReportChange('graoCumprimento', e.target.value)}
                      disabled={subject.isLocked}
                      className="w-full p-2 bg-white border border-stone-300 rounded text-stone-900 text-sm outline-none focus:border-stone-900 disabled:bg-stone-50 disabled:text-stone-500"
                    >
                      <option value=""></option>
                      <option value="Alto">{t('app.reports.alto')}</option>
                      <option value="Medio">{t('app.reports.medio')}</option>
                      <option value="Baixo">{t('app.reports.baixo')}</option>
                    </select>
                  </div>
                </div>

                {!subject.isLocked && (
                  <div className="mb-8 flex flex-wrap items-end gap-3 print:hidden">
                    <div className="flex-1 min-w-[220px]">
                      <label className="text-sm text-stone-600 block mb-1">{t('app.reports.peticionAdicionalOpcional')}</label>
                      <input
                        type="text"
                        value={peticionUnidade}
                        onChange={(e) => setPeticionUnidade(e.target.value)}
                        placeholder={t('app.reports.peticionAdicionalPlaceholder')}
                        className="w-full p-2 bg-white border border-stone-300 rounded-lg text-sm outline-none focus:border-stone-900"
                      />
                    </div>
                    <button
                      onClick={xerarInformeUnidade}
                      disabled={!maltaDispoñible || maltaXerandoUnidadeId === selectedUnitId}
                      title={!maltaDispoñible ? t('app.reports.instalaMaltaParaInformes') : undefined}
                      className="flex items-center gap-1.5 px-3 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-sm font-bold transition-all shadow-sm disabled:opacity-50"
                    >
                      {maltaXerandoUnidadeId === selectedUnitId ? <Loader2 className="w-4 h-4 animate-spin" /> : <Sparkles className="w-4 h-4" />}
                      {maltaXerandoUnidadeId === selectedUnitId ? t('app.reports.xerando') : t('app.reports.xerarConMalta')}
                    </button>
                  </div>
                )}
                {maltaErro && <p className="text-xs text-rose-600 mb-6 print:hidden">{maltaErro}</p>}

                {(selectedUnitData.obxectivos || selectedUnitData.contidos || selectedUnitData.criterios) && (
                  <div className="mb-8 p-4 bg-blue-50/50 border border-blue-100 rounded-xl">
                    <h4 className="text-xs font-bold text-blue-700 uppercase tracking-wider mb-3 flex items-center gap-2">
                      <BookOpen className="w-3 h-3" />
                      {t('app.reports.baseDeCoñecemento')}
                    </h4>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                      {selectedUnitData.obxectivos && (
                        <div>
                          <label className="text-[10px] font-bold text-stone-500 uppercase">{t('app.reports.obxectivos')}</label>
                          <p className="text-xs text-stone-700 mt-1 leading-relaxed">{selectedUnitData.obxectivos}</p>
                        </div>
                      )}
                      {selectedUnitData.contidos && (
                        <div>
                          <label className="text-[10px] font-bold text-stone-500 uppercase">{t('app.reports.contidos')}</label>
                          <p className="text-xs text-stone-700 mt-1 leading-relaxed">{selectedUnitData.contidos}</p>
                        </div>
                      )}
                      {selectedUnitData.criterios && (
                        <div className="md:col-span-2">
                          <label className="text-[10px] font-bold text-stone-500 uppercase">{t('app.reports.criteriosDeAvaliacion')}</label>
                          <p className="text-xs text-stone-700 mt-1 leading-relaxed">{selectedUnitData.criterios}</p>
                        </div>
                      )}
                    </div>
                  </div>
                )}

                <div className="mb-6">
                  <label className="text-sm text-stone-600 block mb-1">{t('app.reports.propostasModificacion')}</label>
                  <textarea
                    value={currentReport.propostas}
                    onChange={(e) => handleReportChange('propostas', e.target.value)}
                    disabled={subject.isLocked}
                    className="w-full p-3 bg-white border border-stone-300 rounded-lg shadow-sm focus:ring-1 focus:ring-piztu-primary focus:border-piztu-primary outline-none min-h-[100px] resize-y text-sm disabled:bg-stone-50 disabled:text-stone-500"
                  ></textarea>
                </div>

                <div>
                  <label className="text-sm text-stone-600 block mb-1">{t('app.reports.observacionsDesviacions')}</label>
                  <textarea
                    value={currentReport.observacions}
                    onChange={(e) => handleReportChange('observacions', e.target.value)}
                    disabled={subject.isLocked}
                    className="w-full p-3 bg-white border border-stone-300 rounded-lg shadow-sm focus:ring-1 focus:ring-piztu-primary focus:border-piztu-primary outline-none min-h-[100px] resize-y text-sm disabled:bg-stone-50 disabled:text-stone-500"
                  ></textarea>
                </div>
              </div>
            </div>
          )}

          {tab === 'monthly' && (() => {
            const selectedMonth = months.find((m: any) => monthKeyOf(m) === selectedMonthKey) || months[months.length - 1] || months[0];
            if (!selectedMonth) {
              return (
                <div className="text-center py-12 text-stone-500">
                  {t('app.reports.nonHaiRexistrosDiarios')}
                </div>
              );
            }
            const monthKey = monthKeyOf(selectedMonth);
            const { monthStart, monthEnd } = monthBounds(selectedMonth);
            const ata = rangoEfectivoAta(monthEnd);
            const maltaEntrada = maltaReportParaMes(selectedMonth);
            const maltaInforme = maltaEntrada ? maltaInformes[maltaEntrada.reportId] : null;
            const maltaPorUnidade: Record<number, any> = {};
            (maltaInforme?.unidades || []).forEach((u: any) => { maltaPorUnidade[u.unitId] = u; });

            const unidadesMes = unidadesTraballadasNoMes(monthStart, monthEnd);
            // Rexistros do mes que non pertencen a ningunha unidade (folgas,
            // excursións... ou sesións despois de esgotar a programación):
            // non entran na comparación, pero non se deben perder do informe.
            const outrosRexistros = selectedMonth.days.filter((d: any) =>
              !d.unit && d.dateString <= ata && (subject.logs[d.dateString]?.content || '').trim());

            const badgeEstado = (status: string) => (
              <span className={`text-[10px] font-bold px-2 py-0.5 rounded-md ${
                status === 'lectivo' ? 'bg-emerald-100 text-emerald-700' :
                status === 'exame' ? 'bg-purple-100 text-purple-700' :
                'bg-amber-100 text-amber-700'
              }`}>
                {status.toUpperCase()}
              </span>
            );

            return (
              <div className="flex flex-col gap-6 print:gap-6">
                <div className="print:hidden">
                  <label className="text-sm font-bold text-stone-700 mb-1 block">{t('app.reports.mesObxectivo')}</label>
                  <select
                    value={monthKey}
                    onChange={(e) => setSelectedMonthKey(e.target.value)}
                    className="p-2.5 bg-white border border-stone-300 rounded-lg shadow-sm outline-none focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary"
                  >
                    {months.map((m: any, i: number) => (
                      <option key={i} value={monthKeyOf(m)}>{m.name} {m.year}</option>
                    ))}
                  </select>
                </div>

                <div className="flex flex-col gap-4 print:break-inside-avoid">
                  <div className="bg-stone-800 text-white px-4 py-2 rounded-t-xl font-bold uppercase tracking-wider text-sm flex justify-between items-center">
                    <span>{selectedMonth.name} {selectedMonth.year}</span>
                    <span className="text-[10px] opacity-70 font-normal">
                      {ata < monthEnd && ata >= monthStart
                        ? t('app.reports.ataData', { data: new Date(ata).toLocaleDateString('gl-ES') })
                        : t('app.reports.rexistroMensual')}
                    </span>
                  </div>

                  <div className="bg-white border-x border-b border-stone-200 rounded-b-xl shadow-sm overflow-hidden print:border-stone-300">
                    {/* 1. Unidades traballadas no mes: cálculo determinista a
                        partir do calendario (mesmo reparto de sesións cá grella). */}
                    <div className="px-4 py-2 bg-stone-50 text-[10px] font-bold text-stone-500 uppercase tracking-wider border-b border-stone-100">
                      {t('app.reports.unidadesTraballadasNoMes')}
                    </div>
                    {unidadesMes.length === 0 ? (
                      <div className="p-6 text-center text-stone-400 text-sm">{t('app.reports.senUnidadesNesteMes')}</div>
                    ) : (
                      <table className="w-full text-left text-sm">
                        <thead className="text-stone-500 font-bold uppercase tracking-wider text-[10px]">
                          <tr>
                            <th className="px-4 py-2 border-b border-stone-100">{t('app.reports.unidadeDidactica')}</th>
                            <th className="px-4 py-2 border-b border-stone-100 text-center">{t('app.reports.sesionsNoMes')}</th>
                            <th className="px-4 py-2 border-b border-stone-100 text-center">{t('app.reports.sesionsAcumuladasProgramadas')}</th>
                            <th className="px-4 py-2 border-b border-stone-100 w-48">{t('app.reports.porcentaxeUnidade')}</th>
                          </tr>
                        </thead>
                        <tbody className="divide-y divide-stone-100">
                          {unidadesMes.map(u => (
                            <tr key={u.unit.id}>
                              <td className="px-4 py-3">
                                <div className="flex items-center gap-2">
                                  <div className={`w-3 h-3 rounded-full shrink-0 ${u.unit.color}`}></div>
                                  <span className="font-medium text-stone-900">UD {u.unit.id}: {u.unit.title}</span>
                                </div>
                              </td>
                              <td className="px-4 py-3 text-center font-bold text-stone-900">{u.sesionsMes}</td>
                              <td className="px-4 py-3 text-center text-stone-700">{u.sesionsAcumuladas} / {u.unit.sessions}</td>
                              <td className="px-4 py-3">
                                <div className="flex items-center gap-2">
                                  <div className="flex-1 h-2 bg-stone-100 rounded-full overflow-hidden">
                                    <div className={`h-full ${u.porcentaxe >= 100 ? 'bg-emerald-500' : 'bg-blue-500'}`} style={{ width: `${u.porcentaxe}%` }}></div>
                                  </div>
                                  <span className="text-xs font-bold text-stone-700 w-10 text-right">{u.porcentaxe}%</span>
                                </div>
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    )}

                    {/* 2. Comparación programación de aula ↔ "Contidos da sesión". */}
                    {unidadesMes.length > 0 && (
                      <>
                        <div className="px-4 py-2 bg-stone-50 text-[10px] font-bold text-stone-500 uppercase tracking-wider border-y border-stone-100">
                          {t('app.reports.comparacionProgramacion')}
                        </div>
                        <div className="divide-y divide-stone-100">
                          {unidadesMes.map(u => {
                            const ia = maltaPorUnidade[u.unit.id];
                            return (
                              <div key={u.unit.id} className="p-4 flex flex-col gap-3 print:break-inside-avoid">
                                <div className="flex items-center gap-2">
                                  <div className={`w-3 h-3 rounded-full shrink-0 ${u.unit.color}`}></div>
                                  <span className="text-sm font-bold text-stone-800">UD {u.unit.id}: {u.unit.title}</span>
                                </div>
                                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                                  <div className="p-3 rounded-lg border border-blue-100 bg-blue-50/40">
                                    <div className="text-[10px] font-bold text-blue-700 uppercase mb-1.5">{t('app.reports.programadoNaUnidade')}</div>
                                    {(u.unit.contidos || '').trim()
                                      ? <p className="text-xs text-stone-700 leading-relaxed whitespace-pre-wrap">{u.unit.contidos}</p>
                                      : <p className="text-xs text-stone-400 italic">{t('app.reports.senContidosProgramados')}</p>}
                                  </div>
                                  <div className="p-3 rounded-lg border border-emerald-100 bg-emerald-50/40">
                                    <div className="text-[10px] font-bold text-emerald-700 uppercase mb-1.5">{t('app.reports.rexistradoNasSesions')}</div>
                                    {u.sesions.length === 0 ? (
                                      <p className="text-xs text-stone-400 italic">{t('app.reports.senContidosDeSesion')}</p>
                                    ) : (
                                      <ul className="flex flex-col gap-2">
                                        {u.sesions.map((s: any) => (
                                          <li key={s.dateString} className="text-xs text-stone-700">
                                            <div className="flex items-center gap-2 mb-0.5">
                                              <span className="font-bold text-stone-600">
                                                {new Date(s.date).toLocaleDateString('gl-ES', { day: '2-digit', month: 'short' })}
                                                {s.sessionIndex ? ` · ${s.sessionIndex}/${u.unit.sessions}` : ''}
                                              </span>
                                              {s.log?.status && badgeEstado(s.log.status)}
                                            </div>
                                            {(s.log?.content || '').trim()
                                              ? <MarkdownPreview content={s.log.content} />
                                              : <span className="italic text-stone-400">{t('app.reports.sesionSenNotas')}</span>}
                                          </li>
                                        ))}
                                      </ul>
                                    )}
                                  </div>
                                </div>
                                {ia?.comparacion && (
                                  <div className="p-3 rounded-lg border border-stone-200 bg-white">
                                    <div className="text-[10px] font-bold text-stone-500 uppercase mb-1 flex items-center gap-1.5">
                                      <Sparkles className="w-3 h-3" />
                                      {t('app.reports.analiseIA')}
                                    </div>
                                    <p className="text-sm text-stone-700 leading-snug whitespace-pre-wrap">{ia.comparacion}</p>
                                    {ia.proposta && (
                                      <p className="text-sm text-stone-700 leading-snug mt-2"><span className="font-bold">{t('app.reports.propostaMellora')}:</span> {ia.proposta}</p>
                                    )}
                                  </div>
                                )}
                              </div>
                            );
                          })}
                        </div>
                      </>
                    )}

                    {/* 3. Informe redactado con IA (Malta): resumo do mes. */}
                    <div className="p-5 border-t border-stone-100 bg-blue-50/30">
                      <div className="flex justify-between items-center mb-3">
                        <h4 className="text-xs font-bold text-blue-800 uppercase tracking-widest flex items-center gap-2">
                          <FileText className="w-3.5 h-3.5" />
                          {t('app.reports.informeMotivadoDeSeguimento')}
                        </h4>
                        {maltaEntrada && !maltaInforme && (
                          <button
                            onClick={() => cargarInformeMalta(maltaEntrada.reportId)}
                            className="flex items-center gap-1.5 px-3 py-1 bg-stone-200 hover:bg-stone-300 text-stone-700 rounded-lg text-[10px] font-bold transition-all shadow-sm print:hidden"
                          >
                            {t('app.reports.verInformeMalta')}
                          </button>
                        )}
                      </div>

                      {maltaInforme ? (
                        <div className="flex flex-col gap-3">
                          {maltaInforme.comentarioGlobal && (
                            <p className="text-sm text-stone-800 leading-relaxed italic font-serif">{maltaInforme.comentarioGlobal}</p>
                          )}
                          {(maltaInforme.unidades || []).map((u: any) => (
                            <div key={u.unitId} className="border border-stone-200 rounded-lg p-3 bg-white">
                              <div className="flex justify-between items-baseline gap-2 mb-1 flex-wrap">
                                <span className="text-xs font-bold text-stone-700">UD {u.unitId} — {u.titulo}</span>
                                {u.porcentaxe !== undefined
                                  ? <span className="text-[10px] font-bold text-blue-700">{u.porcentaxe}%</span>
                                  : u.grauConsecucion && (
                                    <span className={`text-[10px] font-bold ${
                                      u.grauConsecucion === 'Alto' ? 'text-emerald-600' :
                                      u.grauConsecucion === 'Medio' ? 'text-amber-600' :
                                      'text-rose-600'
                                    }`}>{u.grauConsecucion}</span>
                                  )}
                              </div>
                              <p className="text-sm text-stone-700 leading-snug">{u.resumo}</p>
                            </div>
                          ))}
                          <div className="flex gap-2 justify-end print:hidden">
                            <button onClick={() => xerarInformeMensual(selectedMonth)} disabled={!maltaDispoñible || maltaXerandoMes === monthKey} className="flex items-center gap-1 px-3 py-1 bg-stone-100 hover:bg-stone-200 text-stone-700 rounded-lg text-[10px] font-bold disabled:opacity-50">
                              {maltaXerandoMes === monthKey ? <Loader2 className="w-3 h-3 animate-spin" /> : <Sparkles className="w-3 h-3" />}
                              {maltaXerandoMes === monthKey ? t('app.reports.xerando') : t('app.reports.rexenerar')}
                            </button>
                            <button onClick={() => exportarInformeMalta(maltaEntrada.reportId, 'pdf')} className="px-3 py-1 bg-stone-100 hover:bg-stone-200 text-stone-700 rounded-lg text-[10px] font-bold">{t('app.reports.exportarPDF')}</button>
                            <button onClick={() => exportarInformeMalta(maltaEntrada.reportId, 'odt')} className="px-3 py-1 bg-stone-100 hover:bg-stone-200 text-stone-700 rounded-lg text-[10px] font-bold">{t('app.reports.exportarODT')}</button>
                            <button onClick={() => exportarInformeMalta(maltaEntrada.reportId, 'docx')} className="px-3 py-1 bg-stone-100 hover:bg-stone-200 text-stone-700 rounded-lg text-[10px] font-bold">{t('app.reports.exportarDOCX')}</button>
                          </div>
                        </div>
                      ) : (
                        <div className="flex flex-col gap-2 print:hidden">
                          <div className="text-sm text-stone-400 italic py-2 text-center border border-dashed border-stone-200 rounded-lg bg-white/50">
                            {maltaEntrada
                              ? t('app.reports.premeVerInformeMalta')
                              : maltaDispoñible
                                ? t('app.reports.nonHaiInformeMotivado')
                                : t('app.reports.instalaMaltaParaInformes')}
                          </div>
                          {maltaDispoñible && (
                            <div className="flex flex-wrap items-end gap-2">
                              <input
                                type="text"
                                value={peticionMes}
                                onChange={(e) => setPeticionMes(e.target.value)}
                                placeholder={t('app.reports.peticionAdicionalPlaceholder')}
                                className="flex-1 min-w-[180px] p-1.5 bg-white border border-stone-300 rounded-lg text-xs outline-none focus:border-stone-900"
                              />
                              <button
                                onClick={() => xerarInformeMensual(selectedMonth)}
                                disabled={maltaXerandoMes === monthKey}
                                className="flex items-center gap-1.5 px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-[10px] font-bold transition-all shadow-sm disabled:opacity-50"
                              >
                                {maltaXerandoMes === monthKey ? <Loader2 className="w-3 h-3 animate-spin" /> : <Sparkles className="w-3 h-3" />}
                                {maltaXerandoMes === monthKey ? t('app.reports.xerando') : t('app.reports.xerarConMalta')}
                              </button>
                            </div>
                          )}
                        </div>
                      )}
                      {maltaErro && <p className="text-xs text-rose-600 mt-2 print:hidden">{maltaErro}</p>}
                    </div>

                    {outrosRexistros.length > 0 && (
                      <div className="divide-y divide-stone-100 border-t border-stone-100">
                        <div className="px-4 py-2 bg-stone-50 text-[10px] font-bold text-stone-500 uppercase tracking-wider">
                          {t('app.reports.outrosRexistrosDoMes')}
                        </div>
                        {outrosRexistros.map((day: any) => {
                          const log = subject.logs[day.dateString];
                          return (
                            <div key={day.dateString} className="p-4 flex gap-4">
                              <div className="w-24 shrink-0 text-sm font-bold text-stone-600">
                                {new Date(day.date).toLocaleDateString('gl-ES', { day: '2-digit', month: 'short' })}
                              </div>
                              <div className="flex-1 text-sm text-stone-700">
                                <div className="mb-1">{badgeEstado(log.status)}</div>
                                <MarkdownPreview content={log.content} />
                              </div>
                            </div>
                          );
                        })}
                      </div>
                    )}
                  </div>
                </div>
              </div>
            );
          })()}
        </div>
      </div>
    </div>
  );
}
