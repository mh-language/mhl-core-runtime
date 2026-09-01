# Cenário 003b: Chamada de uma Tool incorreta usando o protocolo MCP stateless

**Objetivo:** Verificar se o cliente pode chamar corretamente uma ferramenta disponível no servidor MCP usando o protocolo stateless.

```gherkin
Dado que o servidor MCP está em execução
E o cliente está autenticado e autorizado a usar a ferramenta
Quando o cliente chama uma ferramenta incorreta
Então o servidor responde com um erro indicando que a ferramenta não foi encontrada
```
**Resultado Esperado:** O cliente deve receber uma resposta de erro do servidor MCP, indicando que a ferramenta chamada não foi encontrada ou não está disponível para uso.

**Resultado Real:** 
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] Log do servidor MCP mostrando a conexão do cliente

### Observações:
- <Observações adicionais sobre o teste>

**Executado em:** [Data e hora do teste]


