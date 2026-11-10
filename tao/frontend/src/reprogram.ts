import { Subject, Unit, DailyLog, DayStatus } from './types';
import { generateMonthlySchedule } from './data';

// Reprogramación por sesións.
//
// O contido «sesión a sesión» é o rexistro diario do profesor (Subject.logs, keyed by
// date). O eixe sobre o que se despraza é a secuencia ordenada de días de clase do curso.
// Os días marcados como folga/saída/ausencia son «sen clase»: non levan contido e
// desprazan o resto. As operacións edítanse sobre esa secuencia e reasígnase o contido.

export type NoClassStatus = 'folga' | 'excursion' | 'ausencia';
export const NO_CLASS: DayStatus[] = ['folga', 'excursion', 'ausencia'];

export type ReprogramAction =
  | { kind: 'markNoClass'; status: NoClassStatus }
  | { kind: 'clearNoClass' }
  | { kind: 'delete' }
  | { kind: 'insert' };

export interface ReprogramResult {
  logs: Record<string, DailyLog>;
  units: Unit[];
  overflow: number; // sesións de contido que quedan fóra do calendario (desprazamento á dereita)
  movedRight: boolean;
  unitsNote: string; // resumo dos cambios en unidades (para o aviso)
}

const isNoClass = (log?: DailyLog) => !!log && (NO_CLASS as string[]).includes(log.status);

// classDaysOf devolve as datas dos días de clase do curso, en orde. Reutiliza a mesma
// lóxica de calendario que xa usa toda a app (generateMonthlySchedule).
export function classDaysOf(subject: Subject): string[] {
  const { months } = generateMonthlySchedule(subject.config, subject.units, subject.logs);
  const out: string[] = [];
  for (const m of months) for (const d of m.days) if (d.isClassDay) out.push(d.dateString);
  return out;
}

// unitBoundaries devolve, para cada unidade, o índice de sesión de docencia onde empeza
// (acumulado de unit.sessions). Serve para saber a que unidade pertence a Nª sesión.
function ownerUnit(units: Unit[], teachingPos: number): number {
  let acc = 0;
  for (let i = 0; i < units.length; i++) {
    acc += Math.max(0, units[i].sessions || 0);
    if (teachingPos < acc) return i;
  }
  return units.length - 1; // máis alá da última unidade: atribúese á última
}

// isEmptyContent: un día de docencia lectivo sen nada que conservar. Calquera dato
// (texto, comentario do profesor, incidencias, desbloqueo, equipo) fai que se garde.
function isEmptyContent(item: Omit<DailyLog, 'date'>): boolean {
  return (!item.content || item.content.trim() === '')
    && item.status === 'lectivo'
    && !item.profesorComment
    && !(item.incidents && item.incidents.length > 0)
    && !item.unlocked
    && !item.equipo;
}

// contentOf extrae o «contido» dun log (todo agás a data), que é o que viaxa ao desprazar.
function contentOf(log: DailyLog): Omit<DailyLog, 'date'> {
  const { date, ...rest } = log;
  return rest;
}

