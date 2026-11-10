// Dobrado das imaxes do editor Markdown.
//
// Unha imaxe insírese como data URI (![foto.jpg](data:image/jpeg;base64,...)), que é o
// que fai que viaxe coa copia de seguridade e acabe no PDF asinado. O problema é que
// ese mesmo texto é o que se edita: unha foto son decenas de miles de caracteres que
// enterran o que o alumnado escribiu.
//
// A solución é separar o que se garda do que se amosa. O textarea traballa sempre en
// "texto amosado", onde cada data URI se substitúe por unha marca curta:
//
//   gardado:  ![barack obama.jpg](data:image/jpeg;base64,/9j/4AAQSkZJRgABAQ...)
//   amosado:  ![barack obama.jpg](imaxe:1)
//
// e só se converte nos bordos: fold() ao amosar, unfold() ao devolver o cambio. Así o
// formato gardado non cambia nada — vista previa, informe asinado e a copia seguen
// vendo o Markdown de sempre.

export const IMAGE_TOKEN = 'imaxe:';

// Un data URI non pode conter ')' (o alfabeto base64 é A-Za-z0-9+/=), así que o
// paréntese de peche do Markdown delimita sen ambigüidade onde remata.
const DATA_URI_RE = /\((data:[^)\s]+)\)/g;
const TOKEN_RE = new RegExp(`\\(${IMAGE_TOKEN}(\\d+)\\)`, 'g');

export interface FoldedMarkdown {
  // O texto que se edita, coas imaxes reducidas a (imaxe:N).
  display: string;
  // Os data URI retirados, na orde en que aparecen: a marca (imaxe:N) devolve images[N-1].
  images: string[];
}

// fold retira os data URI do Markdown e déixaos aparte, numerados.
export function fold(real: string): FoldedMarkdown {
  const images: string[] = [];
  const display = (real || '').replace(DATA_URI_RE, (_match, uri: string) => {
    images.push(uri);
    return `(${IMAGE_TOKEN}${images.length})`;
  });
  return { display, images };
}

// unfold devolve o Markdown real, repoñendo cada marca coa súa imaxe.
//
// Unha marca que non apunte a ningunha imaxe déixase tal cal: é texto que escribiu quen
// edita, e inventarlle unha imaxe sería peor que deixalo ver.
export function unfold(display: string, images: string[]): string {
  return (display || '').replace(TOKEN_RE, (match, n: string) => {
    const uri = images[Number(n) - 1];
    return uri ? `(${uri})` : match;
  });
}

// imagesWeight devolve o tamaño aproximado que ocupan as imaxes, en bytes. Serve para
// avisar de que unha práctica se está a facer pesada: todo isto vai dentro da copia de
// seguridade, e súbese enteira cada vez que se garda.
export function imagesWeight(images: string[]): number {
  return images.reduce((total, uri) => {
    const base64 = uri.slice(uri.indexOf(',') + 1);
    // Cada 4 caracteres de base64 son 3 bytes; o recheo final ('=') non conta.
    const padding = base64.endsWith('==') ? 2 : base64.endsWith('=') ? 1 : 0;
    return total + Math.max(0, Math.floor((base64.length * 3) / 4) - padding);
  }, 0);
}

export function formatWeight(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

// Formatos de traballo de editores de imaxe (Photoshop, GIMP): o navegador non os sabe
// debuxar, así que na vista previa e no PDF asinado só quedaría unha imaxe rota, e
// ademais adoitan pesar moito. Compróbase o tipo MIME e tamén a extensión, porque o
// sistema non sempre lles asigna un tipo (moitas veces chegan como "" ou octet-stream).
const BLOCKED_IMAGE_TYPES = ['image/vnd.adobe.photoshop', 'image/x-photoshop', 'application/x-photoshop', 'application/photoshop', 'image/psd', 'image/x-xcf', 'image/xcf'];
const BLOCKED_IMAGE_EXT_RE = /\.(psd|psb|xcf)$/i;

export function isBlockedImageFormat(file: File): boolean {
  return BLOCKED_IMAGE_TYPES.includes(file.type.toLowerCase()) || BLOCKED_IMAGE_EXT_RE.test(file.name);
}

// Tamaño máximo dunha imaxe xa codificada (o data URI enteiro, en base64): 5 MB.
export const MAX_IMAGE_DATA_URI = 5 * 1024 * 1024;
