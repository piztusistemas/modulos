// Grupos dun perfil (espello de grupos.go no lado Go).
//
// O campo "grupo" dun alumno pode conter varios grupos separados por comas
// ("eso3a, eso3b"): así se dan de alta as contas que asisten a máis dun grupo. Comparar
// ese campo como unha cadea única deixa a ese alumnado fóra da súa materia, do muro e
// das listas do profesorado, así que toda comparación de grupo pasa por aquí.

/** Divide o campo de grupo nos grupos individuais, sen baleiros. */
export function gruposDe(campo?: string | null): string[] {
  return (campo || '')
    .split(/[,;]/)
    .map(g => g.trim())
    .filter(Boolean);
}

/** Indica se o campo de grupo inclúe `grupo` (sen distinguir maiúsculas). */
export function pertenceAGrupo(campo: string | null | undefined, grupo: string): boolean {
  const g = grupo.trim().toLowerCase();
  return g !== '' && gruposDe(campo).some(x => x.toLowerCase() === g);
}

/** Indica se dous campos de grupo teñen algún grupo en común. */
export function compartenGrupo(a?: string | null, b?: string | null): boolean {
  return gruposDe(a).some(g => pertenceAGrupo(b, g));
}
