package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Chamada de retorno a Tao (POST /api/v1/malta/resultado, tao/malta_launch.go
// + tao/malta_resultado.go): Malta NON chama a ningún outro endpoint de
// Tao — recibe TODOS os datos que precisa xa empaquetados en
// ContextoTarefaMalta (tarefa.go), e a súa única saída é devolver o
// resultado por aquí. Mesmo transporte (socket Unix + Bearer) ca o resto do
// ecosistema, pero o token aquí é o TOKEN ESTÁTICO de proceso de Tao
// (mesmo que usa o pipe Tao→Yang, ver yang/taoclient.go), non un token de
// módulo con permisos — este endpoint só acepta chamadas de retorno dunha
// tarefa que o propio Tao acaba de lanzar.

const enviarResultadoTimeout = 20 * time.Second

// enviarResultado fai POST http://tao/api/v1/malta/resultado sobre o socket
// Unix de ctx.APISocket, autenticado con ctx.APIToken.
func enviarResultado(ctx ContextoTarefaMalta, resultado ResultadoTarefaMalta) error {
	corpo, err := json.Marshal(resultado)
	if err != nil {
		return err
	}

	httpClient := &http.Client{
		Timeout: enviarResultadoTimeout,
		Transport: &http.Transport{
			DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(dialCtx, "unix", ctx.APISocket)
			},
		},
	}

	req, err := http.NewRequest(http.MethodPost, "http://tao/api/v1/malta/resultado", bytes.NewReader(corpo))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ctx.APIToken)

	res, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("non se puido contactar con Tao: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(res.Body)
		return fmt.Errorf("Tao rexeitou o resultado (%d): %s", res.StatusCode, string(data))
	}
	return nil
}
