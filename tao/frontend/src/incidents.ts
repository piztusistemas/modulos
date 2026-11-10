import { Incident, IncidentType } from './types';

// Símbolo, etiqueta e cor de cada tipo de incidencia — punto único de verdade para o
// selector do DayModal, a grella do calendario (App.tsx) e o resumo dos informes
// (reportHtml.ts). Non reutiliza a letra que xa amosa a grella para folga/ausencia/
// excursión (App.tsx): unha "falta de orde" tamén sería "F", así que vai en insignia de
// cor separada, non coma letra de status, para non confundilas.
export const INCIDENT_SYMBOLS: Record<IncidentType, string> = {
  positivo: '+',
  negativo: '−',
  expulsion: '✕',
  falta: 'F',
};

// INCIDENT_LABEL_KEYS son claves de i18n (i18n/locales/*.json), non texto — este módulo
// non ten acceso a react-i18next (non é un compoñente), así que cada sitio que amosa a
// etiqueta chama t(INCIDENT_LABEL_KEYS[type]) coa súa propia función `t` (App.tsx vía
// useTranslation, reportHtml.ts vía o `t` que xa recibe de ReportBuilder.tsx).
export const INCIDENT_LABEL_KEYS: Record<IncidentType, string> = {
  positivo: 'incident.positivo',
  negativo: 'incident.negativo',
  expulsion: 'incident.expulsion',
  falta: 'incident.falta',
};

export const INCIDENT_COLORS: Record<IncidentType, string> = {
  positivo: 'bg-emerald-500 text-emerald-50',
  negativo: 'bg-rose-500 text-rose-50',
  expulsion: 'bg-stone-900 text-white',
  falta: 'bg-amber-500 text-amber-50',
};

export const INCIDENT_TYPES: IncidentType[] = ['positivo', 'negativo', 'expulsion', 'falta'];

export interface IncidentCounts {
  positivo: number;
  negativo: number;
  expulsion: number;
  falta: number;
}

export function emptyIncidentCounts(): IncidentCounts {
  return { positivo: 0, negativo: 0, expulsion: 0, falta: 0 };
}

// countIncidents resume calquera lista de entradas con `incidents` (as do informe
// filtrado en ReportBuilder.tsx) nos catro contadores que pide o resumo do informe.
export function countIncidents(entries: { incidents?: Incident[] }[]): IncidentCounts {
  const counts = emptyIncidentCounts();
  for (const e of entries) {
    for (const inc of e.incidents || []) {
      counts[inc.type]++;
    }
  }
  return counts;
}

// distinctIncidentTypes devolve os TIPOS presentes nese día, sen repetir (varias
// incidencias do mesmo tipo o mesmo día amosan unha soa insignia na grella — o detalle
// completo, con motivo, está no DayModal).
export function distinctIncidentTypes(incidents?: Incident[]): IncidentType[] {
  if (!incidents || incidents.length === 0) return [];
  const seen = new Set<IncidentType>();
  for (const inc of incidents) seen.add(inc.type);
  return INCIDENT_TYPES.filter(t => seen.has(t));
}
