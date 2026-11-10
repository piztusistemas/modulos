import React, { useEffect, useState } from 'react';
import { X, Loader2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { GetActualizacionTao, AplicarActualizacionTao } from '../bindings/tao/app';
import { Events, Application } from '@wailsio/runtime';
import { EstadoActualizacionTao } from '../bindings/tao/models';
import taoLogo from './assets/images/tao-logo.png';

// SobreTao ("i" da barra de ferramentas) — versión, licenza GPLv3 e
// autoactualización, mesmo mecanismo e mesma UI ca "Sobre Piztu"
// (piztu/frontend/src/main.js) e ca diálogo de información de Yang
// (yang/frontend/index.html #sobreOverlay): comproba unha vez en fondo ao
// arrincar (tao/actualizacion.go), e este compoñente só pinta o que xa se
// sabe + escoita "actualizacion_tao_cambiada" por se cambia mentres está
// aberto.
interface Props {
  onClose: () => void;
}

export default function SobreTao({ onClose }: Props) {
  const { t } = useTranslation();
  const [estado, setEstado] = useState<EstadoActualizacionTao | null>(null);
  const [actualizando, setActualizando] = useState(false);
  const [erro, setErro] = useState('');

  useEffect(() => {
    GetActualizacionTao().then(setEstado).catch(() => {});
    const off = Events.On('actualizacion_tao_cambiada', (e) => setEstado(e.data));
    return () => { off(); };
  }, []);

  const dispo = !!(estado && estado.disponible);

  const handleActualizar = async () => {
    if (!confirm(t('sobre.actualizar.confirmar', 'Isto vai reiniciar Tao para aplicar a actualización. ¿Continuar?'))) {
      return;
    }
    setActualizando(true); setErro('');
    try {
      await AplicarActualizacionTao();
      setTimeout(() => Application.Quit(), 400);
    } catch (e: any) {
      setActualizando(false);
      setErro(typeof e === 'string' ? e : (e?.message || String(e)));
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-sm overflow-hidden flex flex-col">
        <div className="flex items-center justify-between px-4 py-3 border-b border-stone-200">
          <span className="font-bold text-stone-800">{t('sobre.titulo', 'Sobre Tao')}</span>
          <button onClick={onClose} className="p-1 rounded hover:bg-stone-100 text-stone-500">
            <X className="w-4 h-4" />
          </button>
        </div>
        <div className="flex flex-col items-center gap-1.5 px-6 py-5 text-center text-sm text-stone-600">
          <img src={taoLogo} alt="Tao" className="w-16 h-16 object-contain mb-1" />
          <p className="text-stone-800 font-medium">
            {t('sobre.version', 'Versión')} {estado?.versionLocal || ''}
          </p>
          {dispo && (
            <p className="text-amber-700 font-semibold">
              {t('sobre.novaversion', 'Nova versión dispoñible: {{v}}', { v: estado?.versionRemota })}
            </p>
          )}
          {dispo && (
            <button onClick={handleActualizar} disabled={actualizando}
              className="mt-1 px-3 py-1.5 text-sm bg-amber-500 hover:bg-amber-600 text-white font-medium rounded-lg transition-colors disabled:opacity-50">
              {actualizando ? <Loader2 className="w-4 h-4 animate-spin inline" /> : (t('sobre.actualizar', '⟳ Actualizar'))}
            </button>
          )}
          {erro && <p className="text-xs text-red-600">{erro}</p>}
          <p className="mt-2">{t('sobre.autor', 'Autor: Martín Pardiñas')}</p>
          <p><a href="https://piztu.org" target="_blank" rel="noreferrer" className="text-piztu-on underline">piztu.org</a></p>
          <p>{t('sobre.contacto', 'Contacto: piztu@omeu.gal')}</p>
          <p className="font-semibold mt-2">{t('sobre.licenza', 'Licenza GNU General Public License v3 (GPLv3)')}</p>
          <p className="text-xs text-stone-500 whitespace-pre-line">
            {t('sobre.licdesc', 'Software libre: pódese usar, estudar, modificar e redistribuír libremente. Calquera traballo derivado que se redistribúa ten que publicarse tamén baixo GPLv3, co código fonte dispoñible (copyleft). Texto completo no ficheiro LICENSE.')}
          </p>
        </div>
      </div>
    </div>
  );
}
