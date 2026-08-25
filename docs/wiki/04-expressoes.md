# Expressões e valores

## Tipos dinâmicos

O interpretador usa estes valores:

| Tipo | Exemplos |
| --- | --- |
| `null` | `null` |
| `bool` | `true`, `false` |
| `number` | `0`, `3.14` |
| `string` | `"texto"`, `"${name}"` |
| `array` | `[1, "dois", true]` |
| `object` | `{ name: "MHL", version: 1 }` |
| closure | `(item) -> item.active` |

Números são representados internamente como `float64`; comparações que exigem índice ou limite de slice requerem um número inteiro.

## Precedência

Da menor para a maior precedência:

```text
||
&&
== !=
< <= > >=
+ -
* / %
! - (unário)
acesso .campo, chamada (), índice [], slice [..]
```

```mhl
var ok = ready && retries < 3
var total = base + extra * 2
var result = if (ok) "continue" else "stop"
```

`&&` e `||` fazem short-circuit. `if (cond) valor else outro` é uma expressão e exige os dois ramos; não confunda com o statement `if`, que executa blocos.

## Acesso, índices e slices

```mhl
var first = items[0]
var field = record["status"]
record.status = "done"
items[1] = "updated"

var head = items[..3]
var tail = items[2..]
var middle = items[^4..^1]
```

`array[i]` exige índice inteiro dentro dos limites. `object[expr]` exige uma chave string. Slices retornam uma cópia e limites fora da faixa são ajustados ao tamanho do array.

## Métodos de valores

Arrays:

```mhl
items.size()
items.is_empty()
items.get_index(0)
items.index_of("target")
items.filter((item) -> item.active)
items.find((item) -> item.id == wanted)
items.sort_by((item) -> item.name)
```

Objetos:

```mhl
record.keys()
record.values()
```

Strings:

```mhl
name.trim()
name.to_upper()
name.to_lower()
name.contains("mhl")
name.starts_with("M")
name.ends_with("!")
name.split(",")
name.replace("old", "new")
name.substring(0, 4)
```

`keys()` e `values()` usam ordem alfabética determinística para objetos. `find()` retorna o primeiro elemento encontrado; `filter()` e `sort_by()` recebem closures.

## Built-ins

```mhl
log("status", result)
log.info("informação")
log.warn("atenção")
log.error("erro")
fail("interromper a execução")
var token = env("API_TOKEN")
```

`log` retorna `null`. `fail` cria um erro real, que pode ser capturado por `try/catch` ou encerrar a execução.

## Interpolação

Qualquer `${...}` em string pode conter uma expressão MHL:

```mhl
var text = "${user.name}: ${items.size()} itens"
var nested = "resultado: ${if (ok) \"sim\" else \"não\"}"
```

O parser da expressão interpolada é o mesmo parser de expressões do restante do programa.
