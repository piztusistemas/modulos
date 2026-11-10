// Frontend "plano" (sen bundler), mesmo patrón que pancho/frontend/src/main.js:
// os bindings de Go xa quedan dispoñibles en window.go.main.App.* en tempo de
// execución, non fai falla importar nada. Malta só amosa aquí a configuración
// da IA — a xeración de informes en si nunca pasa por esta interface (Tao
// lanza Malta directamente cunha tarefa, ver tarefa.go).
const App = () => window.go.main.App

const $ = (id) => document.getElementById(id)

async function iniciar() {
  const s = await App().GetSettings()
  $('iaProvedor').value = s.iaProvedor || 'gemini'
  $('iaApiKey').value = s.iaApiKey || ''
  $('iaModel').value = s.iaModel || ''
  $('iaBaseUrl').value = s.iaBaseUrl || ''
  $('importadaDeYangAviso').classList.toggle('hidden', !s.importadaDeYang)
}

$('formAxustes').addEventListener('submit', async (ev) => {
  ev.preventDefault()
  const status = $('statusAxustes')
  try {
    await App().SaveSettings({
      iaProvedor: $('iaProvedor').value,
      iaApiKey: $('iaApiKey').value.trim(),
      iaModel: $('iaModel').value.trim(),
      iaBaseUrl: $('iaBaseUrl').value.trim(),
    })
    status.textContent = 'Gardado.'
  } catch (e) {
    status.textContent = 'Erro ao gardar: ' + (e?.message || e)
  }
})

iniciar()

// ── "Sobre Malta" (← botón ℹ da barra) ──────────────────────────────────
// GetSobre le o propio modulo.json (sobre.go): amosa o logo declarado
// (iconaImaxe) tamén na icona da marca, na cabeceira — así non fai falla
// embeber malta.png no frontend, xa chega con lelo dende Go coma no modal.
async function cargarSobre() {
  const s = await App().GetSobre()
  $('sobreTitulo').textContent = 'Sobre ' + s.nome
  $('sobreVersion').textContent = 'Versión ' + (s.version || '')
  $('sobreDescricion').textContent = s.descricion || ''
  $('sobreDescricion').classList.toggle('hidden', !s.descricion)
  $('sobreAutor').textContent = s.autor ? ('Autor: ' + s.autor) : ''
  $('sobreAutor').classList.toggle('hidden', !s.autor)
  $('sobreContacto').textContent = s.email ? ('Contacto: ' + s.email) : ''
  $('sobreContacto').classList.toggle('hidden', !s.email)
  const web = $('sobreWeb')
  if (s.web) {
    web.href = s.web
    web.textContent = s.web
    web.classList.remove('hidden')
  } else {
    web.classList.add('hidden')
  }
  if (s.iconaImaxeDataURI) {
    $('sobreLogo').src = s.iconaImaxeDataURI
    $('sobreLogo').classList.remove('hidden')
    $('sobreLogoEmoji').classList.add('hidden')
    // Mesma icona tamén na marca da cabeceira, en vez do emoji de reserva.
    const brandIcon = $('brandIcon')
    if (brandIcon && brandIcon.tagName === 'SPAN') {
      const img = document.createElement('img')
      img.id = 'brandIcon'
      img.src = s.iconaImaxeDataURI
      img.alt = ''
      img.className = 'brand-icon-img'
      brandIcon.replaceWith(img)
    }
  }
}
cargarSobre()

$('btnSobre').addEventListener('click', () => $('overlaySobre').classList.remove('hidden'))
$('btnSobrePechar').addEventListener('click', () => $('overlaySobre').classList.add('hidden'))
