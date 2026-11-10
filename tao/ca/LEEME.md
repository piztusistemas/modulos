# CA do departamento

Aquí só se publica `ca.crt` (certificado público). A clave privada `ca.key`
non está no repositorio, porque é público e con ela calquera podería emitir
certificados válidos.

Para compilar Tao hai que copiar `ca.key` neste cartafol antes de `go build`
(`certs.go` incrústaa con `//go:embed ca/ca.key`):

    cd frontend && npm install && npm run build
    cd .. && go build -tags gtk3 -o build/bin/tao .
