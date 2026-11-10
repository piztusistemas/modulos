export type DayStatus = 'lectivo' | 'folga' | 'excursion' | 'ausencia' | 'exame';

// Tipo dunha incidencia disciplinar/de mérito (Incident, máis abaixo): positivo,
// negativo, expulsión ou falta de orde. Símbolo amosado no calendario e nos informes —
// ver INCIDENT_SYMBOLS en data.ts.
export type IncidentType = 'positivo' | 'negativo' | 'expulsion' | 'falta';

// Incident é unha incidencia solta engadida polo profesorado a un día concreto dun
// alumno (tao/incidencias.go: AddIncident/RemoveIncident). A diferenza da nota privada
// (ver notas privadas, xestionadas á parte con GetPrivateNote/SavePrivateNote — NUNCA
// viaxan por aquí), un Incident viaxa dentro do propio backup do alumno e é visible
// para el, coma profesorComment.
export interface Incident {
  id: string;
  type: IncidentType;
  note?: string;      // motivo curto, opcional, en texto plano
  createdAt: string;  // ISO
  createdBy: string;  // username do profesor que a engadiu
}

export interface DailyLog {
  date: string;
  content: string;
  status: DayStatus;
  // Comentario do profesorado sobre esta práctica (feedback). Escríbeo o profesor ao
  // revisar o alumno; o alumno véo en modo lectura. Consérvase ao gardar a práctica.
  profesorComment?: string;
  // Incidencias (positivo/negativo/expulsión/falta de orde) deste día. Poden ser varias.
  incidents?: Incident[];
  // Nome do equipo (hostname) que empregou o alumno ao gardar esta práctica, para poder
  // localizar o responsable en caso de desperfectos no computador. Gárdase automaticamente,
  // non o edita ninguén.
  equipo?: string;
  // Marca esta práctica en concreto como desbloqueada para o alumnado aínda sendo un día
  // distinto ao de hoxe. Decídeo o profesor por sesión (vive no seu log-modelo, m.logs, e
  // superponse sobre o log do alumno igual có estado da sesión).
  unlocked?: boolean;
}

export interface Unit {
  id: number;
  title: string;
  sessions: number;
  color: string;
  bgLight: string;
  text: string;
  obxectivos?: string;
  contidos?: string;
  criterios?: string;
  competencias?: string;
}

export interface Evaluation {
  name: string;      // p.ex. "1ª Avaliación"
  startDate: string; // YYYY-MM-DD (inclusive)
  endDate: string;   // YYYY-MM-DD (inclusive)
}

// Tipo de entrada do calendario escolar. Todas son días non lectivos; o tipo só di
// de onde vén cada unha, para que o profesorado distinga o que fixa o DOG (vacacións,
// festivos) do que engade o centro (días propios, actividades).
export type CalendarEntryType = 'vacacions' | 'festivo' | 'centro' | 'actividade';

// CalendarEntry é unha fila da táboa do calendario escolar: un día solto (sen `end`)
// ou un rango pechado [start, end].
export interface CalendarEntry {
  id: string;
  type: CalendarEntryType;
  name: string;
  start: string;  // YYYY-MM-DD
  end?: string;   // YYYY-MM-DD; ausente ou baleiro = un só día
}

export interface AppConfig {
  startDate: string;
  endDate: string;
  scheduleDays: number[]; // 1=Mon, 2=Tue, etc.
  holidays: { date: string; name: string }[];
  customHolidays?: { date: string; name: string }[];
  // calendar é a fonte de verdade dos días non lectivos desde que existe a táboa do
  // calendario escolar. Se está definido (aínda que sexa baleiro), `holidays` e
  // `customHolidays` ignóranse: son o formato antigo, que só se le para migrar.
  calendar?: CalendarEntry[];
  serverUrl?: string; // Fase 2: servidor de sincronización (backup/datos/certificado)
  evaluations?: Evaluation[]; // límites das avaliacións (para o informe por avaliación)
  proensUrl?: string; // URL pública de vistaPreliminar de Proens, usada por ImportProens/ImportProensInfo
  dogUrl?: string; // URL da publicación do DOG co calendario escolar, usada por ImportSchoolCalendar
}

