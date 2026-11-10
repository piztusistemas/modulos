# Tao

Aplicación de escritorio (**Wails**: Go + React + TypeScript) para que o alumnado
**edite as súas prácticas diarias** e xere **informes asinados dixitalmente** co seu
certificado, co fin de dificultar o copiado dos traballos. É o produto compañeiro
de Piztu (control de aula): Piztu móstrase no equipo do profesorado, Tao corre no
equipo do alumnado.

Deriva de `temporaliza` (planificación por unidades didácticas) e engade:

- **Login** por usuario/contrasinal; en Fase 2 descargará do servidor (URL na
  configuración) a última copia de seguridade, os datos do alumno/a e o seu certificado.
- **Rexistro Diario** como editor **Markdown** con vista previa (imaxes, aliñación,
  formato).
- **Informes asinados** por sesión, semana, entre datas, unidade didáctica ou avaliación.
  Cada páxina do PDF leva a **firma en vertical na marxe esquerda coa data**, e o PDF
  asínase criptograficamente (**PAdES**) co certificado do alumno/a.

## Desenvolvemento

- `wails dev` — modo desenvolvemento con recarga en quente do frontend.
- `wails build` — paquete de produción.

O frontend está en `frontend/` (Vite + React 19 + Tailwind v4). O backend Go expón os
métodos vinculados en `app.go`, `auth.go`, `proens.go` e `report.go`.
