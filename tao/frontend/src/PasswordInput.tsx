import { useState, InputHTMLAttributes } from 'react';
import { Eye, EyeOff } from 'lucide-react';
import { useTranslation } from 'react-i18next';

// Campo de contrasinal cun ollo dentro da propia cela para amosar/agochar o texto.
// Acepta as mesmas props ca un <input>; o `className` é o do input (reservamos
// sitio á dereita para o botón con pr-10).
export default function PasswordInput({ className = '', ...props }: Omit<InputHTMLAttributes<HTMLInputElement>, 'type'>) {
  const { t } = useTranslation();
  const [visible, setVisible] = useState(false);
  const etiqueta = visible ? t('login.agocharContrasinal') : t('login.amosarContrasinal');

  return (
    <div className="relative w-full">
      <input {...props} type={visible ? 'text' : 'password'} className={`${className} w-full pr-10`} />
      <button
        type="button"
        onClick={() => setVisible(v => !v)}
        // Que o clic non lle roube o foco ao campo.
        onMouseDown={e => e.preventDefault()}
        tabIndex={-1}
        title={etiqueta}
        aria-label={etiqueta}
        aria-pressed={visible}
        className="absolute right-2 top-1/2 -translate-y-1/2 p-1 text-stone-400 hover:text-stone-700 rounded"
      >
        {visible ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
      </button>
    </div>
  );
}
