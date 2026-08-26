# Pipelines, controle de fluxo e checkpoints

## Estrutura

```mhl
pipeline Build {
    input target: string
    var retries = 0

    step Verify {
        var result = cmd.exec(["go", "test", target])
        if (result.exit_code != 0) {
            retries = retries + 1
            log.warn(result.stderr)
            goto Repair
        }
    }

    step Repair {
        log("tentativa", retries)
    }
}
```

O pipeline executa steps em ordem. Uma variável no nível do pipeline é avaliada uma vez no início e pode ser lida/alterada por todos os steps daquela execução. `var` dentro de um step é descartada ao final do step.

Uma variável de pipeline declarada com `var` é reavaliada do zero a cada execução — inclusive a cada iteração de um `loop pipeline` (ver "Loop pipeline" abaixo). Para um valor que precisa sobreviver entre iterações, use `mem` em vez de `var`.

## Statements

```mhl
var value = expression
name = expression
object.field = expression
return expression
break "motivo"
goto OtherStep
```

`return` sai do bloco de uma tool/lambda; em um step, encerra o step sem produzir valor para o pipeline. `break` aborta a execução do pipeline e, se estiver dentro de `loop pipeline`, encerra também o loop. `goto` transfere para qualquer step nomeado, inclusive anterior; há um limite de segurança para ciclos infinitos.

## Condições e loops

```mhl
if (condition) {
    log("sim")
} else {
    log("não")
}

while (retries < 3) {
    retries = retries + 1
}

for (var item in items) {
    log(item)
}
```

`if`, `while` e `for` também aceitam uma única statement sem chaves. `for` itera arrays. O runtime limita um `while` a 10.000 iterações para impedir travamentos acidentais.

## try/catch/finally

```mhl
try {
    var content = fs.read("config.json")
    log(json.parse(content))
} catch (err) {
    log.error("falha:", err)
} finally {
    log("limpeza")
}
```

O valor capturado é a mensagem textual do erro, sem o prefixo de posição que aparece em uma falha não tratada. `return`, `break` e `goto` não são capturados como erros, mas `finally` ainda é executado.

## Checkpoints

```mhl
pipeline Recoverable {
    checkpoint: {
        enabled: true
        strategy: "per_step"
        storage: "file"
        ttl: 7d
    }

    step First { log("primeiro") }
    step Second { log("segundo") }
}
```

Com `enabled: true` e `strategy: "per_step"`, o runtime salva JSON em `.mhl/state/<pipeline>.json`. O checkpoint registra pipeline, último step, próximo step, steps concluídos, variáveis de pipeline e TTL. O save é atômico; valores string são redigidos pelo resolvedor de credenciais antes de persistir.

Se uma execução falhar no segundo step, `mhl run file.mh --resume` restaura o contexto e começa no próximo step pendente. A inicialização de variáveis do pipeline não é recalculada em um resume. Ao concluir com sucesso, o checkpoint é removido.

## Loop pipeline

```mhl
loop pipeline Poll {
    repeat: {
        stop_when: done == true
        max_iterations: 10
    }

    mem done = false

    step Check {
        done = check()
    }
}
```

O loop avalia `stop_when` somente após uma iteração completa. `max_iterations` é um teto. O progresso do loop é salvo em `.mhl/state/loop-<pipeline>.json`, separado do checkpoint por step; `--resume` continua na próxima iteração incompleta.

## `mem`: variável de pipeline persistente

`var` reseta a cada iteração de um `loop pipeline`, e `stop_when` não enxerga nenhum `var` — só `mem` (e `memory`). `mem` declara uma variável de pipeline que:

- é **get-or-init**: `mem done = false` só escreve o valor inicial na primeira vez; numa iteração seguinte (ou num `--resume`), o valor já persistido é o que vale;
- é lida/escrita como qualquer variável (`done = check()`), inclusive dentro de `stop_when`;
- pode ser reiniciada explicitamente com `nome.reset()` — a próxima leitura ou escrita reexecuta o get-or-init;
- é isolada por execução: cada `mhl run` de um `loop pipeline` recebe um id de instância novo (persistido em `.mhl/state/loop-<pipeline>.json`, recuperado por `--resume`), então duas execuções independentes nunca compartilham o mesmo contador. Uma `pipeline` sem `loop` usa uma instância fixa, então `mem` persiste entre invocações separadas dela.

```mhl
loop pipeline Retry {
    mem attempts = 0

    repeat: {
        stop_when: attempts >= 3
    }

    step Try {
        attempts = attempts + 1
        log("tentativa ${attempts}")
    }
}
```

O backing store de cada `mem` fica em `.mhl/state/mem/<pipeline>/<instância>.json` — um arquivo por execução, ao lado (nunca colidindo com) o checkpoint por step e o checkpoint do loop.
