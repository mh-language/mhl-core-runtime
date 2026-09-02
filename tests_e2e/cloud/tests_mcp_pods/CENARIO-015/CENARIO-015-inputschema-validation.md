# Cenário 015: Validação do `inputSchema` anunciado (campos obrigatórios / extras)

**Objetivo:** Verificar se o pod faz cumprir o `inputSchema` que ele mesmo anuncia
em `tools/list` — todo `input` declarado é `required` e `additionalProperties: false`
— quando um `tools/call` / `run/start` chega com argumentos incompletos ou com
propriedades não declaradas.

```gherkin
Dado que o servidor MCP anuncia inputSchema com "required": ["approved","repo"] e additionalProperties:false para DocPipeline
Quando o cliente chama tools/call DocPipeline sem o argumento "repo"
Então o servidor rejeita com um erro de parâmetros citando o campo faltante
Quando o cliente chama tools/call DocPipeline com um campo não declarado
Então o servidor rejeita com um erro de parâmetros
E o mesmo vale para run/start
```

**Resultado Esperado (contrato pretendido):**
- `tools/call` DocPipeline `{ approved: "yes" }` (sem `repo`) → erro JSON-RPC de
  parâmetros (`-32602`), mensagem citando `repo` / `required`.
- `tools/call` DocPipeline `{ repo, approved, campoExtra }` → erro de parâmetros
  (`additionalProperties: false`).
- `run/start` DocPipeline sem `repo` → mesmo erro de parâmetros.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] `inputSchema` de DocPipeline em `tools/list`
- [ ] Respostas dos três casos (com o corpo exato)
- [ ] Se a run chegou a executar passos, o que saiu

### Observações:
- **Este cenário pode expor uma lacuna.** Hoje `internal/execsvc` só coage/valida os
  inputs **presentes**; não rejeita `required` ausente nem propriedade extra. Se for
  esse o caso, o resultado real será "não funcionou" — a run avança e falha (ou
  interpola vazio) num passo posterior, em vez de um erro de parâmetros limpo.
- O objetivo é registrar o comportamento atual de forma inequívoca.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