// applyReprogram calcula os novos logs e unidades tras unha operación de reprogramación
// ancorada nun día de clase. Non muta o subject; devolve copias.
export function applyReprogram(subject: Subject, anchorDate: string, action: ReprogramAction, count = 1): ReprogramResult {
  const classDays = classDaysOf(subject);
  const anchorIdx = classDays.indexOf(anchorDate);
  const n = Math.max(1, Math.floor(count) || 1);
  const units = subject.units.map(u => ({ ...u }));
  let unitsNote = '';

  if (anchorIdx < 0) {
    return { logs: { ...subject.logs }, units, overflow: 0, movedRight: false, unitsNote };
  }

  // Sequence per class day: {noClass?: status} ou o log actual (ou lectivo baleiro).
  type Slot = { date: string; noClass?: DayStatus; content?: Omit<DailyLog, 'date'> };
  const slots: Slot[] = classDays.map(date => {
    const log = subject.logs[date];
    if (isNoClass(log)) return { date, noClass: log!.status };
    return { date, content: log ? contentOf(log) : { content: '', status: 'lectivo' as DayStatus } };
  });

  // "teaching position" do ancoraxe = nº de días de docencia (non sen-clase) antes del.
  const teachingPosOf = (idx: number) => slots.slice(0, idx).filter(s => !s.noClass).length;

  // Extraer a lista de contidos de docencia (en orde) e a marca sen-clase por día.
  let contents: (Omit<DailyLog, 'date'> | null)[] = slots.filter(s => !s.noClass).map(s => s.content!);
  const noClassByDate = new Map<string, DayStatus>();
  slots.forEach(s => { if (s.noClass) noClassByDate.set(s.date, s.noClass); });

  const teachingPos = teachingPosOf(anchorIdx);
  let movedRight = false;

  switch (action.kind) {
    case 'markNoClass': {
      // O día pasa a sen clase; os contidos non se tocan (o de anchor e seguintes caen no
      // seguinte día de docencia). Se anchor xa era sen clase, primeiro «devólvese» a lectivo.
      noClassByDate.set(anchorDate, action.status);
      movedRight = true;
      break;
    }
    case 'clearNoClass': {
      noClassByDate.delete(anchorDate);
      break;
    }
    case 'delete': {
      // Quita N contidos de docencia desde a posición do ancoraxe (cascada á esquerda).
      const removeCount = Math.min(n, Math.max(0, contents.length - teachingPos));
      // Axuste de unidades: cada sesión eliminada resta á súa unidade propietaria.
      for (let k = 0; k < removeCount; k++) {
        const ui = ownerUnit(units, teachingPos + k);
        if (ui >= 0 && ui < units.length) units[ui].sessions = Math.max(1, (units[ui].sessions || 1) - 1);
      }
      contents.splice(teachingPos, removeCount);
      unitsNote = removeCount > 0 ? `Eliminadas ${removeCount} sesión(s); unidade(s) axustada(s).` : '';
      break;
    }
    case 'insert': {
      // Insire N sesións baleiras na posición do ancoraxe (cascada á dereita).
      const blanks: (Omit<DailyLog, 'date'>)[] = Array.from({ length: n }, () => ({ content: '', status: 'lectivo' as DayStatus }));
      contents.splice(teachingPos, 0, ...blanks);
      const ui = ownerUnit(units, teachingPos);
      if (ui >= 0 && ui < units.length) units[ui].sessions = (units[ui].sessions || 0) + n;
      movedRight = true;
      unitsNote = `Inseridas ${n} sesión(s); unidade «${units[ui]?.title || ''}» +${n}.`;
      break;
    }
  }

  // Reasignar SÓ desde o día de ancoraxe en diante. Unha reprogramación nunca pode
  // modificar nin borrar o rexistrado antes dese día: antes reconstruíase o mapa de logs
  // enteiro a partir de classDays, así que calquera log anterior que non casase cun día
  // de clase calculado nese intre (cambios de horario, calendario ou datas de inicio) ou
  // que se considerase «baleiro» desaparecía do servidor ao publicar.
  //
  // 1) Todo o que queda fóra do tramo reescrito consérvase tal cal: os días anteriores
  //    ao ancoraxe e calquera log nunha data que non sexa día de clase.
  const classSet = new Set(classDays);
  const newLogs: Record<string, DailyLog> = {};
  for (const [date, log] of Object.entries(subject.logs || {})) {
    if (date < anchorDate || !classSet.has(date)) newLogs[date] = log;
  }

  // 2) Desde o ancoraxe: os sen-clase quedan sen contido; aos de docencia asígnaselles,
  //    en orde, a lista de contidos. Os contidos anteriores ao ancoraxe (índices
  //    < teachingPos) non se moven, así que se empeza a contar dende aí. Os que sobren
  //    (desprazamento á dereita) contan como desbordamento.
  let ci = teachingPos;
  for (let i = anchorIdx; i < classDays.length; i++) {
    const date = classDays[i];
    const nc = noClassByDate.get(date);
    if (nc) {
      newLogs[date] = { date, content: '', status: nc };
      continue;
    }
    const item = contents[ci++];
    if (item == null) continue; // sen contido asignado (día baleiro)
    if (isEmptyContent(item)) continue; // non ensuciar os logs con días lectivos baleiros
    newLogs[date] = { date, ...item } as DailyLog;
  }

  // Desbordamento = contido NON baleiro que non colle (perderíase ao desprazar á dereita).
  const teachingCapacity = classDays.filter(d => !noClassByDate.get(d)).length;
  let overflow = 0;
  for (let i = teachingCapacity; i < contents.length; i++) {
    const item = contents[i];
    if (item && item.content && item.content.trim() !== '') overflow++;
  }
  return { logs: newLogs, units, overflow, movedRight, unitsNote };
}
