import { DailyLog, AppConfig, Unit, CalendarEntry, CalendarEntryType } from './types';
import { distinctIncidentTypes } from './incidents';

export const defaultUnits: Unit[] = [
  { id: 1, title: 'Técnicas de representación gráfica', sessions: 24, color: 'bg-blue-500', bgLight: 'bg-blue-100', text: 'text-blue-50' },
  { id: 2, title: 'O método de proxectos', sessions: 15, color: 'bg-emerald-500', bgLight: 'bg-emerald-100', text: 'text-emerald-50' },
  { id: 3, title: 'Os materiais técnicos e as ferramentas do taller', sessions: 3, color: 'bg-amber-500', bgLight: 'bg-amber-100', text: 'text-amber-50' },
  { id: 4, title: 'Os mecanismos', sessions: 12, color: 'bg-rose-500', bgLight: 'bg-rose-100', text: 'text-rose-50' },
  { id: 5, title: 'A electricidade e electrónica básica', sessions: 24, color: 'bg-purple-500', bgLight: 'bg-purple-100', text: 'text-purple-50' },
  { id: 6, title: 'Programación', sessions: 12, color: 'bg-cyan-500', bgLight: 'bg-cyan-100', text: 'text-cyan-50' },
  { id: 7, title: 'Sistemas de control e robots', sessions: 12, color: 'bg-indigo-500', bgLight: 'bg-indigo-100', text: 'text-indigo-50' },
  { id: 8, title: 'Utilización das TIC', sessions: 3, color: 'bg-pink-500', bgLight: 'bg-pink-100', text: 'text-pink-50' },
];

export const defaultHolidays = [
  { date: '2025-10-31', name: 'Día do Ensino' },
  { date: '2025-11-03', name: 'Día non lectivo' },
  { date: '2025-12-08', name: 'Inmaculada Concepción' },
  { date: '2026-05-01', name: 'Día do Traballador' },
];

// Christmas
for (let d = new Date('2025-12-22'); d <= new Date('2026-01-07'); d.setDate(d.getDate() + 1)) {
  defaultHolidays.push({ date: d.toISOString().split('T')[0], name: 'Nadal' });
}

// Entroido
for (let d = new Date('2026-02-16'); d <= new Date('2026-02-18'); d.setDate(d.getDate() + 1)) {
  defaultHolidays.push({ date: d.toISOString().split('T')[0], name: 'Entroido' });
}

// Easter
for (let d = new Date('2026-03-30'); d <= new Date('2026-04-06'); d.setDate(d.getDate() + 1)) {
  defaultHolidays.push({ date: d.toISOString().split('T')[0], name: 'Semana Santa' });
}

export const defaultConfig: AppConfig = {
  startDate: '2025-09-08',
  endDate: '2026-06-19',
  scheduleDays: [1, 3, 5], // L, X, V
  holidays: defaultHolidays,
  serverUrl: '',
  evaluations: [
    { name: '1ª Avaliación', startDate: '2025-09-08', endDate: '2025-12-19' },
    { name: '2ª Avaliación', startDate: '2025-12-20', endDate: '2026-03-20' },
    { name: '3ª Avaliación', startDate: '2026-03-21', endDate: '2026-06-19' },
  ],
};

// addDays devolve a data (YYYY-MM-DD) resultante de sumar n días. Trabállase ao
// mediodía para que o cambio de horario de verán non desprace nunca o día.
const addDays = (date: string, n: number): string => {
  const d = new Date(`${date}T12:00:00`);
  d.setDate(d.getDate() + n);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
};

// expandCalendarEntries desprega as filas da táboa do calendario nun día por data.
// Ante dúas filas que collen o mesmo día, gaña a primeira, e por iso as entradas
// propias do centro deben ir despois das oficiais.
export function expandCalendarEntries(entries: CalendarEntry[]): Map<string, string> {
  const days = new Map<string, string>();
  for (const e of entries) {
    if (!e.start) continue;
    const last = e.end && e.end >= e.start ? e.end : e.start;
    // Tope de seguridade: un rango mal introducido (un ano de máis nun extremo) non
    // debe poder xerar un bucle interminable.
    for (let d = e.start, guard = 0; d <= last && guard < 800; d = addDays(d, 1), guard++) {
      if (!days.has(d)) days.set(d, e.name || 'Día non lectivo');
    }
  }
  return days;
}

// nonLectiveDays devolve os días non lectivos da configuración: a táboa do calendario
// se existe, e se non as listas do formato antigo (holidays + customHolidays).
export function nonLectiveDays(config: AppConfig): Map<string, string> {
  if (Array.isArray(config.calendar)) {
    return expandCalendarEntries(config.calendar);
  }
  const days = new Map<string, string>();
  for (const h of [...(config.holidays || []), ...(config.customHolidays || [])]) {
    if (h?.date && !days.has(h.date)) days.set(h.date, h.name);
  }
  return days;
}

