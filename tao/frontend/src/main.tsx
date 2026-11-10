import {StrictMode} from 'react';
import {createRoot} from 'react-dom/client';
import App from './App.tsx';
import './index.css';
import {aplicarIdiomaPiztuPorDefecto} from './i18n';

// Agarda a aplicar o idioma de Piztu (se toca, ver i18n/index.ts) ANTES do
// primeiro render, para non amosar un flash no idioma detectado do sistema
// e trocar decontado ao de Piztu.
aplicarIdiomaPiztuPorDefecto().finally(() => {
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
});
