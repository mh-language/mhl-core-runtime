# Referência rápida

## Declarações

```mhl
import "./file.mh" as ns
use { Name as Alias } from "./file.mh"
export agent A { ... }
prompt P(x: string) { "..." }
prompt P(x: string) from "./p.prompt.md"
skill S { ... }
memory M { type: "kv", store: "memory" }
tool T { method(x: string) -> expression }
mcp_server S { transport: "stdio", command: "server" }
pipeline P {
    input x: string
    step S { ... }
}
loop pipeline P { repeat: { stop_when: expr, max_iterations: 3 } }
```

## Literais e operadores

```mhl
null true false
42 3.14 30s 5m 1h 7d
"string" """multiline"""
[1, 2, 3]
{ name: "MHL", "version": 1 }

!ready  -count
a * b / c % d
a + b - c
a < b <= c > d >= e
a == b != c
a && b || c
```

## Controle de fluxo

```mhl
if (cond) { ... } else { ... }
while (cond) { ... }
for (var item in items) { ... }
try { ... } catch (err) { ... } finally { ... }
return value
break "reason"
goto StepName
```

## Operações nativas

```mhl
cmd.exec("go test ./...")
cmd.exec(["go", "test", "./..."] , timeout: 120s)
cmd.exec_all([["go", "test", "./..."], ["go", "vet", "./..."]])
git.status()
git.diff("HEAD~1")
git.add(["."])
git.commit("feat: update")
fs.read(path)
fs.write(path, content)
fs.append(path, content)
fs.exists(path)
fs.list(path)
fs.delete(path)
http.post(url: url, headers: headers, body: payload)
json.parse(text)
json.parse_lines(ndjson)
json.stringify(value)
log.info(value)
```

## Memória

```mhl
mem.set("key", value)
mem.get("key", default)
mem.remove("key")
mem.append(text)
```

Os métodos dependem do backend: `kv` usa `set/get`; `json` usa `set/get/remove`; `append_log` e `jsonl` usam `append`.

## Agentes

```mhl
agent A {
    engine: "cli/tool"
    command: "tool"
    args: ["--prompt", "${prompt}"]
}

A.run(prompt: "Faça a tarefa")
A.run(prompt: P(x: value), schema: schema_text)
```

## Convenções

- use `.mh` como extensão recomendada;
- caminhos são relativos ao arquivo que os declara para imports/prompts externos;
- argumentos de prompts são nomeados;
- métodos de tools são posicionais;
- valores entre steps devem ir para variáveis de pipeline ou memória;
- segredos devem vir de `env()` e não de texto versionado.
