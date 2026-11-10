import React from 'react';
import { Plus, Trash2, AlertCircle } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { CalendarEntry, CalendarEntryType } from './types';

// Táboa editable do calendario escolar. Amosa o inicio e o remate do curso e unha fila
// por período vacacional, festivo, día propio do centro ou actividade non lectiva. É a
// única interface que edita estes datos: o que aquí queda é o que se publica na materia
// e o que ve o alumnado no seu calendario.

const TYPE_KEYS: CalendarEntryType[] = ['vacacions', 'festivo', 'centro', 'actividade'];

// Cor por tipo, para distinguir dun golpe de vista o que fixa o DOG do que engade o centro.
const TYPE_STYLES: Record<CalendarEntryType, string> = {
  vacacions: 'bg-sky-100 text-sky-800 border-sky-200',
  festivo: 'bg-amber-100 text-amber-800 border-amber-200',
  centro: 'bg-emerald-100 text-emerald-800 border-emerald-200',
  actividade: 'bg-purple-100 text-purple-800 border-purple-200',
};

const newId = () => `c${Date.now().toString(36)}${Math.random().toString(36).slice(2, 7)}`;

// countDays conta os días naturais que ocupa unha fila (o rango é pechado).
const countDays = (e: CalendarEntry): number => {
  if (!e.start) return 0;
  const last = e.end && e.end >= e.start ? e.end : e.start;
  const ms = new Date(`${last}T12:00:00`).getTime() - new Date(`${e.start}T12:00:00`).getTime();
  return Math.round(ms / 86400000) + 1;
};

interface Props {
  startDate: string;
  endDate: string;
  entries: CalendarEntry[];
  onStartDateChange: (d: string) => void;
  onEndDateChange: (d: string) => void;
  onEntriesChange: (entries: CalendarEntry[]) => void;
}

