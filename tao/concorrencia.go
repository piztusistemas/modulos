package main

import "sync"

// Descargas en paralelo.
//
// Varias operacións son "listar un cartafol e despois baixar cada ficheiro": as
// materias, as mensaxes dun fío, os traballos que se copian á carpeta de corrección.
// Feitas en serie, o tempo é N veces a latencia da rede — na aula, cun servidor cargado
// e trinta equipos á vez, iso nótase moito máis ca o tamaño dos ficheiros.
//
// enParalelo lanza esas descargas á vez, pero cun tope: sen límite, trinta equipos
// abrindo unha conexión por ficheiro afogan o servidor e empeoran o que se quería
// arranxar. O límite é deliberadamente pequeno.

// fbMaxParalelo é o número máximo de peticións simultáneas ao servidor de ficheiros
// dentro dunha mesma operación.
const fbMaxParalelo = 4

// enParalelo executa fn(0..n-1) con como moito fbMaxParalelo goroutines á vez e agarda
// a que rematen todas. fn ten que escribir só na súa propia posición (por exemplo, nun
// slice de resultados indexado por i) ou protexer o que comparta.
func enParalelo(n int, fn func(i int)) {
	if n <= 0 {
		return
	}
	if n == 1 {
		fn(0) // caso habitual: nada que repartir, evítase crear goroutines
		return
	}
	sem := make(chan struct{}, fbMaxParalelo)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}
