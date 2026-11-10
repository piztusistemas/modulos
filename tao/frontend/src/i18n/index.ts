// i18n/index.ts — Configuración de i18next para Tao.
//
// Tao corre illado no equipo do alumnado (non le config.yaml do servidor,
// ver filebrowser.go), así que o idioma escóllese aquí mesmo,
// gárdase en localStorage (persiste entre sesións do navegador embebido de
// Wails) e detéctase por defecto a partir do idioma do sistema operativo.
import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';

import { IdiomaPiztu } from '../../bindings/tao/app';

import gl from './locales/gl.json';
import es from './locales/es.json';
import en from './locales/en.json';
import pt from './locales/pt.json';

export const IDIOMAS_DISPONIBLES = [
  { code: 'gl', nome: 'Galego' },
  { code: 'es', nome: 'Castellano' },
  { code: 'en', nome: 'English' },
  { code: 'pt', nome: 'Português' },
] as const;

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      gl: { translation: gl },
      es: { translation: es },
      en: { translation: en },
      pt: { translation: pt },
    },
    fallbackLng: 'gl',
    supportedLngs: ['gl', 'es', 'en', 'pt'],
    interpolation: { escapeValue: false },
    detection: {
      // localStorage (elección explícita do alumnado) antes ca o idioma do
      // navegador/SO, para que a escolla persista entre sesións.
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
      // Nome legado (mantido por compatibilidade coa elección xa gardada no
      // navegador do alumnado en instalacións existentes).
      lookupLocalStorage: 'ronsel_idioma',
    },
  });

export default i18n;

// EXPLICITO_KEY marca que o idioma actual foi escollido A MAN nesta
// instalación de Tao (selector de idioma en Login.tsx) — distinto da
// entrada normal de LanguageDetector (`ronsel_idioma`), que tamén se
// escribe en localStorage por AUTODETECCIÓN (SO/navegador) ou cando
// aplicarIdiomaPiztuPorDefecto aplica o idioma de Piztu, sen que ninguén
// escollera nada a propósito. Antes desta marca, calquera destas escrituras
// "accidentais" quedaba indistinguible dunha elección real, así que o
// idioma de Piztu só se aplicaba a primeira vez que arrincaba Tao e nunca
// máis — ver a nota de implementación abaixo.
const EXPLICITO_KEY = 'ronsel_idioma_explicito';

// marcarIdiomaExplicito rexistra que o cambio de idioma foi unha elección real
// da persoa que usa Tao — chamar SÓ dende o selector de Login.tsx, nunca
// dende código que aplica un valor por defecto.
export function marcarIdiomaExplicito(): void {
  localStorage.setItem(EXPLICITO_KEY, '1');
}

// aplicarIdiomaPiztuPorDefecto consulta o idioma que o profesorado ten escollido
// para o propio Piztu (IdiomaPiztu, app.go — vén de PIZTU_CONTEXTO cando Piztu
// abriu Tao) e, SO SE ninguén fixo unha elección real en Tao (EXPLICITO_KEY,
// non `ronsel_idioma` — ver nota abaixo), aplícao. Chámase en CADA arranque
// de Tao (non só o primeiro): se o profesorado cambia o idioma de Piztu e
// volve abrir Tao, Tao debe reflectir ese cambio, non quedar no que fose a
// primeira vez.
//
// Chámase unha soa vez por proceso, en main.tsx, ANTES do primeiro render —
// así evita calquera parpadeo (arrincar nun idioma e trocar decontado a
// outro). Tolerante de máis a fallos a propósito: sen contexto de Piztu (Tao
// arrincado á man, desenvolvemento) IdiomaPiztu() devolve "", e calquera
// erro da chamada Wails simplemente non cambia nada — o idioma xa detectado
// (localStorage/SO) segue a valer.
//
// Nota de implementación: o deseño orixinal miraba `ronsel_idioma` (a
// entrada que LanguageDetector xestiona) para decidir "xa hai elección,
// non tocar". O problema: `caches: ['localStorage']` fai que
// LanguageDetector escriba esa mesma entrada en CALQUERA cambio de idioma,
// INCLUÍDO o que este mesmo código aplica ao seguir a Piztu — así que a
// primeira vez que Tao arrincaba dentro de Piztu xa quedaba "marcado" coma
// elección explícita para sempre, e cambios posteriores do idioma de Piztu
// deixaban de chegar. `EXPLICITO_KEY` é unha entrada á parte que só escribe
// marcarIdiomaExplicito(), chamada unicamente dende a acción real da persoa
// no selector — nunca dende aquí.
export async function aplicarIdiomaPiztuPorDefecto(): Promise<void> {
  if (localStorage.getItem(EXPLICITO_KEY)) {
    return; // elección real xa feita en Tao: Piztu nunca a substitúe
  }
  try {
    const idioma = (await IdiomaPiztu()).trim();
    if (idioma && IDIOMAS_DISPONIBLES.some((d) => d.code === idioma)) {
      await i18n.changeLanguage(idioma);
    }
  } catch {
    // Sen contexto de Piztu ou API non dispoñible: queda o idioma xa detectado.
  }
}
