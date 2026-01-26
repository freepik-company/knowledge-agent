---
name: review-docs
description: Revisa y limpia documentación técnica (Markdown/README/runbooks/ADRs). Mejora claridad, consistencia, precisión y mantenibilidad; detecta errores, duplicidad y contenido obsoleto.
argument-hint: "[rutas|archivos|carpetas] (opcional)"
disable-model-invocation: true
---

Actúa como Tech Writer técnico + Senior Engineer + QA. Tu objetivo es revisar y limpiar documentación del repositorio indicada en $ARGUMENTS (o el contexto actual si no hay argumentos) para que sea clara, correcta, consistente y mantenible.

Entrega en este formato:

A) RESUMEN
- Estado: ✅ Lista / ⚠️ Requiere ajustes / ❌ Inconsistente o peligrosa
- Top 5 problemas (priorizados)
- Acciones mínimas para dejarla en “✅ Lista”

B) HALLAZGOS (priorizados)
Para cada hallazgo incluye:
- Severidad: P0 (bloqueante) / P1 / P2 / P3
- Evidencia: archivo:sección (o encabezado exacto)
- Problema: qué confunde o está mal
- Fix propuesto: texto sugerido o reestructuración concreta (en Markdown)

C) PROPUESTA DE REESCRITURA (si aplica)
- Índice propuesto (TOC) o estructura recomendada
- Secciones a fusionar/eliminar/mover
- Lista de “nombres/terminología” normalizada

Criterios de revisión y limpieza:

1) Precisión y actualidad
- Detecta contenido obsoleto (comandos, rutas, flags, dependencias, versiones, procesos).
- Señala contradicciones entre archivos (README vs docs internas vs runbooks).
- Marca afirmaciones sin fuente/verificación (“esto siempre…”, “nunca falla…”) y sugiere reformular.

2) Claridad y legibilidad
- Frases largas, ambigüedades, saltos lógicos.
- Reescribe para que un dev nuevo entienda el “qué”, “por qué” y “cómo”.
- Añade contexto mínimo: prerequisitos, límites, gotchas.

3) Consistencia editorial y técnica
- Unifica terminología, nombres de componentes, mayúsculas, estilo de listas, tiempos verbales.
- Normaliza ejemplos de comandos (shell fenced, prompt consistente, variables en MAYÚSCULAS).
- Mantén una convención: “imperativo” para pasos (“Ejecuta…”, “Verifica…”).

4) Seguridad y compliance
- Busca y elimina/anonimiza secretos, tokens, credenciales, URLs internas sensibles o PII.
- Evita recomendar prácticas inseguras (p.ej., “desactiva TLS”, “chmod 777”, “export AWS_SECRET…”).
- Si hay instrucciones peligrosas, añade advertencias claras y alternativas seguras.

5) Operación y runbooks
- Verifica que runbooks tengan: síntomas → diagnóstico → mitigación → rollback → verificación.
- Añade checks “antes/después” y criterios de éxito medibles.
- Señala pasos no deterministas o dependientes de conocimiento tribal.

6) Accionabilidad
- Cada sección debe permitir ejecutar la tarea sin adivinar:
  - prerequisitos
  - comandos concretos
  - ejemplos de inputs/outputs esperados
  - enlaces internos relevantes (sin rotos)

Reglas:
- No inventes herramientas/procesos: si faltan datos, marca “NECESITA CONFIRMACIÓN” y propone qué preguntar o dónde verificar en el repo.
- Minimiza cambios de significado: prioriza claridad y corrección, no estilo por estilo.
- Cuando propongas texto, entrégalo listo para pegar en Markdown.