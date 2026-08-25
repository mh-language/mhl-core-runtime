# Instalação e CLI

## Instalação

No macOS/Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.sh | sh
```

No Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/mh-language/mhl-core-runtime/main/install.ps1 | iex
```

Para compilar o runtime a partir deste repositório:

```bash
cd src/mhl-runtime
make build
```

O binário é gerado em `src/mhl-runtime/dist/mhl`.

## Comandos

```text
mhl run <arquivo.mh> [--input chave=valor ...] [--resume]
mhl test <arquivo.mh|diretório>
mhl skills list [diretório]
mhl lint [diretório]
mhl lsp
```

### Executar pipeline

```bash
mhl run main.mh --input name="Ana"
mhl run main.mh --input issue_id=BUG-102 --input target_file=src/app.go
```

`--input` injeta strings no contexto inicial. O pipeline pode declarar inputs tipados, mas a conversão de argumentos de linha de comando começa como texto; faça a conversão/validação no próprio programa quando necessário.

### Retomar execução

```bash
mhl run main.mh --resume
```

Quando o pipeline tem checkpoint `per_step`, o runtime restaura o próximo step e as variáveis persistidas. Um pipeline concluído limpa seu checkpoint; uma falha deixa o estado disponível para retomada.

### Testes e lint

```bash
mhl test test.mh
mhl test test/
mhl lint .
mhl skills list .
```

Os blocos `test` são executados pelo comando `mhl test`. O lint faz análise estática de sintaxe, imports e referências conhecidas antes da execução.

## Desenvolvimento do runtime

```bash
cd src/mhl-runtime
make test
go test ./internal/lang/parser
go vet ./...
```

O runtime não exige dependências externas para ser distribuído como binário, mas os adapters de agente e as ferramentas naturalmente dependem dos executáveis/serviços configurados pelo usuário.
