// Plan de cambio de grupo dun alumno (lado Go: tao/cambio_grupo.go).
//
// O traballo do alumno gárdase por materia, e cada grupo adoita ter a súa propia
// materia (outro id, outro calendario). Para que ao pasar de bach1a a bach1b vexa no
// grupo novo o que xa fixera, emparéllase cada materia que deixa coa equivalente que
// colle (mesmo nome e curso) e tradúcense as datas: a Nª sesión do calendario vello
// pasa á Nª do novo. Iso só se pode calcular aquí, coa mesma lóxica de calendario da
// app (classDaysOf), que non existe en Go.

import { MateriaDef, MigracionMateria } from '../bindings/tao/models';
import { Subject } from './types';
import { defaultConfig } from './data';
import { classDaysOf } from './reprogram';
import { gruposDe, pertenceAGrupo } from './grupos';

export interface ParMaterias {
  from: MateriaDef;
  to: MateriaDef;
  datas: Record<string, string>;
}

export interface PlanCambioGrupo {
  pares: ParMaterias[];
  // Materias que deixa sen equivalente no grupo novo: o seu traballo non se copia
  // (queda gardado, pero o alumno non o verá mentres estea no grupo novo).
  senPar: MateriaDef[];
}

const imparteA = (m: MateriaDef, campoGrupo: string) =>
  (m.groups || []).some(g => pertenceAGrupo(campoGrupo, g));

// Clave de emparellamento: nome sen maiúsculas, acentos nin espazos de máis, e o curso
// académico cando o hai (dúas "Tecnoloxía" de cursos distintos non son a mesma).
const clave = (m: MateriaDef) => {
  const nome = (m.name || '').normalize('NFD').replace(/[̀-ͯ]/g, '')
    .toLowerCase().replace(/\s+/g, ' ').trim();
  return `${nome}|${(m.academicYear || '').trim()}`;
};

// diasDeClase devolve as datas das sesións da materia, igual que as ve o alumnado
// (mesma config por defecto que buildStudentSubjects en App.tsx, e os estados do
// profesor —folgas, saídas...— que desprazan as sesións).
function diasDeClase(m: MateriaDef): string[] {
  const cfg: any = m.config || {};
  const subject = {
    id: m.id, name: m.name, academicYear: m.academicYear, isLocked: false,
    config: { ...defaultConfig, ...cfg, scheduleDays: cfg.scheduleDays || [1, 3, 5], customHolidays: cfg.customHolidays || [] },
    units: Array.isArray(m.units) ? m.units : [],
    logs: (m.logs as any) || {},
    unitReports: {},
  } as Subject;
  try {
    return classDaysOf(subject);
  } catch {
    return []; // unha programación rota non debe impedir o cambio: as datas consérvanse
  }
}

/** Calcula que materias se migran ao pasar do grupo `vello` ao `novo`. */
export function planCambioGrupo(materias: MateriaDef[], vello: string, novo: string): PlanCambioGrupo {
  // Só contan as materias que realmente cambian: unha que se imparte aos dous grupos
  // xa a ve igual despois do cambio.
  const deixa = materias.filter(m => imparteA(m, vello) && !imparteA(m, novo));
  const colle = materias.filter(m => imparteA(m, novo) && !imparteA(m, vello));

  const pares: ParMaterias[] = [];
  const senPar: MateriaDef[] = [];
  for (const from of deixa) {
    const candidatas = colle.filter(m => clave(m) === clave(from));
    // Con dúas candidatas iguais non se adiviña: mellor non copiar que copiar mal.
    if (candidatas.length !== 1) { senPar.push(from); continue; }
    const to = candidatas[0];
    const vellos = diasDeClase(from), novos = diasDeClase(to);
    const datas: Record<string, string> = {};
    for (let i = 0; i < Math.min(vellos.length, novos.length); i++) {
      if (vellos[i] !== novos[i]) datas[vellos[i]] = novos[i];
    }
    pares.push({ from, to, datas });
  }
  return { pares, senPar };
}

/** O plan no formato que espera ChangeUserGroup. */
export const migracions = (plan: PlanCambioGrupo): MigracionMateria[] =>
  plan.pares.map(p => new MigracionMateria({ from: p.from.id, to: p.to.id, datas: p.datas }));

/** Normaliza o campo de grupo como o garda o backend ("eso3a, eso3b"). */
export const normalizarGrupo = (campo: string) => gruposDe(campo).join(', ');
