import { useState, useEffect, useRef } from 'react';
import { Lock, User, Loader2, School, Globe } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import Logo from './Logo';
import PasswordInput from './PasswordInput';
import { Login as LoginBinding, AdminLogin, CheckServer, DirectorConfigurado, ConfigurarDirector, ServidorPorDefecto, DetectarServidor } from '../bindings/tao/app';
import { StudentProfile } from './types';
import { IDIOMAS_DISPONIBLES, marcarIdiomaExplicito } from './i18n';

const SERVER_KEY = 'file-browser-url';
const DEFAULT_SERVER_URL = 'http://127.0.0.1:8080/arquivos';

export default function Login({ onLogin }: { onLogin: (p: StudentProfile) => void }) {
  const { t, i18n } = useTranslation();
  const [serverUrl, setServerUrl] = useState(() => localStorage.getItem(SERVER_KEY) ?? DEFAULT_SERVER_URL);
  // Vale true en canto o usuario toca o campo do servidor: a detección automática de
  // abaixo é só unha PROPOSTA e nunca debe pisar o que el escribiu.
  const editadoPolaPersoa = useRef(false);

  // O campo do servidor énchese só, en dous pasos (ver tao/servidor.go):
  //   1. Primeira vez neste equipo: a URL que deixou escrita a instalación
  //      (/opt/piztu/tao-servidor.conf), en vez dun localhost que non vale.
  //   2. Sempre: compróbase cal responde de verdade. Así un equipo que tiña
  //      gardado o vello ":8080" pásase só ao porto 80 cando o servidor muda,
  //      sen ter que ir equipo por equipo. Se non responde ningunha (servidor
  //      apagado, sen rede) déixase intacta a que houbese.
  // Tao tamén deduce o nome do servidor do nome deste equipo (bac17 -> bac0.local) e,
  // ao entrar, completa o que escriba o alumno (só "bac0.local" abonda: o porto e
  // /arquivos búscanos Tao, sexa File Browser clásico ou Quantum).
  useEffect(() => {
    let cancelado = false;
    (async () => {
      let base = localStorage.getItem(SERVER_KEY) ?? '';
      if (!base) {
        try {
          base = await ServidorPorDefecto();
        } catch { /* queda DEFAULT_SERVER_URL */ }
        if (base && !cancelado && !editadoPolaPersoa.current) setServerUrl(base);
      }
      try {
        const detectada = await DetectarServidor(base || DEFAULT_SERVER_URL);
        if (detectada && !cancelado && !editadoPolaPersoa.current) {
          setServerUrl(detectada);
          localStorage.setItem(SERVER_KEY, detectada);
        }
      } catch { /* sen rede: quédase coa que había */ }
    })();
    return () => { cancelado = true; };
  }, []);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  // Modo "crea o teu contrasinal de Director": só se activa cando se escribe
  // "director" e AÍNDA non hai ningún configurado neste equipo (DirectorConfigurado
  // devolve false) — ver submit(). Nese caso pídese o contrasinal por duplicado antes
  // de gardalo, en vez de intentar un login que sempre fallaría.
  const [modoCrearDirector, setModoCrearDirector] = useState(false);
  const [passwordConfirm, setPasswordConfirm] = useState('');
  // Estado de conexión co servidor de ficheiros: null mentres se comproba, true se
  // responde (borde verde) e false se non hai conexión (borde roxo).
  const [serverOnline, setServerOnline] = useState<boolean | null>(null);

  // Comproba a conexión co servidor cada vez que cambia a URL, cun pequeno atraso
  // (debounce) para non lanzar unha petición por cada tecla mentres se escribe.
  useEffect(() => {
    const url = serverUrl.trim();
    if (!url) {
      setServerOnline(null);
      return;
    }
    setServerOnline(null);
    let cancelled = false;
    const t = setTimeout(async () => {
      try {
        const ok = await CheckServer(url);
        if (!cancelled) setServerOnline(ok);
      } catch {
        if (!cancelled) setServerOnline(false);
      }
    }, 500);
    return () => {
      cancelled = true;
      clearTimeout(t);
    };
  }, [serverUrl]);

  // configurarEEntrar garda o contrasinal novo de Director (dúas veces confirmado por
  // quen chama) e, se se garda ben, fai login con el de seguido — para non obrigar o
  // profesorado a repetir os mesmos datos nun segundo formulario.
  const configurarEEntrar = async () => {
    if (password.length < 8) {
      setError(t('login.director.curto'));
      return;
    }
    if (password !== passwordConfirm) {
      setError(t('login.director.noncoinciden'));
      return;
    }
    setError('');
    setLoading(true);
    try {
      await ConfigurarDirector(password);
      await AdminLogin('director', password);
      onLogin({ username: 'director', name: 'Director', surname: '', isAdmin: true });
    } catch (err: any) {
      setError(typeof err === 'string' ? err : (err?.message || t('login.erro')));
    } finally {
      setLoading(false);
    }
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (modoCrearDirector) {
      await configurarEEntrar();
      return;
    }
    setError('');
    setLoading(true);
    try {
      // Modo Director: acceso local de xestión de Tao (materias, alumnado, sesións,
      // configuración do servidor) — distinto e independente do Admin de File Browser.
      // Non contacta co servidor de ficheiros; a interface muda a un panel de xestión.
      if (username.trim().toLowerCase() === 'director') {
        const configurado = await DirectorConfigurado();
        if (!configurado) {
          // Aínda non hai contrasinal de Director neste equipo (instalación nova, ou
          // unha vella recentemente actualizada): pídese crealo en vez de tentar un
          // login que sempre fallaría.
          setModoCrearDirector(true);
          setLoading(false);
          return;
        }
        await AdminLogin('director', password);
        onLogin({ username: 'director', name: 'Director', surname: '', isAdmin: true });
        return;
      }
      const p = await LoginBinding(username, password, serverUrl.trim());
      localStorage.setItem(SERVER_KEY, serverUrl.trim());
      onLogin({
        username: p.username,
        name: p.name,
        surname: p.surname,
        group: p.group,
        certPath: p.certPath,
        hasCert: p.hasCert,
        connected: p.connected,
        role: (p.role as 'profesor' | 'alumno') || 'alumno',
      });
    } catch (err: any) {
      setError(typeof err === 'string' ? err : (err?.message || t('login.erro')));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-stone-100 flex items-center justify-center p-4 font-sans">
      <div className="w-full max-w-sm bg-white rounded-2xl shadow-xl border border-stone-200 overflow-hidden">
        <div className="p-8 pb-6 text-center border-b border-stone-100">
          <div className="mb-3 flex items-center justify-center">
            <div className="p-1.5 rounded-xl bg-piztu-primary-soft border border-piztu-primary-soft-border">
              <Logo className="w-14 h-14" />
            </div>
          </div>
          <h1 className="text-xl font-bold text-stone-900">Tao</h1>
          <div className="mt-3 flex items-center justify-center gap-1.5">
            <Globe className="w-3.5 h-3.5 text-stone-400" />
            <select
              value={i18n.resolvedLanguage}
              onChange={(e) => { marcarIdiomaExplicito(); i18n.changeLanguage(e.target.value); }}
              className="text-xs text-stone-500 bg-transparent border-none outline-none cursor-pointer"
            >
              {IDIOMAS_DISPONIBLES.map((idioma) => (
                <option key={idioma.code} value={idioma.code}>{idioma.nome}</option>
              ))}
            </select>
          </div>
        </div>

        <form onSubmit={submit} className="p-8 pt-6 flex flex-col gap-4">
          {modoCrearDirector && (
            <div className="text-sm text-stone-700 bg-stone-50 border border-stone-200 rounded-lg px-3 py-2">
              {t('login.director.axuda')}
            </div>
          )}

          {!modoCrearDirector && (
            <div className="flex flex-col gap-1.5">
              <label className="text-sm font-bold text-stone-700">{t('login.servidor')}</label>
              <div className="relative">
                <School className="w-4 h-4 text-stone-400 absolute left-3 top-1/2 -translate-y-1/2" />
                <input
                  value={serverUrl}
                  onChange={(e) => { editadoPolaPersoa.current = true; setServerUrl(e.target.value); }}
                  className={`w-full pl-9 pr-3 py-2.5 bg-white border-2 rounded-lg shadow-sm focus:ring-2 outline-none text-sm ${
                    serverOnline === true
                      ? 'border-emerald-500 focus:ring-emerald-500 focus:border-emerald-500'
                      : serverOnline === false
                      ? 'border-purple-600 focus:ring-purple-600 focus:border-purple-600'
                      : 'border-stone-300 focus:ring-piztu-primary focus:border-piztu-primary'
                  }`}
                  placeholder="bac0.local"
                />
              </div>
            </div>
          )}

          <div className="flex flex-col gap-1.5">
            <label className="text-sm font-bold text-stone-700">{t('login.usuario')}</label>
            <div className="relative">
              <User className="w-4 h-4 text-stone-400 absolute left-3 top-1/2 -translate-y-1/2" />
              <input
                value={modoCrearDirector ? 'director' : username}
                onChange={(e) => setUsername(e.target.value)}
                readOnly={modoCrearDirector}
                autoFocus={!modoCrearDirector}
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                className={`w-full pl-9 pr-3 py-2.5 border border-stone-300 rounded-lg shadow-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none ${modoCrearDirector ? 'bg-stone-100 text-stone-500' : 'bg-white'}`}
                placeholder=""
              />
            </div>
          </div>

          <div className="flex flex-col gap-1.5">
            <label className="text-sm font-bold text-stone-700">
              {modoCrearDirector ? t('login.director.novoContrasinal') : t('login.contrasinal')}
            </label>
            <div className="relative">
              <Lock className="w-4 h-4 text-stone-400 absolute left-3 top-1/2 -translate-y-1/2" />
              <PasswordInput
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoFocus={modoCrearDirector}
                className="pl-9 py-2.5 bg-white border border-stone-300 rounded-lg shadow-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none"
                placeholder=""
              />
            </div>
          </div>

          {modoCrearDirector && (
            <div className="flex flex-col gap-1.5">
              <label className="text-sm font-bold text-stone-700">{t('login.director.confirmarContrasinal')}</label>
              <div className="relative">
                <Lock className="w-4 h-4 text-stone-400 absolute left-3 top-1/2 -translate-y-1/2" />
                <PasswordInput
                  value={passwordConfirm}
                  onChange={(e) => setPasswordConfirm(e.target.value)}
                  className="pl-9 py-2.5 bg-white border border-stone-300 rounded-lg shadow-sm focus:ring-2 focus:ring-piztu-primary focus:border-piztu-primary outline-none"
                  placeholder=""
                />
              </div>
            </div>
          )}

          {error && (
            <div className="text-sm text-rose-700 bg-rose-50 border border-rose-200 rounded-lg px-3 py-2">
              {error}
            </div>
          )}

          <button
            type="submit"
            disabled={loading}
            className="mt-1 w-full py-2.5 bg-stone-900 text-white font-medium rounded-lg hover:bg-stone-800 disabled:opacity-60 transition-colors flex items-center justify-center gap-2"
          >
            {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <Lock className="w-4 h-4" />}
            {modoCrearDirector ? t('login.director.crearEEntrar') : t('login.entrar')}
          </button>

          {modoCrearDirector && (
            <button
              type="button"
              onClick={() => { setModoCrearDirector(false); setUsername(''); setPassword(''); setPasswordConfirm(''); setError(''); }}
              className="text-sm text-stone-500 hover:text-stone-700 underline text-center"
            >
              {t('login.director.cancelar')}
            </button>
          )}
        </form>
      </div>
    </div>
  );
}