export default function CalendarTable({
  startDate, endDate, entries, onStartDateChange, onEndDateChange, onEntriesChange,
}: Props) {
  const { t } = useTranslation();

  const TYPE_LABELS: Record<CalendarEntryType, string> = {
    vacacions: t('calendar.tipoVacacions'),
    festivo: t('calendar.tipoFestivo'),
    centro: t('calendar.tipoCentro'),
    actividade: t('calendar.tipoActividade'),
  };

  const update = (id: string, patch: Partial<CalendarEntry>) => {
    onEntriesChange(entries.map(e => (e.id === id ? { ...e, ...patch } : e)));
  };

  const remove = (id: string) => onEntriesChange(entries.filter(e => e.id !== id));

  const add = () => {
    onEntriesChange([
      ...entries,
      { id: newId(), type: 'centro', name: '', start: startDate || '', end: '' },
    ]);
  };

  // Ordénase para amosar, pero non se reordena o estado mentres se edita: unha fila non
  // debe saltar de sitio no medio de escribir a data.
  const sorted = [...entries].sort((a, b) => (a.start || '').localeCompare(b.start || ''));

  const outOfCourse = (e: CalendarEntry): boolean => {
    if (!e.start || !startDate || !endDate) return false;
    const last = e.end && e.end >= e.start ? e.end : e.start;
    return last < startDate || e.start > endDate;
  };

  const badRange = (e: CalendarEntry): boolean => !!e.end && !!e.start && e.end < e.start;

  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <label className="flex flex-col gap-1.5">
          <span className="text-sm font-bold text-stone-700">{t('calendar.inicioDoCurso')}</span>
          <input
            type="date"
            value={startDate}
            onChange={e => onStartDateChange(e.target.value)}
            className="p-2 border border-stone-300 rounded-lg"
          />
        </label>
        <label className="flex flex-col gap-1.5">
          <span className="text-sm font-bold text-stone-700">{t('calendar.remateDoCurso')}</span>
          <input
            type="date"
            value={endDate}
            onChange={e => onEndDateChange(e.target.value)}
            className="p-2 border border-stone-300 rounded-lg"
          />
        </label>
      </div>
      {startDate && endDate && endDate < startDate && (
        <p className="text-xs text-red-600 flex items-center gap-1">
          <AlertCircle className="w-3 h-3" /> {t('calendar.remateAnteriorAoInicio')}
        </p>
      )}

      <div className="overflow-x-auto border border-stone-200 rounded-lg">
        <table className="w-full text-sm min-w-[640px]">
          <thead className="bg-stone-50 text-stone-600">
            <tr>
              <th className="text-left font-bold p-2 w-[140px]">{t('calendar.tipo')}</th>
              <th className="text-left font-bold p-2">{t('calendar.nome')}</th>
              <th className="text-left font-bold p-2 w-[150px]">{t('calendar.desde')}</th>
              <th className="text-left font-bold p-2 w-[150px]">{t('calendar.ata')}</th>
              <th className="text-right font-bold p-2 w-[70px]">{t('calendar.dias')}</th>
              <th className="p-2 w-[44px]"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-stone-100">
            {sorted.length === 0 && (
              <tr>
                <td colSpan={6} className="p-4 text-center text-stone-400 italic">
                  {t('calendar.senDiasNonLectivos')}
                </td>
              </tr>
            )}
            {sorted.map(e => (
              <tr key={e.id} className="align-middle">
                <td className="p-2">
                  <select
                    value={e.type}
                    onChange={ev => update(e.id, { type: ev.target.value as CalendarEntryType })}
                    className={`w-full p-1.5 rounded-lg border font-medium outline-none ${TYPE_STYLES[e.type]}`}
                  >
                    {TYPE_KEYS.map(tk => (
                      <option key={tk} value={tk}>{TYPE_LABELS[tk]}</option>
                    ))}
                  </select>
                </td>
                <td className="p-2">
                  <input
                    type="text"
                    value={e.name}
                    onChange={ev => update(e.id, { name: ev.target.value })}
                    placeholder={t('calendar.exemploNome')}
                    className="w-full p-1.5 bg-white border border-stone-300 rounded-lg outline-none focus:ring-2 focus:ring-piztu-primary"
                  />
                </td>
                <td className="p-2">
                  <input
                    type="date"
                    value={e.start}
                    onChange={ev => update(e.id, { start: ev.target.value })}
                    className="w-full p-1.5 border border-stone-300 rounded-lg"
                  />
                </td>
                <td className="p-2">
                  <input
                    type="date"
                    value={e.end || ''}
                    onChange={ev => update(e.id, { end: ev.target.value })}
                    title={t('calendar.deixaoBaleiroSeUnSoDia')}
                    className={`w-full p-1.5 border rounded-lg ${badRange(e) ? 'border-red-400 bg-red-50' : 'border-stone-300'}`}
                  />
                </td>
                <td className="p-2 text-right tabular-nums text-stone-500">
                  {badRange(e) ? '—' : countDays(e)}
                </td>
                <td className="p-2">
                  <button
                    onClick={() => remove(e.id)}
                    title={t('calendar.quitarAFila')}
                    className="p-1.5 text-stone-400 hover:text-red-600 rounded hover:bg-red-50 transition-colors"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {sorted.some(badRange) && (
        <p className="text-xs text-red-600 flex items-center gap-1">
          <AlertCircle className="w-3 h-3" /> {t('calendar.filasComRemateAnteriorAoInicio')}
        </p>
      )}
      {sorted.some(outOfCourse) && (
        <p className="text-xs text-amber-600 flex items-center gap-1">
          <AlertCircle className="w-3 h-3" /> {t('calendar.filasForaDoCurso')}
        </p>
      )}

      <div className="flex items-center justify-between gap-3 flex-wrap">
        <button
          onClick={add}
          className="px-3 py-1.5 text-sm bg-stone-100 border border-stone-300 text-stone-700 font-medium hover:bg-stone-200 rounded-lg transition-colors flex items-center gap-2"
        >
          <Plus className="w-4 h-4" /> {t('calendar.engadirFila')}
        </button>
        <p className="text-xs text-stone-400">
          {t('calendar.axudaAtaBaleiro')}
        </p>
      </div>
    </div>
  );
}