// calendarEntriesFromLegacy migra as listas antigas de días soltos á táboa, agrupando
// os días consecutivos co mesmo nome nun único rango (os 17 días de Nadal pasan a ser
// unha fila). Un rango de máis dun día lese como período vacacional; un día solto,
// como festivo.
export function calendarEntriesFromLegacy(config: AppConfig): CalendarEntry[] {
  const days = [...nonLectiveDays(config).entries()]
    .map(([date, name]) => ({ date, name }))
    .sort((a, b) => a.date.localeCompare(b.date));

  const entries: CalendarEntry[] = [];
  for (const day of days) {
    const prev = entries[entries.length - 1];
    const prevLast = prev ? (prev.end || prev.start) : '';
    if (prev && prev.name === day.name && addDays(prevLast, 1) === day.date) {
      prev.end = day.date; // continúa o rango aberto
      continue;
    }
    entries.push({ id: `${day.date}-${entries.length}`, type: 'festivo', name: day.name, start: day.date });
  }
  return entries.map(e => ({
    ...e,
    type: (e.end && e.end > e.start ? 'vacacions' : 'festivo') as CalendarEntryType,
  }));
}

export function generateMonthlySchedule(config: AppConfig, units: Unit[], logs: Record<string, DailyLog>, locale: string = 'gl') {
  const months = [];
  let currentUnitIndex = 0;
  let sessionsAssignedToCurrentUnit = 0;
  let totalSchoolDays = 0;
  let totalClassDays = 0;

  const startYear = parseInt(config.startDate.split('-')[0]);
  const startMonth = parseInt(config.startDate.split('-')[1]) - 1;
  const endYear = parseInt(config.endDate.split('-')[0]);
  const endMonth = parseInt(config.endDate.split('-')[1]) - 1;
  
  const totalMonths = (endYear - startYear) * 12 + (endMonth - startMonth) + 1;
  // Nomes de mes segundo o idioma da UI (locale), non fixos en galego.
  const monthFormatter = new Intl.DateTimeFormat(locale, { month: 'long' });
  const monthNameOf = (y: number, m: number) => {
    const name = monthFormatter.format(new Date(y, m, 1));
    return name.charAt(0).toUpperCase() + name.slice(1);
  };

  const nonLective = nonLectiveDays(config);

  for (let monthOffset = 0; monthOffset < totalMonths; monthOffset++) {
    const year = startYear + Math.floor((startMonth + monthOffset) / 12);
    const month = (startMonth + monthOffset) % 12;
    const daysInMonth = new Date(year, month + 1, 0).getDate();
    const monthName = monthNameOf(year, month);

    const days = [];
    for (let d = 1; d <= daysInMonth; d++) {
      const date = new Date(year, month, d);
      const dateString = `${year}-${String(month + 1).padStart(2, '0')}-${String(d).padStart(2, '0')}`;
      const dayOfWeek = date.getDay();
      const isWeekend = dayOfWeek === 0 || dayOfWeek === 6;
      const holidayName = nonLective.get(dateString);
      const isHoliday = holidayName !== undefined;

      const isSchoolDay = dateString >= config.startDate && dateString <= config.endDate && !isHoliday && !isWeekend;
      const isClassDay = isSchoolDay && (config.scheduleDays || [1, 3, 5]).includes(dayOfWeek);
      
      const log = logs[dateString];
      const isException = log && (log.status === 'folga' || log.status === 'excursion' || log.status === 'ausencia');
      
      let unit = null;
      // sessionIndex: número (1-based) desta sesión dentro das de unit.sessions da súa
      // unidade - p.ex. 3 nunha unidade de 12 sesións. Só ten sentido xunto a `unit`
      // (amósase no tooltip do día, ver App.tsx: "(3/12 Venres)").
      let sessionIndex = null;

      // Only consume a session if it's a class day and NOT an exception (strike, excursion, etc.)
      // If it's an exam, it still consumes a session of the unit.
      if (isClassDay) {
        if (!isException) {
          totalClassDays++;
          if (currentUnitIndex < units.length) {
            unit = units[currentUnitIndex];
            sessionsAssignedToCurrentUnit++;
            sessionIndex = sessionsAssignedToCurrentUnit;
            if (sessionsAssignedToCurrentUnit >= unit.sessions) {
              currentUnitIndex++;
              sessionsAssignedToCurrentUnit = 0;
            }
          }
        }
      }

      if (isSchoolDay) {
        totalSchoolDays++;
      }

      days.push({
        day: d,
        date,
        dateString,
        dayOfWeek,
        isWeekend,
        isHoliday,
        holidayName: holidayName ?? null,
        isSchoolDay,
        isClassDay,
        isException,
        logStatus: log?.status,
        incidentTypes: distinctIncidentTypes(log?.incidents),
        unit,
        sessionIndex,
      });
    }

    months.push({
      year,
      month,
      name: monthName,
      days,
    });
  }

  return {
    months,
    stats: {
      totalSchoolDays,
      totalClassDays,
      totalUnitSessions: units.reduce((acc, u) => acc + u.sessions, 0)
    }
  };
}
