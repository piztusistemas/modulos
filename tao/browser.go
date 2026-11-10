package main

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// candidatosChromium son os nomes de binario que probamos, na orde en que un
// Debian normal os adoita ter.
var candidatosChromium = []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"}

// chromiumExecPath busca un Chromium/Chrome xa instalado no sistema (fala
// con el directamente polo protocolo CDP, coma calquera extensión de
// DevTools). A diferenza de Playwright, NON descarga o seu propio
// navegador nin un "driver" á parte na primeira execución — evita así o
// punto de fallo que tiña antes (un 404 do CDN de Microsoft ao tentar
// baixar ese driver). Require só que exista un dos paquetes habituais.
func chromiumExecPath() (string, error) {
	for _, nome := range candidatosChromium {
		if ruta, err := exec.LookPath(nome); err == nil {
			return ruta, nil
		}
	}
	return "", fmt.Errorf(
		"non se atopou Chromium/Chrome instalado neste equipo. Instálao con: sudo apt install chromium")
}

// novoContextoChromium abre un Chromium en fondo (sen sandbox de UI, headless)
// e devolve o seu contexto chromedp, xa limitado por `timeout`. É a base
// compartida por renderizarPDF e ImportProens: os dous falan co mesmo
// Chromium do sistema, sen Playwright.
func novoContextoChromium(timeout time.Duration) (context.Context, context.CancelFunc, error) {
	execPath, err := chromiumExecPath()
	if err != nil {
		return nil, nil, err
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(execPath))
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancelCtx := chromedp.NewContext(allocCtx)
	ctx, cancelTimeout := context.WithTimeout(ctx, timeout)
	cancel := func() {
		cancelTimeout()
		cancelCtx()
		cancelAlloc()
	}
	return ctx, cancel, nil
}

// renderizarPDF carga `html` (contido xa autocontido, sen recursos externos)
// nun Chromium en fondo e devolve o PDF resultante, respectando as regras
// CSS @page e os fondos — mesmo resultado ca Playwright, sen depender del.
func renderizarPDF(html string) ([]byte, error) {
	ctx, cancel, err := novoContextoChromium(30 * time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()

	var pdfBuf []byte
	err = chromedp.Run(ctx,
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			frameTree, err := page.GetFrameTree().Do(ctx)
			if err != nil {
				return err
			}
			return page.SetDocumentContent(frameTree.Frame.ID, html).Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			buf, _, err := page.PrintToPDF().
				WithPrintBackground(true).
				WithPreferCSSPageSize(true).
				Do(ctx)
			if err != nil {
				return err
			}
			pdfBuf = buf
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("non se puido xerar o PDF con Chromium: %w", err)
	}
	return pdfBuf, nil
}
