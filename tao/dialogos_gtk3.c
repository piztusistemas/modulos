//go:build linux && cgo && gtk3

// Cola C dos exploradores de ficheiros NON modais de Tao (ver dialogos_gtk3.go).
// Vai nun .c á parte porque o ficheiro Go leva //export e iso impide definir
// funcións C no seu preámbulo.

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include "_cgo_export.h"

static void tao_dialogo_resposta(GtkDialog *dialogo, gint resposta, gpointer datos) {
	taoDialogoResposta(GPOINTER_TO_UINT(datos), resposta, GTK_FILE_CHOOSER(dialogo));
}

// Crea o diálogo SEN modalidade: nin gtk_dialog_run (que o faría modal e
// capturaría toda a entrada da aplicación) nin gtk_window_set_modal. Así a xanela
// principal segue recibindo o clic no botón de pechar aínda que o explorador
// estea aberto. A resposta chega polo sinal "response".
GtkWidget *tao_dialogo_novo(const char *titulo, GtkWindow *pai, GtkFileChooserAction accion,
                            const char *aceptar, guint id) {
	GtkWidget *d = gtk_file_chooser_dialog_new(titulo, pai, accion,
		"_Cancelar", GTK_RESPONSE_CANCEL,
		aceptar, GTK_RESPONSE_ACCEPT,
		NULL);
	gtk_window_set_modal(GTK_WINDOW(d), FALSE);
	gtk_window_set_destroy_with_parent(GTK_WINDOW(d), TRUE);
	gtk_dialog_set_default_response(GTK_DIALOG(d), GTK_RESPONSE_ACCEPT);
	g_signal_connect(d, "response", G_CALLBACK(tao_dialogo_resposta), GUINT_TO_POINTER(id));
	return d;
}

// --- <input type="file"> do webview -----------------------------------------
// WebKitGTK abre polo seu conta un selector modal. Collemos o sinal
// "run-file-chooser" e mostramos no seu lugar o noso diálogo non modal.

static gboolean tao_webview_selector(WebKitWebView *wv, WebKitFileChooserRequest *req, gpointer datos) {
	(void)wv; (void)datos;
	g_object_ref(req);
	taoWebviewSelector(req, webkit_file_chooser_request_get_select_multiple(req));
	return TRUE; // xa o atendemos nós: WebKit non abre o seu
}

static WebKitWebView *tao_buscar_webview(GtkWidget *w) {
	if (WEBKIT_IS_WEB_VIEW(w)) {
		return WEBKIT_WEB_VIEW(w);
	}
	if (!GTK_IS_CONTAINER(w)) {
		return NULL;
	}
	WebKitWebView *atopado = NULL;
	GList *fillos = gtk_container_get_children(GTK_CONTAINER(w));
	for (GList *l = fillos; l != NULL && atopado == NULL; l = l->next) {
		atopado = tao_buscar_webview(GTK_WIDGET(l->data));
	}
	g_list_free(fillos);
	return atopado;
}

gboolean tao_webview_enganchar(GtkWindow *xanela) {
	WebKitWebView *wv = tao_buscar_webview(GTK_WIDGET(xanela));
	if (wv == NULL) {
		return FALSE;
	}
	g_signal_connect(wv, "run-file-chooser", G_CALLBACK(tao_webview_selector), NULL);
	return TRUE;
}

void tao_webview_filtro(GtkFileChooser *fc, WebKitFileChooserRequest *req) {
	GtkFileFilter *f = webkit_file_chooser_request_get_mime_types_filter(req);
	if (f != NULL) {
		gtk_file_chooser_add_filter(fc, f);
	}
}

void tao_webview_resposta(WebKitFileChooserRequest *req, const gchar *const *ficheiros) {
	if (ficheiros != NULL && ficheiros[0] != NULL) {
		webkit_file_chooser_request_select_files(req, ficheiros);
	} else {
		webkit_file_chooser_request_cancel(req);
	}
	g_object_unref(req);
}
