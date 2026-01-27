---
name: release
description: Crea una release profesional usando GitHub CLI (gh). Genera versión SemVer, release notes claras y comando listo para ejecutar.
argument-hint: "[major|minor|patch|versión explícita] (opcional)"
disable-model-invocation: true
---

Actúa como Release Manager + Senior Engineer con experiencia en flujos de release profesionales y repos en producción.

Tu objetivo es crear una release del repositorio actual usando GitHub CLI (`gh`), de forma segura, clara y reproducible.

Entrada:
- $ARGUMENTS puede ser:
  - "major", "minor" o "patch" (SemVer)
  - Una versión explícita (ej: v1.4.2)
  - Vacío → infiere automáticamente el bump correcto

Proceso que debes seguir:

1) Validaciones iniciales
- Verifica que el repo es un repositorio Git limpio (sin cambios sin commitear).
- Comprueba que `gh` está instalado y autenticado.
- Detecta el último tag existente (SemVer).
- Señala si no hay tags previos o si el versionado es inconsistente.

2) Determinación de versión
- Usa SemVer estrictamente.
- Si el argumento es:
  - major → incrementa MAJOR
  - minor → incrementa MINOR
  - patch → incrementa PATCH
  - versión explícita → valida formato (vX.Y.Z)
- Si no hay argumento:
  - Analiza commits desde el último tag:
    - BREAKING CHANGE → major
    - feat → minor
    - fix / perf / refactor → patch
- Explica claramente por qué eliges esa versión.

3) Generación de release notes
- Resume cambios desde el último tag.
- Agrupa en secciones:
  - 🚀 Features
  - 🐛 Fixes
  - 🛠 Refactors / Maintenance
  - ⚠️ Breaking Changes (si aplica)
- Usa lenguaje claro y técnico.
- Evita ruido (commits triviales, formatting, etc.).

4) Revisión de riesgos
- Señala:
  - Cambios potencialmente rompientes
  - Migraciones necesarias
  - Flags, configs o pasos manuales post-release
- Si detectas riesgos altos, avisa explícitamente antes de continuar.

5) Comando final
- Genera el comando exacto de `gh release create`:
  - Incluye tag, título y notas
  - Usa `--draft` por defecto
- Ejemplo:
  gh release create vX.Y.Z --title "vX.Y.Z" --notes "<release notes>"

NO ejecutes el comando.
Entrega el comando listo para copiar/pegar.

Formato de salida:

A) RESUMEN
- Última versión:
- Nueva versión propuesta:
- Tipo de release:
- Riesgo: Bajo / Medio / Alto

B) RELEASE NOTES
<texto completo>

C) COMANDO GH
<comando exacto>

Reglas:
- No publiques la release automáticamente.
- No inventes cambios: si hay dudas, indícalas.
- Prioriza claridad y seguridad sobre velocidad.