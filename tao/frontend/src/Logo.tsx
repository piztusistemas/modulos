import taoLogo from './assets/images/tao-logo.png';

// A marca de Tao: a mascota do aplicativo (ver logos/tao.png na raíz do repo).
export default function Logo({ className }: { className?: string }) {
  return <img src={taoLogo} alt="Tao" className={`${className ?? ''} object-contain`} />;
}
