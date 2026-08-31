# Cenário 002b: Teste de Listagem de Tools usando o protocolo MCP stateless

**Objetivo:** Verificar se o cliente pode listar corretamente as ferramentas disponíveis no servidor MCP.

```gherkin
Dado que o servidor MCP está em execução
Quando o cliente solicita a listagem de ferramentas disponíveis
E o protocolo informado é stateless
Então o servidor MCP deve retornar a lista completa de ferramentas
```

**Resultado Esperado:** O cliente deve receber uma lista completa de ferramentas disponíveis no servidor MCP, incluindo informações como nome, versão e descrição de cada ferramenta.

**Resultado Real:** 
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] Log do servidor MCP mostrando a conexão do cliente

**Executado em:** [Data e hora do teste]


