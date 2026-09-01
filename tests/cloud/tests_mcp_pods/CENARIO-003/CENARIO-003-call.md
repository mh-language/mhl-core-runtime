# Cenário 003: Chamada de uma Tool usando o protocolo MCP stateless

**Objetivo:** Verificar se o cliente pode chamar corretamente uma ferramenta disponível no servidor MCP usando o protocolo stateless.

```gherkin
Dado que o servidor MCP está em execução
E o cliente está autenticado e autorizado a usar a ferramenta
Quando o cliente chama a ferramenta
Então o servidor responde corretamente
```
**Resultado Esperado:** O cliente deve receber uma resposta correta do servidor MCP, indicando que a ferramenta foi chamada com sucesso e fornecendo os resultados esperados da execução da ferramenta.

**Resultado Real:** 
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] Log do servidor MCP mostrando a conexão do cliente

### Observações:
- <Observações adicionais sobre o teste>

**Executado em:** [Data e hora do teste]