// Espello de ProensObxectivo (tao/proens_info.go): un obxectivo (OBX1...) coas
// competencias clave ás que contribúe.
export interface ProensObxectivo {
  codigo: string;
  texto: string;
  competencias: string[];
}

// Espello de ProensPesoCualificacion (tao/proens_info.go).
export interface ProensPesoCualificacion {
  instrumento: string;
  total: string;
}

// Espello de ProensCriterio (tao/proens_info.go): un criterio de avaliación ou
// un contido (currículo, 3.2) ou unha fila de avaliación (3.3), có seu código
// curto ("CA1.1", "C1.1"...) e a súa descrición.
export interface ProensCriterio {
  codigo: string;
  descricion: string;
}

// Espello de ProensBloqueUD (tao/proens_info.go): un bloque de currículo
// restrinxido a unha UD concreta (só os criterios/contidos marcados para ela).
export interface ProensBloqueUD {
  bloque: string;
  criterios: ProensCriterio[];
  contidos: ProensCriterio[];
}

// Espello de ProensInstrumentoUD (tao/proens_info.go): unha fila da táboa de
// avaliación dunha UD.
export interface ProensInstrumentoUD {
  criterio: ProensCriterio;
  tipoInstrumento: string;
  graoMinimo: string;
  peso: string;
}

// Espello de ProensUnidadeInfo (tao/proens_info.go): o desglose por UD amosado
// ao enfocar un chip "UD N" — trimestre, bloques de currículo e avaliación.
export interface ProensUnidadeInfo {
  secuencia: number;
  titulo: string;
  trimestre: number;
  bloques: ProensBloqueUD[];
  avaliacion: ProensInstrumentoUD[];
}

// Espello de ProensInfo (tao/proens_info.go): información xeral da programación
// (introdución, obxectivos+competencias, criterios de cualificación) máis, por
// UD, o desglose de currículo e avaliación, extraído de Proens por ImportProensInfo.
export interface ProensInfo {
  introducion: string[];
  obxectivos: ProensObxectivo[];
  criteriosTexto: string[];
  criteriosPesos: ProensPesoCualificacion[];
  unidades: ProensUnidadeInfo[];
}

// Datos básicos do alumno/a, obtidos no login (Fase 1: perfil local; Fase 2: servidor).
export interface StudentProfile {
  username: string;
  name: string;
  surname: string;
  group?: string;      // grupo/curso, p.ex. "3º ESO A"
  certPath?: string;   // ruta local ao certificado .p12/.pfx
  hasCert?: boolean;   // indicador de que hai certificado dispoñible
  certSource?: string; // orixe do certificado: "servidor", "local", "demo" ou "" (ningún)
  connected?: boolean; // true se está autenticado contra o servidor de ficheiros (File Browser)
  isAdmin?: boolean;   // true se entrou en modo administración (Director)
  role?: 'profesor' | 'alumno'; // rol resolto no login (o admin resólvese con isAdmin)
}

export interface UnitReport {
  dataAlta: string;
  dataUltimaModificacion: string;
  graoCumprimento: 'Alto' | 'Medio' | 'Baixo' | '';
  propostas: string;
  observacions: string;
}

export interface Subject {
  id: string;
  name: string;
  academicYear: string;
  isLocked: boolean;
  config: AppConfig;
  units: Unit[];
  logs: Record<string, DailyLog>;
  unitReports: Record<number, UnitReport>;
  monthlyReports?: Record<string, string>; // Key: "YYYY-MM"
  groups?: string[]; // grupos aos que se imparte a materia (usado polo profesor)
  // Username do profesor que creou a materia (tao/profesor.go: MateriaDef.Owner). Só el
  // (ou un administrador) pode gardala/eliminala — ver ListMyMaterias/SaveMateria. Baleiro
  // nunha materia previa a este campo, ata que un profesor a garde e a "adopte".
  owner?: string;
  // Cache do último ImportProensInfo correcto, para non ter que rescrapear Proens
  // cada vez que se abre o panel de información xeral da programación.
  programacionInfo?: ProensInfo;
  programacionInfoUpdatedAt?: string; // ISO, data da última importación correcta
}

