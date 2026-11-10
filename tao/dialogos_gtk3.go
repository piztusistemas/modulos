//go:build linux && cgo && gtk3

package main

/*
#cgo linux pkg-config: gtk+-3.0 webkit2gtk-4.1
#include <stdlib.h>
#include <gtk/gtk.h>
#include <webkit2/webkit2.h>

GtkWidget *tao_dialogo_novo(const char *titulo, GtkWindow *pai, GtkFileChooserAction accion, const char *aceptar, guint id);
gboolean tao_webview_enganchar(GtkWindow *xanela);
void tao_webview_filtro(GtkFileChooser *fc, WebKitFileChooserRequest *req);
void tao_webview_resposta(WebKitFileChooserRequest *req, const gchar *const *ficheiros);
*/
import "C"

import (
	"strings"
	"sync"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Por que non usamos application.Get().Dialog: en GTK3 Wails abre o explorador
// con gtk_dialog_run, que o fai MODAL e captura toda a entrada da aplicación.
// Mentres está aberto (aínda que quede tapado detrás doutra xanela), GTK descarta
// o clic no botón de pechar da xanela principal e Tao parece colgado. Aquí
// creamos o mesmo GtkFileChooserDialog pero sen modalidade e sen bucle aniñado:
// a xanela principal pódese pechar sempre, e co peche o proceso remata e o
// explorador desaparece con el.

// Estado tocado só desde o fío de GTK (dentro de InvokeSync ou do sinal "response").
var (
	dialogoAberto    *C.GtkWidget
	dialogoSeguinte  C.guint
	dialogoRespostas = map[C.guint]func([]string){}
)

var webviewEnganchado sync.Once

// xanelaPrincipalGtk devolve a GtkWindow de Tao, para que o explorador quede
// enriba dela (transient) sen ser modal. nil se aínda non existe.
func xanelaPrincipalGtk() *C.GtkWindow {
	app := application.Get()
	if app == nil {
		return nil
	}
	w, ok := app.Window.GetByName(nomeXanelaPrincipal)
	if !ok || w == nil {
		return nil
	}
	return (*C.GtkWindow)(w.NativeWindow())
}

// abrirDialogoGtk crea e amosa o explorador (fío de GTK). ao_rematar chámase
// tamén no fío de GTK cos ficheiros escollidos (nil se se cancelou). Se xa hai un
// explorador aberto, tráeo á fronte e devolve false sen crear outro.
func abrirDialogoGtk(o opcionsDialogo, pai *C.GtkWindow, ao_rematar func([]string)) (*C.GtkFileChooser, bool) {
	if dialogoAberto != nil {
		C.gtk_window_present((*C.GtkWindow)(unsafe.Pointer(dialogoAberto)))
		return nil, false
	}

	accion := C.GtkFileChooserAction(C.GTK_FILE_CHOOSER_ACTION_OPEN)
	aceptar := "_Abrir"
	if o.gardar {
		accion = C.GTK_FILE_CHOOSER_ACTION_SAVE
		aceptar = "_Gardar"
	}
	cTitulo := C.CString(o.titulo)
	defer C.free(unsafe.Pointer(cTitulo))
	cAceptar := C.CString(aceptar)
	defer C.free(unsafe.Pointer(cAceptar))

	dialogoSeguinte++
	id := dialogoSeguinte
	dialogoRespostas[id] = ao_rematar

	d := C.tao_dialogo_novo(cTitulo, pai, accion, cAceptar, id)
	fc := (*C.GtkFileChooser)(unsafe.Pointer(d))

	for _, f := range o.filtros {
		filtro := C.gtk_file_filter_new()
		cNome := C.CString(f.DisplayName)
		C.gtk_file_filter_set_name(filtro, cNome)
		C.free(unsafe.Pointer(cNome))
		for _, p := range strings.Split(f.Pattern, ";") {
			cPatron := C.CString(strings.TrimSpace(p))
			C.gtk_file_filter_add_pattern(filtro, cPatron)
			C.free(unsafe.Pointer(cPatron))
		}
		C.gtk_file_chooser_add_filter(fc, filtro)
	}
	C.gtk_file_chooser_set_select_multiple(fc, gboolean(o.multiple))
	if o.gardar {
		C.gtk_file_chooser_set_do_overwrite_confirmation(fc, C.TRUE)
		if o.nome != "" {
			cNome := C.CString(o.nome)
			C.gtk_file_chooser_set_current_name(fc, cNome)
			C.free(unsafe.Pointer(cNome))
		}
	}

	dialogoAberto = d
	C.gtk_widget_show(d)
	return fc, true
}

//export taoDialogoResposta
func taoDialogoResposta(id C.guint, resposta C.gint, fc *C.GtkFileChooser) {
	var ficheiros []string
	if resposta == C.GTK_RESPONSE_ACCEPT {
		lista := C.gtk_file_chooser_get_filenames(fc)
		for l := lista; l != nil; l = l.next {
			ficheiros = append(ficheiros, C.GoString((*C.char)(l.data)))
			C.g_free(l.data)
		}
		C.g_slist_free(lista)
	}
	C.gtk_widget_destroy((*C.GtkWidget)(unsafe.Pointer(fc)))
	dialogoAberto = nil
	if f, ok := dialogoRespostas[id]; ok {
		delete(dialogoRespostas, id)
		f(ficheiros)
	}
}

func dialogoNativo(o opcionsDialogo) ([]string, error) {
	pai := xanelaPrincipalGtk()
	resultado := make(chan []string, 1)
	aberto := false
	application.InvokeSync(func() {
		_, aberto = abrirDialogoGtk(o, pai, func(f []string) { resultado <- f })
	})
	if !aberto {
		// Xa había un explorador aberto (quedou á fronte): esta petición cóntase
		// como cancelada; a anterior segue esperando a súa resposta.
		return nil, nil
	}
	return <-resultado, nil
}

// engancharSelectorWebview fai que os <input type="file"> da interface usen
// tamén o explorador non modal. Chamar desde o fío de GTK cando a xanela xa ten
// o webview dentro.
func engancharSelectorWebview() {
	webviewEnganchado.Do(func() {
		if pai := xanelaPrincipalGtk(); pai != nil {
			C.tao_webview_enganchar(pai)
		}
	})
}

//export taoWebviewSelector
func taoWebviewSelector(req *C.WebKitFileChooserRequest, multiple C.gboolean) {
	o := opcionsDialogo{titulo: "Escolle un ficheiro", multiple: multiple != 0}
	if o.multiple {
		o.titulo = "Escolle ficheiros"
	}
	fc, ok := abrirDialogoGtk(o, xanelaPrincipalGtk(), func(ficheiros []string) {
		cLista := make([]*C.gchar, len(ficheiros)+1) // remata en NULL
		for i, f := range ficheiros {
			cLista[i] = (*C.gchar)(C.CString(f))
		}
		C.tao_webview_resposta(req, (**C.gchar)(unsafe.Pointer(&cLista[0])))
		for _, p := range cLista[:len(ficheiros)] {
			C.free(unsafe.Pointer(p))
		}
	})
	if !ok {
		C.tao_webview_resposta(req, nil)
		return
	}
	C.tao_webview_filtro(fc, req)
}

func gboolean(b bool) C.gboolean {
	if b {
		return C.TRUE
	}
	return C.FALSE
}
